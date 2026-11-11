// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
)

// mockIssuer is a minimal OIDC provider for PKCE flow tests: discovery,
// JWKS and a token endpoint that accepts one fixed code and returns a signed
// id token plus a JWT access token carrying realm roles.
type mockIssuer struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string

	mu    sync.Mutex
	nonce string
}

func newMockIssuer(t *testing.T) *mockIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	m := &mockIssuer{key: key, kid: "test-kid"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", m.discovery)
	mux.HandleFunc("/protocol/openid-connect/certs", m.jwks)
	mux.HandleFunc("/protocol/openid-connect/token", m.token)
	mux.HandleFunc("/protocol/openid-connect/logout", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockIssuer) issuer() string { return m.server.URL }

func (m *mockIssuer) setNonce(nonce string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nonce = nonce
}

func (m *mockIssuer) discovery(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"issuer":                                m.issuer(),
		"authorization_endpoint":                m.issuer() + "/protocol/openid-connect/auth",
		"token_endpoint":                        m.issuer() + "/protocol/openid-connect/token",
		"jwks_uri":                              m.issuer() + "/protocol/openid-connect/certs",
		"end_session_endpoint":                  m.issuer() + "/protocol/openid-connect/logout",
		"revocation_endpoint":                   m.issuer() + "/protocol/openid-connect/revoke",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

func (m *mockIssuer) jwks(w http.ResponseWriter, _ *http.Request) {
	e := big.NewInt(int64(m.key.E))
	set := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": m.kid,
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(m.key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(e.Bytes()),
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(set)
}

func (m *mockIssuer) token(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "good-code" {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	if form.Get("code_verifier") == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	nonce := m.nonce
	m.mu.Unlock()

	now := time.Now()
	idClaims := map[string]any{
		"iss":                m.issuer(),
		"aud":                "nereidsphere",
		"sub":                "user-1",
		"nonce":              nonce,
		"email":              "u@example.com",
		"name":               "Test User",
		"preferred_username": "tester",
		"iat":                now.Unix(),
		"exp":                now.Add(time.Hour).Unix(),
	}
	accessClaims := map[string]any{
		"iss":          m.issuer(),
		"aud":          "account",
		"sub":          "user-1",
		"realm_access": map[string]any{"roles": []string{"admin", "user", "offline_access"}},
		"iat":          now.Unix(),
		"exp":          now.Add(time.Hour).Unix(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token":  m.signJWT(accessClaims),
		"id_token":      m.signJWT(idClaims),
		"refresh_token": "refresh-1",
		"token_type":    "Bearer",
		"expires_in":    "3600",
	})
}

// signJWT builds an RS256 JWT signed with the mock key.
func (m *mockIssuer) signJWT(claims map[string]any) string {
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": m.kid}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." +
		base64.RawURLEncoding.EncodeToString(cb)
	sum := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA256, sum[:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// oidcTestConfig builds a validated-looking config for the auth server.
func oidcTestConfig(issuer string) *config.Config {
	cfg := &config.Config{}
	cfg.Auth.Mode = config.AuthModeOIDC
	cfg.OIDC.Issuer = issuer
	cfg.Server.BaseURL = "http://localhost:8081"
	cfg.Server.Env = "dev"
	cfg.Session.CookieName = SessionCookie
	cfg.Session.Secure = false
	cfg.Session.IdleDuration = 30 * time.Minute
	cfg.Session.AbsoluteDuration = time.Hour
	cfg.Products = []config.ProductSection{{
		ID: "link", URL: "http://localhost:8082", Mode: config.ProductModeNS,
		SSOSecret: "secret-link", SSOCallbackPath: "/auth/ns-callback",
	}}
	return cfg
}

func newOIDCTestServer(t *testing.T, cfg *config.Config) (*Server, *Codec, *mockIssuer) {
	t.Helper()
	codec, err := NewCodec(testSecret)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	mock := newMockIssuer(t)
	provider := NewProvider(ProviderConfig{
		Issuer:       mock.issuer(),
		ClientID:     "nereidsphere",
		ClientSecret: "changeme",
		RedirectURL:  "http://localhost:8081/auth/callback",
		Scopes:       []string{"openid", "profile", "email"},
	})
	return NewServer(cfg, codec, provider, nil), codec, mock
}

// TestPKCELoginCallbackEndToEnd drives GET /auth/login through the mock
// identity service callback and verifies the sealed NS session is created.
func TestPKCELoginCallbackEndToEnd(t *testing.T) {
	cfg := oidcTestConfig("")
	srv, codec, mock := newOIDCTestServer(t, cfg)
	cfg.OIDC.Issuer = mock.issuer()

	// GET /auth/login -> 302 to the issuer with state, nonce and S256
	// challenge, plus the sealed flow cookie.
	entry := httptest.NewRecorder()
	srv.loginEntry(entry, httptest.NewRequest(http.MethodGet, "/auth/login?redirect=/", nil))
	if entry.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", entry.Code)
	}
	authURL, err := url.Parse(entry.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if !strings.HasPrefix(authURL.String(), mock.issuer()+"/protocol/openid-connect/auth") {
		t.Fatalf("auth url = %q", authURL.String())
	}
	q := authURL.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		q.Get("state") == "" || q.Get("nonce") == "" {
		t.Fatalf("missing PKCE parameters: %s", authURL.RawQuery)
	}
	// The id token the mock returns must carry the nonce from this flow.
	mock.setNonce(q.Get("nonce"))

	// The handshake cookie is sealed: decrypting it must yield the verifier,
	// while the raw cookie value must not.
	var flowCookie *http.Cookie
	for _, c := range entry.Result().Cookies() {
		if c.Name == FlowCookie {
			flowCookie = c
		}
	}
	if flowCookie == nil {
		t.Fatal("expected sealed flow cookie")
	}
	var flow loginFlow
	if err := codec.open(flowCookie.Value, &flow); err != nil || flow.Verifier == "" {
		t.Fatalf("flow cookie did not decrypt: %v", err)
	}
	if strings.Contains(flowCookie.Value, flow.Verifier) {
		t.Fatal("flow cookie leaked the PKCE verifier")
	}

	// GET /auth/callback with the mock's fixed code and the same state.
	callback := httptest.NewRecorder()
	cbURL := "/auth/callback?code=good-code&state=" + url.QueryEscape(q.Get("state"))
	cbreq := httptest.NewRequest(http.MethodGet, cbURL, nil)
	cbreq.AddCookie(flowCookie)
	srv.oidcCallback(callback, cbreq)

	if callback.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302: %s", callback.Code, callback.Body.String())
	}
	if loc := callback.Header().Get("Location"); loc != "/" {
		t.Fatalf("post-login location = %q, want /", loc)
	}
	var sessionCookie *http.Cookie
	for _, c := range callback.Result().Cookies() {
		if c.Name == SessionCookie {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected sealed session cookie after callback")
	}

	// Verify the established identity through the resolver-facing cookie.
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meReq.AddCookie(sessionCookie)
	me := httptest.NewRecorder()
	srv.Me(me, meReq)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"preferred_username":"tester"`) ||
		!strings.Contains(me.Body.String(), `"admin"`) {
		t.Fatalf("me = %d %s", me.Code, me.Body.String())
	}
}

// TestPKCEBadStateRejected verifies a state mismatch fails the callback and
// burns the flow cookie rather than exchanging the code.
func TestPKCEBadStateRejected(t *testing.T) {
	cfg := oidcTestConfig("")
	srv, _, mock := newOIDCTestServer(t, cfg)
	cfg.OIDC.Issuer = mock.issuer()

	entry := httptest.NewRecorder()
	srv.loginEntry(entry, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	authURL, _ := url.Parse(entry.Header().Get("Location"))
	mock.setNonce(authURL.Query().Get("nonce"))

	callback := httptest.NewRecorder()
	cbreq := httptest.NewRequest(http.MethodGet, "/auth/callback?code=good-code&state=forged", nil)
	for _, c := range entry.Result().Cookies() {
		cbreq.AddCookie(c)
	}
	srv.oidcCallback(callback, cbreq)

	if callback.Code != http.StatusFound ||
		!strings.HasPrefix(callback.Header().Get("Location"), "/login?error=sso_failed") {
		t.Fatalf("callback = %d %q, want redirect to sso_failed", callback.Code, callback.Header().Get("Location"))
	}
}

// TestPKCEDownIssuerRedirectsToLogin verifies an unreachable identity
// service does not 500 the browser: it returns to the SPA login with an
// explicit error so the page can offer the emergency path.
func TestPKCEDownIssuerRedirectsToLogin(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	provider := NewProvider(ProviderConfig{
		Issuer:       "http://127.0.0.1:1/realms/nereidai",
		ClientID:     "nereidsphere",
		ClientSecret: "changeme",
		RedirectURL:  "http://localhost:8081/auth/callback",
	})
	srv := NewServer(oidcTestConfig(provider.cfg.Issuer), codec, provider, nil)

	rec := httptest.NewRecorder()
	srv.loginEntry(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?error=identity_unavailable" {
		t.Fatalf("got %d %q, want identity_unavailable redirect", rec.Code, rec.Header().Get("Location"))
	}
}

// TestOIDCModeDisablesPasswordLogin verifies the password API only exists
// in local mode; in oidc mode the browser uses the identity service page.
func TestOIDCModeDisablesPasswordLogin(t *testing.T) {
	cfg := oidcTestConfig("")
	srv, _, mock := newOIDCTestServer(t, cfg)
	cfg.OIDC.Issuer = mock.issuer()

	rec := httptest.NewRecorder()
	srv.login(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"a","password":"b"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("password login in oidc mode status = %d, want 404", rec.Code)
	}
}

// TestExchangeRejectsNonceMismatch verifies the id token nonce is bound to
// the login flow even when the issuer returns a properly signed token.
func TestExchangeRejectsNonceMismatch(t *testing.T) {
	cfg := oidcTestConfig("")
	srv, _, mock := newOIDCTestServer(t, cfg)
	cfg.OIDC.Issuer = mock.issuer()
	mock.setNonce("real-nonce")

	if _, err := srv.oidc.Exchange(t.Context(), "good-code", "verifier", "different-nonce"); err == nil {
		t.Fatal("expected nonce mismatch error")
	}
}

// TestAuthMethod reports the configured mode for SPA rendering.
func TestAuthMethod(t *testing.T) {
	cfg := oidcTestConfig("")
	srv, _, mock := newOIDCTestServer(t, cfg)
	cfg.OIDC.Issuer = mock.issuer()

	rec := httptest.NewRecorder()
	srv.authMethod(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/method", nil))
	if !strings.Contains(rec.Body.String(), `"mode":"oidc"`) {
		t.Fatalf("method body = %s", rec.Body.String())
	}
}

// TestLogoutBrowserUsesRegisteredPostLogoutRedirect guards the identity
// service's exact-match post-logout redirect requirement: NS must send the
// client origin with no trailing slash, even when server.base_url carries one.
// Regression test for the "We are sorry... Invalid redirect uri" logout page.
func TestLogoutBrowserUsesRegisteredPostLogoutRedirect(t *testing.T) {
	const want = "https://nereidsphere.example.com"
	for _, base := range []string{want, want + "/"} {
		cfg := oidcTestConfig("")
		cfg.Server.BaseURL = base
		srv, _, _ := newOIDCTestServer(t, cfg)

		rec := httptest.NewRecorder()
		srv.logoutBrowser(rec, httptest.NewRequest(http.MethodGet, "/auth/logout", nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("base=%q logout status = %d, want 302", base, rec.Code)
		}
		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatalf("base=%q parse location: %v", base, err)
		}
		if got := loc.Query().Get("post_logout_redirect_uri"); got != want {
			t.Errorf("base=%q post_logout_redirect_uri = %q, want %q", base, got, want)
		}
	}
}
