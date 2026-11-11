// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGroup is the tiny Keycloak group representation the harness serves.
type fakeGroup struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	SubGroups []*fakeGroup `json:"subGroups,omitempty"`
}

// groupView is what Keycloak 26 returns for a group listing: it carries the
// child count but never the children themselves (nested subGroups are only
// reachable through the children endpoint).
type groupView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SubGroupCount int    `json:"subGroupCount"`
}

func viewsOf(groups []*fakeGroup) []groupView {
	views := make([]groupView, 0, len(groups))
	for _, g := range groups {
		views = append(views, groupView{ID: g.ID, Name: g.Name, SubGroupCount: len(g.SubGroups)})
	}
	return views
}

// groupHarness is an in-process Keycloak: token endpoint, GET /groups tree,
// and POST /groups[/{id}/children] with per-call programmed responses.
type groupHarness struct {
	mu     sync.Mutex
	groups []*fakeGroup

	// childPOST returns (status, body); status 201 is used when status == 0.
	// It runs while the harness mutex is held, so tests may mutate groups to
	// simulate a racing winner committing first.
	childPOST func(parent *fakeGroup, name string, call int) (int, string)

	postCalls atomic.Int32
}

func (h *groupHarness) findLocked(id string, gs []*fakeGroup) *fakeGroup {
	for _, g := range gs {
		if g.ID == id {
			return g
		}
		if found := h.findLocked(id, g.SubGroups); found != nil {
			return found
		}
	}
	return nil
}

