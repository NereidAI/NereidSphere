// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/NereidAI/NereidSphere/internal/config"
)

// mockNIAdmin simulates the identity service Admin REST API for group
// membership operations.
type mockNIAdmin struct {
	server *httptest.Server
	mu     sync.Mutex
	groups map[string]string // path -> id
	calls  []string
	// userGroups maps userID -> set of group paths currently joined.
	userGroups map[string]map[string]bool
}

func newMockNIAdmin(t *testing.T) *mockNIAdmin {
	t.Helper()
	m := &mockNIAdmin{
		groups:     map[string]string{},
		userGroups: map[string]map[string]bool{},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/realms/nereidai/protocol/openid-connect/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tkn",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	})

	// List groups (GET) and create top-level group (POST).
	mux.HandleFunc("/admin/realms/nereidai/groups", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		if r.Method == http.MethodGet {
			// Keycloak 26 reports only top-level groups here (with
			// subGroupCount); it does not expand nested subGroups.
			_ = json.NewEncoder(w).Encode(m.childGroups(""))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(raw, &payload)
		newPath := "/" + payload["name"]
		id := "g-" + newPath
		m.mu.Lock()
		m.groups[newPath] = id
		m.mu.Unlock()
		w.Header().Set("Location", "/admin/realms/nereidai/groups/"+id)
		w.WriteHeader(http.StatusCreated)
	})

	// List (GET) or create (POST) a child group.
	mux.HandleFunc("/admin/realms/nereidai/groups/{id}/children", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(m.childGroups(m.pathOf(r.PathValue("id"))))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(raw, &payload)
		parentID := r.PathValue("id")
		parentPath := m.pathOf(parentID)
		newPath := parentPath + "/" + payload["name"]
		id := "g-" + newPath
		m.mu.Lock()
		m.groups[newPath] = id
		m.mu.Unlock()
		w.Header().Set("Location", "/admin/realms/nereidai/groups/"+id)
		w.WriteHeader(http.StatusCreated)
	})

	// User group membership.
	mux.HandleFunc("/admin/realms/nereidai/users/{uid}/groups", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		uid := r.PathValue("uid")
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			paths := m.userGroups[uid]
			out := []UserGroup{}
			for p := range paths {
				out = append(out, UserGroup{ID: m.groups[p], Name: p[strings.LastIndex(p, "/")+1:], Path: p})
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/admin/realms/nereidai/users/{uid}/groups/{gid}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		uid := r.PathValue("uid")
		gid := r.PathValue("gid")
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.userGroups[uid] == nil {
			m.userGroups[uid] = map[string]bool{}
		}
		if r.Method == http.MethodPut {
			m.userGroups[uid][m.pathOf(gid)] = true
		} else {
			delete(m.userGroups[uid], m.pathOf(gid))
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Any other admin call (user lookup etc.).
	mux.HandleFunc("/admin/realms/nereidai/users/{uid}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		uid := r.PathValue("uid")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": uid, "username": uid, "enabled": true, "requiredActions": []string{},
		})
	})

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockNIAdmin) seedMembership(userID, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.userGroups[userID] == nil {
		m.userGroups[userID] = map[string]bool{}
	}
	m.userGroups[userID][path] = true
	// Seed the full ancestor chain so parents have real IDs.
	prefix := ""
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		prefix += "/" + part
		if _, ok := m.groups[prefix]; !ok {
			m.groups[prefix] = "g-" + prefix
		}
	}
}

func (m *mockNIAdmin) pathOf(id string) string {
	for p, gid := range m.groups {
		if gid == id {
			return p
		}
	}
	return ""
}

func (m *mockNIAdmin) record(method, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, method+" "+path)
}

func (m *mockNIAdmin) joined(userID string) map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	for p := range m.userGroups[userID] {
		out[p] = true
	}
	return out
}

// childGroups returns the direct children of parentPath ("" = realm root),
// mirroring Keycloak's children endpoint: only one level, never the whole tree.
func (m *mockNIAdmin) childGroups(parentPath string) []UserGroup {
	prefix := parentPath + "/"
	out := []UserGroup{}
	m.mu.Lock()
	defer m.mu.Unlock()
	for p, id := range m.groups {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := p[len(prefix):]
		if strings.Contains(rest, "/") {
			continue // descendant, not a direct child
		}
		out = append(out, UserGroup{ID: id, Name: rest, Path: p})
	}
	return out
}

