// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NereidAI/NereidSphere/internal/auth"
	"github.com/NereidAI/NereidSphere/internal/config"
)

func portalTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Session.CookieName = "ns_session"
	cfg.Session.AbsoluteDuration = time.Hour
	cfg.Session.IdleDuration = time.Hour
	cfg.Products = []config.ProductSection{
		{ID: "link", URL: "http://localhost:8082", Mode: config.ProductModeNS},
		{ID: "forge", URL: "https://forge.example.com", Mode: config.ProductModeNS},
	}
	return cfg
}

func sessionCookie(t *testing.T, cfg *config.Config, groups []string) *http.Cookie {
	return sessionCookieAs(t, cfg, nil, groups)
}

func sessionCookieAs(t *testing.T, cfg *config.Config, roles, groups []string) *http.Cookie {
	t.Helper()
	codec, err := auth.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	now := time.Now()
	data := &auth.SessionData{
		Identity:     auth.Identity{Sub: "u1", Email: "a@b.c", Roles: roles, Groups: groups},
		AccessExpiry: now.Add(time.Hour),
		LoginAt:      now,
		LastSeen:     now,
		Expiry:       now.Add(time.Hour),
	}
	rec := httptest.NewRecorder()
	if err := codec.SaveSession(rec, auth.CookieOptions{Name: cfg.Session.CookieName, TTL: time.Hour}, data); err != nil {
		t.Fatalf("save: %v", err)
	}
	return rec.Result().Cookies()[0]
}

func TestListProductsAssignmentFlags(t *testing.T) {
	cfg := portalTestConfig()
	codec, err := auth.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	h := NewHandler(cfg, auth.NewResolver(nil, codec, cfg))

	get := func(cookie *http.Cookie) []productView {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		h.listProducts(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var views []productView
		if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return views
	}

	flag := func(views []productView, id string) bool {
		for _, v := range views {
			if v.ID == id {
				return v.Assigned
			}
		}
		t.Fatalf("product %s missing", id)
		return false
	}

	// Anonymous: nothing assigned.
	anon := get(nil)
	if flag(anon, "link") || flag(anon, "forge") {
		t.Fatalf("anonymous catalogue must show unassigned: %+v", anon)
	}

	// Session with only a link assignment: only link is assigned.
	assigned := get(sessionCookie(t, cfg, []string{"/nereidlink/tenant-1/admins"}))
	if !flag(assigned, "link") {
		t.Fatalf("link should be assigned: %+v", assigned)
	}
	if flag(assigned, "forge") {
		t.Fatalf("forge should be unassigned: %+v", assigned)
	}

	// Other-product and engine-default groups never grant entry.
	none := get(sessionCookie(t, cfg, []string{"/nereidhorizon/org/partner", "/user"}))
	if flag(none, "link") || flag(none, "forge") {
		t.Fatalf("foreign groups must not assign: %+v", none)
	}

	// Suite administrator implicitly has every product, even with no
	// groups; the operator role alone does not.
	admin := get(sessionCookieAs(t, cfg, []string{"admin", "user"}, nil))
	if !flag(admin, "link") || !flag(admin, "forge") {
		t.Fatalf("suite admin must be assigned to every product: %+v", admin)
	}
	operator := get(sessionCookieAs(t, cfg, []string{"operator", "user"}, nil))
	if flag(operator, "link") || flag(operator, "forge") {
		t.Fatalf("operator without assignment must stay unassigned: %+v", operator)
	}
}
