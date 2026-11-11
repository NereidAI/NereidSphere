// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NereidAI/NereidSphere/internal/auth"
	"github.com/NereidAI/NereidSphere/internal/config"
)

type mockAdmin struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []string
}

func newMockAdmin(t *testing.T) (*mockAdmin, *Client) {
	t.Helper()
	m := &mockAdmin{}
	mux := http.NewServeMux()

	mux.HandleFunc("/realms/nereidai/protocol/openid-connect/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "header." + encode(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}) + ".sig",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/admin/realms/nereidai/users", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.RequestURI())
		if r.URL.Path == "/admin/realms/nereidai/users/count" {
			_, _ = w.Write([]byte("2"))
			return
		}
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Location", "/admin/realms/nereidai/users/u-new")
			w.WriteHeader(http.StatusCreated)
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "u1", "username": "alice", "enabled": true, "emailVerified": true, "requiredActions": []string{}},
				{"id": "fed1", "username": "bob", "enabled": true,
					"federationLink":       m.server.URL + "/broker/entra/entities/x",
					"federationIdentities": []map[string]string{{"identityProvider": "entra", "userName": "bob@x"}},
				},
			})
		}
	})
	mux.HandleFunc("/admin/realms/nereidai/users/count", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2"))
	})
	mux.HandleFunc("/admin/realms/nereidai/users/{id...}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/role-mappings/realm/composite") && r.Method == http.MethodGet {
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/realms/nereidai/users/"), "/role-mappings/realm/composite")
			// Mirror the real realm: "user" is inherited by every account
			// through the default-roles composite even with no direct
			// mapping; u1 additionally holds a direct admin role.
			if id == "u1" {
				_, _ = w.Write([]byte(`[{"id":"r-user","name":"user"},{"id":"r-admin","name":"admin"}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"r-user","name":"user"}]`))
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/admin/realms/nereidai/users/")
		id = strings.Split(id, "/")[0]
		if id == "fed1" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "fed1", "username": "bob", "enabled": true,
				"federationLink": m.server.URL + "/broker/entra/entities/x",
			})
			return
		}
		if r.Method == http.MethodGet {
			user := map[string]any{"id": id, "username": id, "enabled": true}
			if id == "u1" {
				user["email"] = "alice@example.com"
			}
			_ = json.NewEncoder(w).Encode(user)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/admin/realms/nereidai/roles", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "r-user", "name": "user"},
			{"id": "r-operator", "name": "operator"},
			{"id": "r-admin", "name": "admin"},
			{"id": "r-default", "name": "default-roles-nereidai"},
		})
	})
	mux.HandleFunc("/admin/realms/nereidai/roles/{role}/users", func(w http.ResponseWriter, r *http.Request) {
		role := r.PathValue("role")
		if role == "admin" {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "u1", "username": "alice", "enabled": true}})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	mux.HandleFunc("/admin/realms/nereidai/users/u-new/role-mappings/realm", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"name":"admin"`) {
			t.Errorf("role mapping body = %s", string(raw))
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		m.record(r.Method, r.URL.Path)
		if strings.Contains(r.URL.Path, "/reset-password") {
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"temporary":true`) {
				t.Errorf("reset password body = %s", string(raw))
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)

	client, err := NewClient(m.server.URL+"/realms/nereidai", "nereidsphere-admin", "secret")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return m, client
}

func encode(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (m *mockAdmin) record(method, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, method+" "+path)
}