// productRolesHandler builds a Handler with a working NI admin client.
func productRolesHandler(t *testing.T, roles []string, niBaseURL string) (*Handler, *http.Cookie) {
	t.Helper()
	h, cookie := adminTestSetup(t, roles)
	h.niAdmin = NewNIAdminClient(niBaseURL+"/realms/nereidai", "nereidsphere-admin", "secret")
	return h, cookie
}

func TestGetProductRolesParsesGroups(t *testing.T) {
	ni := newMockNIAdmin(t)
	ni.seedMembership("u1", "/nereidlink/tenant-a/admins")
	ni.seedMembership("u1", "/nereidforge/tenant-a/operator")
	ni.seedMembership("u1", "/unrelated/group") // ignored

	h, cookie := productRolesHandler(t, []string{"admin"}, ni.server.URL)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/u1/product-roles", nil)
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.getProductRoles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get product roles = %d %s", rec.Code, rec.Body.String())
	}
	var resp ProductRolesRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Assignments["link"]) != 1 || resp.Assignments["link"][0].TenantID != "tenant-a" ||
		resp.Assignments["link"][0].Role != "admins" {
		t.Fatalf("link assignments = %+v", resp.Assignments["link"])
	}
	if len(resp.Assignments["forge"]) != 1 || resp.Assignments["forge"][0].Role != "operator" {
		t.Fatalf("forge assignments = %+v", resp.Assignments["forge"])
	}
	if _, ok := resp.Assignments["horizon"]; ok {
		t.Fatalf("unexpected horizon assignments: %+v", resp.Assignments)
	}
}

