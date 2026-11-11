// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCORSPermitted verifies an allowlisted origin receives credentialed
// CORS headers and preflight succeeds.
func TestCORSPermitted(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := CORSMiddleware([]string{"https://nereidlink.example.com"})(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Origin", "https://nereidlink.example.com")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://nereidlink.example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentials header missing")
	}

	pre := httptest.NewRecorder()
	preReq := httptest.NewRequest(http.MethodOptions, "/api/v1/me", nil)
	preReq.Header.Set("Origin", "https://nereidlink.example.com")
	h.ServeHTTP(pre, preReq)
	if pre.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", pre.Code)
	}
}

// TestCORSRejected verifies unknown origins get no CORS headers and a
// forbidden preflight.
func TestCORSRejected(t *testing.T) {
	h := CORSMiddleware([]string{"https://nereidlink.example.com"})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	pre := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/me", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	h.ServeHTTP(pre, req)

	if pre.Code != http.StatusForbidden {
		t.Fatalf("preflight status = %d, want 403", pre.Code)
	}
	if pre.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unknown origin must not receive ACAO")
	}
}

// TestRateLimiter verifies burst then refill semantics.
func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter(60, 3) // 1/s steady, 3 burst
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !l.allow("ip1", now) {
			t.Fatalf("burst request %d must pass", i+1)
		}
	}
	if l.allow("ip1", now) {
		t.Fatal("fourth immediate request must be rejected")
	}
	// A different client has its own bucket.
	if !l.allow("ip2", now) {
		t.Fatal("buckets must be per client")
	}
}

// TestRateLimitMiddleware verifies the over-budget response is a 429.
func TestRateLimitMiddleware(t *testing.T) {
	l := NewRateLimiter(60, 1)
	h := RateLimitMiddleware(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	r1 := httptest.NewRecorder()
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r1, req)
	h.ServeHTTP(r2, req)
	if r1.Code != http.StatusOK || r2.Code != http.StatusTooManyRequests {
		t.Fatalf("statuses = %d,%d want 200,429", r1.Code, r2.Code)
	}
	if r2.Header().Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}
}

// TestSecurityHeaders verifies the static headers are present on NS paths.
func TestSecurityHeaders(t *testing.T) {
	h := ApplySecurityHeaders(true,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("X-Frame-Options missing")
	}
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS must be set when enabled")
	}

	proxy := httptest.NewRecorder()
	h.ServeHTTP(proxy, httptest.NewRequest(http.MethodGet, "/p/link/x", nil))
	if proxy.Header().Get("X-Frame-Options") != "" {
		t.Fatal("product gateway responses must not carry NS security headers")
	}
}
