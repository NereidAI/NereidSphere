// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testSecret is a deterministic 32-byte key for codec tests.
const testSecret = "test-session-key-do-not-use-0000"

// TestCodecRoundTrip verifies a sealed session survives save/load.
func TestCodecRoundTrip(t *testing.T) {
	codec, err := NewCodec(testSecret)
	if err != nil {
		t.Fatalf("new codec: %v", err)
	}

	want := &SessionData{
		Identity:    Identity{Sub: "user-1", Email: "a@b.c", Roles: []string{"admin"}},
		AccessToken: "access",
		Expiry:      time.Now().Add(time.Hour),
	}

	rec := httptest.NewRecorder()
	if err := codec.SaveSession(rec, CookieOptions{Secure: false, TTL: time.Hour}, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	resp := rec.Result()
	if len(resp.Cookies()) == 0 {
		t.Fatal("no session cookie set")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(resp.Cookies()[0])
	got, err := codec.LoadSession(req, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Identity.Sub != want.Identity.Sub || got.AccessToken != "access" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Identity.Roles) != 1 || got.Identity.Roles[0] != "admin" {
		t.Fatalf("roles mismatch: %v", got.Identity.Roles)
	}
}

// TestCodecRejectsTampering ensures modified ciphertext fails authentication.
func TestCodecRejectsTampering(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	rec := httptest.NewRecorder()
	if err := codec.SaveSession(rec, CookieOptions{TTL: time.Hour}, &SessionData{
		Identity: Identity{Sub: "x"}, Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	c := rec.Result().Cookies()[0]
	// Flip a character in the sealed value.
	if len(c.Value) < 4 {
		t.Fatal("cookie value too short")
	}
	tampered := c.Value[:2] + "AA" + c.Value[4:]
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: c.Name, Value: tampered})
	if _, err := codec.LoadSession(req, c.Name); err == nil {
		t.Fatal("expected decryption failure for tampered cookie")
	}
}

// TestCodecWrongKey ensures data sealed with one key cannot be opened by
// another.
func TestCodecWrongKey(t *testing.T) {
	a, _ := NewCodec(testSecret)
	b, _ := NewCodec("ffffffffffffffffffffffffffffffff")
	rec := httptest.NewRecorder()
	_ = a.SaveSession(rec, CookieOptions{TTL: time.Hour}, &SessionData{
		Identity: Identity{Sub: "x"}, Expiry: time.Now().Add(time.Hour),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	if _, err := b.LoadSession(req, ""); err == nil {
		t.Fatal("expected failure opening with a different key")
	}
}

// TestSessionExpiry verifies expired sessions are rejected.
func TestSessionExpiry(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	rec := httptest.NewRecorder()
	_ = codec.SaveSession(rec, CookieOptions{TTL: time.Hour}, &SessionData{
		Identity: Identity{Sub: "x"}, Expiry: time.Now().Add(-time.Minute),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	if _, err := codec.LoadSession(req, ""); err != ErrNoSession {
		t.Fatalf("expected ErrNoSession for expired, got %v", err)
	}
}

// TestSessionWithUpstreamTokensFitsCookieLimit reproduces the OIDC session,
// which bundles three realistically-sized Keycloak JWTs: the raw sealed
// cookie used to exceed the browser per-cookie ceiling of 4096 bytes and was
// silently dropped, leaving the SPA stuck on the login page.
func TestSessionWithUpstreamTokensFitsCookieLimit(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	access := makeFakeJWT(t, 1100, map[string]any{"typ": "Bearer", "azp": "nereidsphere"})
	id := makeFakeJWT(t, 1120, map[string]any{"typ": "ID"})
	refresh := makeFakeJWT(t, 600, map[string]any{"typ": "Refresh"})

	session := &SessionData{
		Identity:     Identity{Sub: "53ceeafc-cb22-4fc0-a4fd-6e67be2f8e40", Email: "a@b.c", Name: "A B", Username: "admin", Roles: []string{"admin", "user"}},
		AccessToken:  access,
		IDToken:      id,
		RefreshToken: refresh,
		Expiry:       time.Now().Add(time.Hour),
	}
	rec := httptest.NewRecorder()
	if err := codec.SaveSession(rec, CookieOptions{TTL: time.Hour}, session); err != nil {
		t.Fatalf("save: %v", err)
	}
	value := rec.Result().Cookies()[0].Value
	if len(value) >= 4000 {
		t.Fatalf("session cookie value is %d bytes, want < 4000 to stay under the 4096 browser limit", len(value))
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: value})
	got, err := codec.LoadSession(req, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.IDToken != id || got.RefreshToken != refresh || got.AccessToken != access {
		t.Fatal("tokens did not survive the compressed round trip")
	}
}

// makeFakeJWT builds a three-part base64url token close to the real Keycloak
// size target: shared JSON scaffolding across access/id/refresh tokens is
// what makes deflate effective.
func makeFakeJWT(t *testing.T, target int, extra map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "C3pXq27wFfKz9vNbQ1sTe"}
	body := map[string]any{
		"exp":                1791234567,
		"iat":                1791231234,
		"auth_time":          1791231200,
		"jti":                strings.Repeat("d2f1a09b-", 4) + "44",
		"iss":                "http://localhost:8090/realms/nereidai",
		"aud":                "account",
		"sub":                "53ceeafc-cb22-4fc0-a4fd-6e67be2f8e40",
		"typ":                "Bearer",
		"azp":                "nereidsphere",
		"sid":                "7a25714f-58f6-40a6-88f9-edda4fe3441b",
		"email":              "admin@nereidai.local",
		"name":               "Admin User",
		"preferred_username": "admin",
		"scope":              "openid profile email",
	}
	for k, v := range extra {
		body[k] = v
	}
	h := encodeJWTPart(t, header)
	b := encodeJWTPart(t, body)
	sig := strings.Repeat("aB3", 114)
	token := h + "." + b + "." + sig
	// Grow the body with realistic claim-shaped content (as Keycloak does
	// with realm_access/roles) until the target token size is reached.
	n := 1
	for len(token) < target {
		n++
		body["filler"] = strings.Repeat("realm-role-claim-entry,", n)
		b = encodeJWTPart(t, body)
		token = h + "." + b + "." + sig
	}
	return token
}

func encodeJWTPart(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// sealLegacy seals without the compression prefix, like pre-upgrade builds.
func (c *Codec) sealLegacy(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := c.gcm.Seal(nonce, nonce, raw, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// TestCodecReadsLegacyUncompressedSealed ensures sessions sealed before
// compression (plaintext JSON behind AEAD) still open after upgrade.
func TestCodecReadsLegacyUncompressedSealed(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	want := &SessionData{Identity: Identity{Sub: "legacy"}, AccessToken: "at"}
	sealed, err := codec.sealLegacy(want)
	if err != nil {
		t.Fatalf("seal legacy: %v", err)
	}
	var got SessionData
	if err := codec.open(sealed, &got); err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	if got.Identity.Sub != "legacy" || got.AccessToken != "at" {
		t.Fatalf("legacy payload mismatch: %+v", got)
	}
}

// TestNewCodecRejectsBadKeyLength enforces the 32-byte AES-256 key.
func TestNewCodecRejectsBadKeyLength(t *testing.T) {
	if _, err := NewCodec("short"); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
}

// TestRolesHeader verifies the comma-joined downstream header value.
func TestRolesHeader(t *testing.T) {
	s := &SessionData{Identity: Identity{Roles: []string{"admin", "user"}}}
	if got := s.RolesHeader(); got != "admin,user" {
		t.Fatalf("roles header = %q", got)
	}
	if strings.Contains(s.RolesHeader(), " ") {
		t.Fatal("roles header must not contain spaces")
	}
}