func TestPutProductRolesAppliesDiff(t *testing.T) {
	ni := newMockNIAdmin(t)
	ni.seedMembership("u1", "/nereidlink/tenant-a/read_only")

	h, cookie := productRolesHandler(t, []string{"admin"}, ni.server.URL)
	body := `{"assignments":{"link":[{"tenant_id":"tenant-a","role":"admins"}]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/u1/product-roles", strings.NewReader(body))
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.putProductRoles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put product roles = %d %s", rec.Code, rec.Body.String())
	}

	joined := ni.joined("u1")
	if !joined["/nereidlink/tenant-a/admins"] {
		t.Fatalf("expected admins group joined, got %+v", joined)
	}
	if joined["/nereidlink/tenant-a/read_only"] {
		t.Fatalf("expected read_only group removed, got %+v", joined)
	}

	var resp ProductRolesRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Assignments["link"]) != 1 || resp.Assignments["link"][0].Role != "admins" {
		t.Fatalf("response assignments = %+v", resp.Assignments)
	}
}

func TestPutProductRolesKeepsUnrelatedGroups(t *testing.T) {
	ni := newMockNIAdmin(t)
	ni.seedMembership("u1", "/nereidlink/tenant-a/read_only")
	ni.seedMembership("u1", "/unrelated/group")
	ni.seedMembership("u1", "/user")

	h, cookie := productRolesHandler(t, []string{"admin"}, ni.server.URL)
	body := `{"assignments":{"link":[{"tenant_id":"tenant-a","role":"admins"}]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/u1/product-roles", strings.NewReader(body))
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.putProductRoles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put product roles = %d %s", rec.Code, rec.Body.String())
	}

	joined := ni.joined("u1")
	// Suite-managed stale assignment is reconciled away…
	if joined["/nereidlink/tenant-a/read_only"] {
		t.Fatalf("expected stale suite group removed, got %+v", joined)
	}
	// …but groups outside the suite namespace are never touched.
	if !joined["/unrelated/group"] || !joined["/user"] {
		t.Fatalf("unrelated groups must be preserved, got %+v", joined)
	}
}

// TestCreateUserWithProductRolesReusesExistingGroup reproduces the reported
// 502: assigning the same product/tenant/role to a second user must reuse the
// group created for the first user instead of re-creating it.
func TestCreateUserWithProductRolesReusesExistingGroup(t *testing.T) {
	ni := newMockNIAdmin(t)
	ni.seedMembership("existing", "/nereidlink/tenant-a/admins") // first user already has it

	h, cookie := adminTestSetup(t, []string{"admin"})
	h.niAdmin = NewNIAdminClient(ni.server.URL+"/realms/nereidai", "nereidsphere-admin", "secret")

	body := `{"username":"second","password":"longenoughpw","temporary":true,"roles":["admin"],
		"product_roles":{"link":[{"tenant_id":"tenant-a","role":"admins"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.createUser(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second user in same group = %d %s", rec.Code, rec.Body.String())
	}
	for _, call := range ni.calls {
		if strings.HasPrefix(call, "POST /admin/realms/nereidai/groups") {
			t.Fatalf("must not create an existing group, calls: %v", ni.calls)
		}
	}
}

func TestPutProductRolesRejectsUnknownRole(t *testing.T) {
	ni := newMockNIAdmin(t)
	h, cookie := productRolesHandler(t, []string{"admin"}, ni.server.URL)
	body := `{"assignments":{"link":[{"tenant_id":"tenant-a","role":"superuser"}]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/u1/product-roles", strings.NewReader(body))
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.putProductRoles(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown role = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPutProductRolesRejectsUnknownProduct(t *testing.T) {
	ni := newMockNIAdmin(t)
	h, cookie := productRolesHandler(t, []string{"admin"}, ni.server.URL)
	body := `{"assignments":{"nereidedge":[{"tenant_id":"t","role":"x"}]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/u1/product-roles", strings.NewReader(body))
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.putProductRoles(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown product = %d %s", rec.Code, rec.Body.String())
	}
}

func TestProductRolesRequireAdmin(t *testing.T) {
	ni := newMockNIAdmin(t)
	h, cookie := productRolesHandler(t, []string{"operator"}, ni.server.URL)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/u1/product-roles",
		strings.NewReader(`{"assignments":{}}`))
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.putProductRoles(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator put = %d, want 403", rec.Code)
	}

	anon := httptest.NewRecorder()
	h.putProductRoles(anon, httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/u1/product-roles",
		strings.NewReader(`{"assignments":{}}`)))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous put = %d, want 401", anon.Code)
	}
}

func TestProductRolesUnavailableWithoutAdminClient(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"}) // no NI client configured
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/u1/product-roles", nil)
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.getProductRoles(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get without ni admin = %d, want 404", rec.Code)
	}
}

func TestProductCatalog(t *testing.T) {
	ni := newMockNIAdmin(t)
	h, cookie := productRolesHandler(t, []string{"admin"}, ni.server.URL)
	h.cfg.Products = []config.ProductSection{
		{ID: "link", Name: "NereidLink", TenantsPath: "/v1/tenants"},
		{ID: "forge", Name: "NereidForge", TenantsPath: "/v1/integration/ns/tenants"},
		{ID: "edge", Name: "NereidEdge"}, // no suite product-role mapping
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/product-catalog", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.productCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog = %d %s", rec.Code, rec.Body.String())
	}
	var resp ProductCatalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Products) != 2 {
		t.Fatalf("products = %+v", resp.Products)
	}
	byID := map[string]ProductCatalogEntry{}
	for _, p := range resp.Products {
		byID[p.ID] = p
	}
	if got := byID["link"]; got.TenantsPath != "/v1/tenants" || len(got.Roles) != 3 {
		t.Fatalf("link catalog = %+v", got)
	}
	if _, ok := byID["edge"]; ok {
		t.Fatalf("edge should not be in the catalog: %+v", resp.Products)
	}
}

func TestCreateUserWithProductRoles(t *testing.T) {
	ni := newMockNIAdmin(t)
	h, cookie := adminTestSetup(t, []string{"admin"})
	h.niAdmin = NewNIAdminClient(ni.server.URL+"/realms/nereidai", "nereidsphere-admin", "secret")

	body := `{"username":"newbie","password":"longenoughpw","temporary":true,"roles":["admin"],
		"product_roles":{"link":[{"tenant_id":"tenant-a","role":"admins"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.createUser(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with product roles = %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	joined := ni.joined(created["id"])
	if !joined["/nereidlink/tenant-a/admins"] {
		t.Fatalf("expected group joined for %q, got %+v", created["id"], joined)
	}
}

func TestCreateUserWithProductRolesRejectedWithoutAdminClient(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"}) // no NI admin client
	body := `{"username":"newbie","password":"longenoughpw","temporary":true,
		"product_roles":{"link":[{"tenant_id":"tenant-a","role":"admins"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.createUser(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create without ni admin = %d, want 400", rec.Code)
	}
}

func TestPutProductRolesFederatedReadOnly(t *testing.T) {
	ni := newMockNIAdmin(t)
	h, cookie := productRolesHandler(t, []string{"admin"}, ni.server.URL)
	// mockAdmin serves "fed1" as a federated account.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/fed1/product-roles",
		strings.NewReader(`{"assignments":{}}`))
	req.SetPathValue("id", "fed1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.putProductRoles(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("federated put = %d, want 409", rec.Code)
	}
}
