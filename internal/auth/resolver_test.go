// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
	"golang.org/x/oauth2"
)

// fakeTokens is a controllable TokenManager for resolver tests.
type fakeTokens struct {
	refreshErr error
	calls      int
}

func (f *fakeTokens) Refresh(context.Context, string) (*oauth2.Token, error) {
	f.calls++
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	return &oauth2.Token{AccessToken: "fresh-access", RefreshToken: "fresh-refresh", Expiry: time.Now().Add(time.Hour)}, nil
}
func (f *fakeTokens) Revoke(context.Context, string) error { return nil }
func (f *fakeTokens) Ready(context.Context) error          { return nil }

// resolverCfg builds the minimal config the session resolver reads.
func resolverCfg() *config.Config {
	cfg := &config.Config{}
	cfg.Session.CookieName = SessionCookie
	cfg.Session.IdleDuration = 30 * time.Minute
	cfg.Session.AbsoluteDuration = 8 * time.Hour
	return cfg
}

// seedRequest writes an encrypted session cookie onto a request via a
// recorder, returning the request ready for Resolve.
func seedRequest(t *testing.T, codec *Codec, cfg *config.Config, data *SessionData) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := codec.SaveSession(rec, CookieOptions{Name: SessionCookie, TTL: cfg.Session.AbsoluteDuration}, data); err != nil {
		t.Fatalf("save: %v", err)
	}
	return rec
}

// TestResolverRefreshesExpiredAccessToken verifies transparent refresh and
// cookie re-seal once the upstream access token expires.
func TestResolverRefreshesExpiredAccessToken(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := resolverCfg()
	fp := &fakeTokens{}
	r := NewResolver(fp, codec, cfg)

	now := time.Now()
	save := seedRequest(t, codec, cfg, &SessionData{
		Identity:     Identity{Sub: "u1", Roles: []string{"user"}},
		AccessToken:  "stale",
		RefreshToken: "rt",
		AccessExpiry: now.Add(-time.Minute),
		LoginAt:      now.Add(-time.Hour),
		LastSeen:     now.Add(-5 * time.Minute),
		Expiry:       now.Add(cfg.Session.AbsoluteDuration),
	})
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(save.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	session, err := r.Resolve(rec, req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if session.AccessToken != "fresh-access" {
		t.Fatalf("access token = %q, want refresh result", session.AccessToken)
	}
	if fp.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", fp.calls)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected the refreshed session cookie to be re-sealed")
	}
}

// TestResolverIdleExpiry verifies an inactive session beyond idle TTL dies.
func TestResolverIdleExpiry(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := resolverCfg()
	r := NewResolver(&fakeTokens{}, codec, cfg)

	now := time.Now()
	save := seedRequest(t, codec, cfg, &SessionData{
		Identity:     Identity{Sub: "u1"},
		AccessExpiry: now.Add(time.Hour),
		LoginAt:      now.Add(-2 * time.Hour),
		LastSeen:     now.Add(-time.Hour),
		Expiry:       now.Add(cfg.Session.AbsoluteDuration),
	})
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(save.Result().Cookies()[0])

	if _, err := r.Resolve(httptest.NewRecorder(), req); err != ErrNoSession {
		t.Fatalf("err = %v, want ErrNoSession", err)
	}
}

// TestResolverNoRefreshNeeded verifies a fresh session passes without calling
// the provider or rewriting the cookie within the re-seal window.
func TestResolverNoRefreshNeeded(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := resolverCfg()
	fp := &fakeTokens{}
	r := NewResolver(fp, codec, cfg)

	now := time.Now()
	save := seedRequest(t, codec, cfg, &SessionData{
		Identity:     Identity{Sub: "u1"},
		AccessExpiry: now.Add(time.Hour),
		LoginAt:      now,
		LastSeen:     now,
		Expiry:       now.Add(cfg.Session.AbsoluteDuration),
	})
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(save.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	if _, err := r.Resolve(rec, req); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if fp.calls != 0 {
		t.Fatalf("refresh calls = %d, want 0", fp.calls)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("fresh session must not re-seal the cookie immediately")
	}
}