func TestCreateUserFlow(t *testing.T) {
	m, client := newMockAdmin(t)
	id, err := client.CreateUser(t.Context(), CreateUserRequest{
		Username: "newbie", Password: "longenoughpw", Temporary: true, Roles: []string{"admin"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id != "u-new" {
		t.Fatalf("id = %q", id)
	}
	joined := strings.Join(m.calls, "\n")
	for _, want := range []string{
		"POST /admin/realms/nereidai/users",
		"GET /admin/realms/nereidai/roles",
		"POST /admin/realms/nereidai/users/u-new/role-mappings/realm",
		"PUT /admin/realms/nereidai/users/u-new/reset-password",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
}

func TestListUsersProjectsFederation(t *testing.T) {
	_, client := newMockAdmin(t)
	page, err := client.ListUsers(t.Context(), "", 0, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Users) != 2 || page.Total != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page.Users[1].FederationProvider != "entra" || !page.Users[1].Federated {
		t.Fatalf("federation projection = %+v", page.Users[1])
	}
}

func TestSetRolesRejectsUnknown(t *testing.T) {
	_, client := newMockAdmin(t)
	if err := client.SetRoles(t.Context(), "u1", []string{"superuser"}); err == nil {
		t.Fatal("expected unknown role rejected")
	}
}

func adminTestSetup(t *testing.T, roles []string) (*Handler, *http.Cookie) {
	t.Helper()
	_, client := newMockAdmin(t)
	cfg := &config.Config{}
	cfg.Auth.Mode = config.AuthModeOIDC
	cfg.OIDC.Issuer = "http://issuer/realms/nereidai"
	cfg.Session.CookieName = auth.SessionCookie
	cfg.Session.IdleDuration = time.Hour
	cfg.Session.AbsoluteDuration = time.Hour
	cfg.Products = []config.ProductSection{{ID: "link", Mode: config.ProductModeNS}}

	codec, err := auth.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	srv := auth.NewServer(cfg, codec, nil, nil)

	rec := httptest.NewRecorder()
	now := time.Now()
	if err := codec.SaveSession(rec, auth.CookieOptions{Name: auth.SessionCookie, TTL: time.Hour},
		&auth.SessionData{
			Identity: auth.Identity{Sub: "me", Username: "me", Roles: roles},
			LoginAt:  now, LastSeen: now, Expiry: now.Add(time.Hour),
		}); err != nil {
		t.Fatalf("session: %v", err)
	}
	return NewHandler(cfg, srv.Resolver(), client, nil), rec.Result().Cookies()[0]
}

func TestAdminAuthorization(t *testing.T) {
	adminH, cookie := adminTestSetup(t, []string{"admin"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	adminH.listUsers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list = %d %s", rec.Code, rec.Body.String())
	}

	opH, opCookie := adminTestSetup(t, []string{"operator"})
	opReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	opReq.AddCookie(opCookie)
	opRec := httptest.NewRecorder()
	opH.listUsers(opRec, opReq)
	if opRec.Code != http.StatusOK {
		t.Fatalf("operator list = %d", opRec.Code)
	}

	userH, userCookie := adminTestSetup(t, []string{"user"})
	userReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users",
		strings.NewReader(`{"username":"x","password":"longenough"}`))
	userReq.AddCookie(userCookie)
	userRec := httptest.NewRecorder()
	userH.createUser(userRec, userReq)
	if userRec.Code != http.StatusForbidden {
		t.Fatalf("user create = %d, want 403", userRec.Code)
	}

	anon := httptest.NewRecorder()
	adminH.listUsers(anon, httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", anon.Code)
	}
}

func TestFederatedUserReadOnly(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/fed1/reset-password",
		strings.NewReader(`{"password":"longenough"}`))
	req.SetPathValue("id", "fed1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.resetPassword(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "FEDERATED_READ_ONLY") {
		t.Fatalf("federated reset = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSelfActionForbidden(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/me", nil)
	req.SetPathValue("id", "me")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.deleteUser(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "SELF_ACTION_FORBIDDEN") {
		t.Fatalf("self delete = %d %s", rec.Code, rec.Body.String())
	}
}

func TestLastAdminProtected(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/u1/roles",
		strings.NewReader(`{"roles":["user"]}`))
	req.SetPathValue("id", "u1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.setRoles(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "LAST_ADMIN_PROTECTED") {
		t.Fatalf("last admin demotion = %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteUser(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/u2", nil)
	req.SetPathValue("id", "u2")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.deleteUser(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateUserValidation(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users",
		strings.NewReader(`{"username":"x","password":"short"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.createUser(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("validation = %d %s", rec.Code, rec.Body.String())
	}
}

func TestListUsersRejectsBadPagination(t *testing.T) {
	h, cookie := adminTestSetup(t, []string{"admin"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?max=201", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.listUsers(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pagination = %d %s", rec.Code, rec.Body.String())
	}
}

func TestLocalModeAdminDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Auth.Mode = config.AuthModeLocal
	cfg.Session.CookieName = auth.SessionCookie
	cfg.Session.IdleDuration = time.Hour
	cfg.Session.AbsoluteDuration = time.Hour
	codec, _ := auth.NewCodec("0123456789abcdef0123456789abcdef")
	srv := auth.NewServer(cfg, codec, nil, auth.NewLocalAuthenticator(nil))
	h := NewHandler(cfg, srv.Resolver(), nil, nil)

	rec := httptest.NewRecorder()
	h.listUsers(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("local mode admin = %d, want 404", rec.Code)
	}
}
