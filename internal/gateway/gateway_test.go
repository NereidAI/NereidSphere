// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NereidAI/NereidSphere/internal/auth"
	"github.com/NereidAI/NereidSphere/internal/config"
)

// stubResolver returns a fixed session or error.
type stubResolver struct {
	session *auth.SessionData
	err     error
}

func (s stubResolver) Resolve(_ http.ResponseWriter, _ *http.Request) (*auth.SessionData, error) {
	return s.session, s.err
}

// sessionFor builds a session carrying identity.
func sessionFor(sub string) *auth.SessionData {
	return &auth.SessionData{Identity: auth.Identity{Sub: sub, Email: "a@b.c", Name: "A", Roles: []string{"admin"}}}
}

// TestGatewayRequiresSession verifies unauthenticated requests get a 401
// envelope and never reach the upstream.
func TestGatewayRequiresSession(t *testing.T) {
	mw := NewMiddleware(stubResolver{err: errors.New("no session")})
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })

	rec := httptest.NewRecorder()
	mw.RequireSession(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p/link/x", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("upstream must not be called without a session")
	}
}

// TestGatewayInjectsAndStripsIdentity verifies spoofed inbound identity
// headers are removed and replaced with the verified session values.
func TestGatewayInjectsAndStripsIdentity(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		if r.URL.Path != "/api/devices" {
			t.Errorf("upstream path = %q, want /api/devices", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	product := config.ProductSection{ID: "link", URL: upstream.URL}
	proxy := NewReverseProxy(product, 5*time.Second)
	mw := NewMiddleware(stubResolver{session: sessionFor("user-42")})

	req := httptest.NewRequest(http.MethodGet, "/p/link/api/devices", nil)
	req.Header.Set("X-User-Id", "attacker")
	req.Header.Set("X-User-Roles", "root")
	rec := httptest.NewRecorder()
	mw.RequireSession(proxy).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := seen.Get("X-User-Id"); got != "user-42" {
		t.Fatalf("X-User-Id = %q, want user-42", got)
	}
	if got := seen.Get("X-User-Roles"); got != "admin" {
		t.Fatalf("X-User-Roles = %q, want admin", got)
	}
	if got := seen.Get("X-User-Email"); got != "a@b.c" {
		t.Fatalf("X-User-Email = %q", got)
	}
}

// TestGatewayStampsIntegrationSecretOnTenantsPath verifies the product's
// shared secret is attached only to the tenant-list call and that an inbound
// spoofed copy is always stripped.
func TestGatewayStampsIntegrationSecretOnTenantsPath(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	product := config.ProductSection{
		ID:          "link",
		URL:         upstream.URL,
		TenantsPath: "/v1/integration/ns/tenants",
		SSOSecret:   "secret-link",
	}
	proxy := NewReverseProxy(product, 5*time.Second)
	mw := NewMiddleware(stubResolver{session: sessionFor("user-42")})

	// The tenant-list call carries the real secret, replacing any spoofed one.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p/link/v1/integration/ns/tenants", nil)
	req.Header.Set("X-NS-Integration-Secret", "attacker")
	mw.RequireSession(proxy).ServeHTTP(rec, req)
	if got := seen.Get("X-NS-Integration-Secret"); got != "secret-link" {
		t.Fatalf("secret on tenants path = %q, want secret-link", got)
	}

	// Any other path strips the header and does not re-set it.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/p/link/api/devices", nil)
	req.Header.Set("X-NS-Integration-Secret", "attacker")
	mw.RequireSession(proxy).ServeHTTP(rec, req)
	if got := seen.Get("X-NS-Integration-Secret"); got != "" {
		t.Fatalf("secret leaked on non-integration path: %q", got)
	}
}

// TestGatewayForwardingMetadata verifies that the request rewrite keeps the
// original Director-based proxy's routing and forwarding behavior.
func TestGatewayForwardingMetadata(t *testing.T) {
	var seen struct {
		host         string
		rawQuery     string
		forwardedFor string
		userAgent    string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.host = r.Host
		seen.rawQuery = r.URL.RawQuery
		seen.forwardedFor = r.Header.Get("X-Forwarded-For")
		seen.userAgent = r.UserAgent()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	product := config.ProductSection{ID: "link", URL: upstream.URL}
	proxy := NewReverseProxy(product, 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/p/link/api/devices?a=1&a=2", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := seen.host, upstream.Listener.Addr().String(); got != want {
		t.Fatalf("upstream Host = %q, want %q", got, want)
	}
	if got, want := seen.rawQuery, "a=1&a=2"; got != want {
		t.Fatalf("upstream RawQuery = %q, want %q", got, want)
	}
	if got, want := seen.forwardedFor, "198.51.100.10, 192.0.2.1"; got != want {
		t.Fatalf("X-Forwarded-For = %q, want %q", got, want)
	}
	if seen.userAgent != "" {
		t.Fatalf("User-Agent = %q, want empty", seen.userAgent)
	}
}

// TestGatewayUpstreamTimeout verifies a slow upstream surfaces a 504.
func TestGatewayUpstreamTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	product := config.ProductSection{ID: "link", URL: upstream.URL}
	proxy := NewReverseProxy(product, 60*time.Millisecond)
	mw := NewMiddleware(stubResolver{session: sessionFor("u")})

	rec := httptest.NewRecorder()
	mw.RequireSession(proxy).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p/link/", nil))

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
}