func (h *groupHarness) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/nereidai/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"t","expires_in":300}`)
	})
	mux.HandleFunc("/admin/realms/nereidai/groups", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(viewsOf(h.groups))
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := decodeName(r)
		call := int(h.postCalls.Add(1))
		status, body := 0, ""
		if h.childPOST != nil {
			status, body = h.childPOST(nil, name, call)
		}
		h.respondLocked(w, name, call, status, body, func(g *fakeGroup) {
			h.groups = append(h.groups, g)
		})
	})
	mux.HandleFunc("/admin/realms/nereidai/groups/", func(w http.ResponseWriter, r *http.Request) {
		const suffix = "/children"
		if !strings.HasSuffix(r.URL.Path, suffix) {
			http.NotFound(w, r)
			return
		}
		parentID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path,
			"/admin/realms/nereidai/groups/"), suffix)
		h.mu.Lock()
		defer h.mu.Unlock()
		parent := h.findLocked(parentID, h.groups)
		if parent == nil {
			http.Error(w, "parent not found", http.StatusNotFound)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(viewsOf(parent.SubGroups))
			return
		}
		name := decodeName(r)
		call := int(h.postCalls.Add(1))
		status, body := 0, ""
		if h.childPOST != nil {
			status, body = h.childPOST(parent, name, call)
		}
		h.respondLocked(w, name, call, status, body, func(g *fakeGroup) {
			parent.SubGroups = append(parent.SubGroups, g)
		})
	})
	return httptest.NewServer(mux)
}

func (h *groupHarness) respondLocked(w http.ResponseWriter, name string, call, status int, body string, commit func(*fakeGroup)) {
	if status == 0 {
		status = http.StatusCreated
	}
	w.Header().Set("Content-Type", "application/json")
	if status != http.StatusCreated && status != http.StatusOK {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
		return
	}
	id := fmt.Sprintf("new-%d", call)
	w.Header().Set("Location", "/admin/realms/nereidai/groups/"+id)
	w.WriteHeader(http.StatusCreated)
	commit(&fakeGroup{ID: id, Name: name})
}

func decodeName(r *http.Request) string {
	raw, _ := io.ReadAll(r.Body)
	var rep struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &rep)
	return rep.Name
}

func newEnsureClient(t *testing.T, h *groupHarness) (*NIAdminClient, func()) {
	t.Helper()
	srv := h.server()
	oldDelay, oldAttempts := conflictAdoptDelay, conflictAdoptAttempts
	conflictAdoptDelay = time.Millisecond
	conflictAdoptAttempts = 10
	c := NewNIAdminClient(srv.URL+"/realms/nereidai", "cid", "sec")
	return c, func() {
		srv.Close()
		conflictAdoptDelay = oldDelay
		conflictAdoptAttempts = oldAttempts
	}
}

// TestEnsureGroupExistsAdopts409 verifies that a group created by a racing
// writer (clean duplicate response) is adopted instead of failing.
func TestEnsureGroupExistsAdopts409(t *testing.T) {
	h := &groupHarness{groups: []*fakeGroup{{ID: "p1", Name: "nereidlink"}}}
	var dupCalls atomic.Int32
	h.childPOST = func(parent *fakeGroup, name string, call int) (int, string) {
		if parent != nil && parent.ID == "p1" && name == "tenant-a" && dupCalls.Add(1) == 1 {
			// The racing winner commits the sibling first.
			parent.SubGroups = append(parent.SubGroups,
				&fakeGroup{ID: "winner-tenant", Name: name})
			return http.StatusConflict, `Sibling group named 'tenant-a' already exists.`
		}
		return 0, ""
	}
	c, cleanup := newEnsureClient(t, h)
	defer cleanup()

	id, err := c.EnsureGroupExists(context.Background(), "/nereidlink/tenant-a/admins")
	if err != nil {
		t.Fatalf("EnsureGroupExists: %v", err)
	}
	if id != "new-2" {
		t.Fatalf("leaf id = %q, want new-2 (admins after adopting tenant)", id)
	}
}

// TestEnsureGroupExistsAdoptsRace500 reproduces the sibling_names 23505 error
// Keycloak returns when two check-then-create transactions race.
func TestEnsureGroupExistsAdoptsRace500(t *testing.T) {
	h := &groupHarness{groups: []*fakeGroup{{ID: "p1", Name: "nereidlink"}}}
	var raced atomic.Int32
	h.childPOST = func(parent *fakeGroup, name string, call int) (int, string) {
		if name == "tenant-b" && raced.Add(1) == 1 {
			parent.SubGroups = append(parent.SubGroups,
				&fakeGroup{ID: "winner-tenant-b", Name: name})
			return http.StatusInternalServerError,
				`duplicate key value violates unique constraint "sibling_names"`
		}
		return 0, ""
	}
	c, cleanup := newEnsureClient(t, h)
	defer cleanup()

	if _, err := c.EnsureGroupExists(context.Background(), "/nereidlink/tenant-b/admins"); err != nil {
		t.Fatalf("EnsureGroupExists: %v", err)
	}
}

// TestEnsureGroupExistsPersistentConflict fails when the conflicting group
// never becomes visible.
func TestEnsureGroupExistsPersistentConflict(t *testing.T) {
	h := &groupHarness{groups: []*fakeGroup{{ID: "p1", Name: "nereidlink"}}}
	h.childPOST = func(parent *fakeGroup, name string, call int) (int, string) {
		return http.StatusConflict, `Sibling group named 'tenant-c' already exists.`
	}
	c, cleanup := newEnsureClient(t, h)
	defer cleanup()

	if _, err := c.EnsureGroupExists(context.Background(), "/nereidlink/tenant-c/admins"); err == nil {
		t.Fatal("expected error when conflicting group never appears")
	}
}

// TestEnsureGroupExistsReusesNestedGroup is the Keycloak 26 regression: the
// realm group listing reports subGroupCount but never expands nested subGroups,
// so an existing assignment group must be discovered through the children
// endpoint instead of being re-created (which made Keycloak answer 500
// sibling_names and NS return 502 IDENTITY_ERROR).
func TestEnsureGroupExistsReusesNestedGroup(t *testing.T) {
	tenant := &fakeGroup{ID: "t1", Name: "00000000-0000-0000-0000-000000000001"}
	tenant.SubGroups = []*fakeGroup{{ID: "r1", Name: "engineers"}}
	h := &groupHarness{groups: []*fakeGroup{
		{ID: "p1", Name: "nereidlink", SubGroups: []*fakeGroup{tenant}},
	}}
	c, cleanup := newEnsureClient(t, h)
	defer cleanup()

	id, err := c.EnsureGroupExists(context.Background(),
		"/nereidlink/00000000-0000-0000-0000-000000000001/engineers")
	if err != nil {
		t.Fatalf("EnsureGroupExists: %v", err)
	}
	if id != "r1" {
		t.Fatalf("leaf id = %q, want r1 (existing group)", id)
	}
	if n := h.postCalls.Load(); n != 0 {
		t.Fatalf("create calls = %d, want 0 when the group already exists", n)
	}
}

// TestFindGroupByPathResolvesNestedGroup guards the removal path, which must
// resolve nested suite groups even though the top-level listing hides them.
func TestFindGroupByPathResolvesNestedGroup(t *testing.T) {
	tenant := &fakeGroup{ID: "t1", Name: "tenant-a"}
	tenant.SubGroups = []*fakeGroup{{ID: "r1", Name: "admins"}}
	h := &groupHarness{groups: []*fakeGroup{
		{ID: "p1", Name: "nereidlink", SubGroups: []*fakeGroup{tenant}},
	}}
	c, cleanup := newEnsureClient(t, h)
	defer cleanup()

	id, err := c.FindGroupByPath(context.Background(), "/nereidlink/tenant-a/admins")
	if err != nil {
		t.Fatalf("FindGroupByPath: %v", err)
	}
	if id != "r1" {
		t.Fatalf("id = %q, want r1", id)
	}
	if _, err := c.FindGroupByPath(context.Background(), "/nereidlink/missing/admins"); err == nil {
		t.Fatal("expected error for a missing path")
	}
}

func TestIsSiblingCreateConflict(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{http.StatusConflict, `Sibling group named 'x' already exists.`, true},
		{http.StatusInternalServerError, `duplicate key value violates unique constraint "sibling_names"`, true},
		{http.StatusInternalServerError, "ERROR: duplicate key value ...", true},
		{http.StatusInternalServerError, "some unrelated engine failure", false},
		{http.StatusForbidden, "forbidden", false},
	}
	for _, tc := range cases {
		if got := isSiblingCreateConflict(tc.status, []byte(tc.body)); got != tc.want {
			t.Errorf("isSiblingCreateConflict(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}
