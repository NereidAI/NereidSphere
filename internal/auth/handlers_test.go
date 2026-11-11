// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
)

// testConfig builds a config with two product origins for redirect checks.
func testConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Session.CookieName = SessionCookie
	cfg.Auth.Mode = config.AuthModeLocal
	cfg.Products = []config.ProductSection{
		{ID: "link", URL: "http://localhost:8082", Mode: config.ProductModeNS},
		{ID: "forge", URL: "https://forge.example.com", Mode: config.ProductModeNS},
	}
	return cfg
}

// TestSafeRedirectAcceptsAllowlisted verifies exact product origins pass.
func TestSafeRedirectAcceptsAllowlisted(t *testing.T) {
	s := NewServer(testConfig(), nil, nil, nil)
	cases := []string{
		"http://localhost:8082",
		"http://localhost:8082/some/deep?link=1",
		"https://forge.example.com",
		"/dashboard",
		"",
	}
	for _, in := range cases {
		if _, err := s.safeRedirect(in); err != nil {
			t.Errorf("expected %q allowed, got %v", in, err)
		}
	}
}

// TestSafeRedirectRejectsAbuse verifies open-redirect and protocol-relative
// attempts are blocked.
func TestSafeRedirectRejectsAbuse(t *testing.T) {
	s := NewServer(testConfig(), nil, nil, nil)
	cases := []string{
		"http://evil.example.com",
		"http://localhost:8082.evil.com", // suffix attack
		"//evil.example.com",             // protocol relative
		"https://localhost:8082",         // scheme mismatch
		"http://evil.example.com/?next=http://localhost:8082",
	}
	for _, in := range cases {
		if _, err := s.safeRedirect(in); err == nil {
			t.Errorf("expected %q rejected, but allowed", in)
		}
	}
}

// TestRolesFromAccessToken verifies role extraction and built-in filtering.
func TestRolesFromAccessToken(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"realm_access":{"roles":["admin","user","offline_access","default-roles-nereidai"]}}`))
	token := header + "." + payload + ".sig"

	roles := RolesFromAccessToken(token)
	joined := strings.Join(roles, ",")
	if joined != "admin,user" {
		t.Fatalf("roles = %v, want [admin user]", roles)
	}
}

// TestRolesFromAccessTokenMalformed ensures bad tokens yield no roles rather
// than erroring the request.
func TestRolesFromAccessTokenMalformed(t *testing.T) {
	if roles := RolesFromAccessToken("not-a-jwt"); roles != nil {
		t.Fatalf("expected nil roles, got %v", roles)
	}
}

// TestGroupsFromAccessToken verifies suite product groups are extracted and
// non-suite groups (engine defaults, other namespaces) are dropped.
func TestGroupsFromAccessToken(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"groups":["/user","/nereidlink/t1/admins","/nereidforge/t2/operator","/other/x"]}`))
	token := header + "." + payload + ".sig"

	groups := GroupsFromAccessToken(token)
	joined := strings.Join(groups, ",")
	if joined != "/nereidlink/t1/admins,/nereidforge/t2/operator" {
		t.Fatalf("groups = %v, want the two suite product groups", groups)
	}
}

// TestGroupsFromAccessTokenMalformed ensures bad tokens yield no groups.
func TestGroupsFromAccessTokenMalformed(t *testing.T) {
	if groups := GroupsFromAccessToken("not-a-jwt"); groups != nil {
		t.Fatalf("expected nil groups, got %v", groups)
	}
}

// TestLoginEntryAnonymous verifies an unauthenticated visitor is sent to the
// SPA login page, with the safe destination preserved.
func TestLoginEntryAnonymous(t *testing.T) {
	codec, err := NewCodec(testSecret)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	srv := NewServer(testConfig(), codec, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/login?redirect="+url.QueryEscape("http://localhost:8082/x"), nil)
	rec := httptest.NewRecorder()
	srv.loginEntry(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	want := "/login?redirect=" + url.QueryEscape("http://localhost:8082/x")
	if loc := rec.Header().Get("Location"); loc != want {
		t.Fatalf("location = %q, want %q", loc, want)
	}
}

// TestLoginEntryAnonymousNoRedirect verifies bare /auth/login goes to /login.
func TestLoginEntryAnonymousNoRedirect(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	srv := NewServer(testConfig(), codec, nil, nil)

	rec := httptest.NewRecorder()
	srv.loginEntry(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 302 /login", rec.Code, rec.Header().Get("Location"))
	}
}

// TestLoginEntryAuthenticated verifies an existing session jumps straight to
// the allowlisted product instead of showing the login page.
func TestLoginEntryAuthenticated(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	srv := NewServer(testConfig(), codec, nil, nil)

	// Seed a valid session cookie.
	save := httptest.NewRecorder()
	if err := codec.SaveSession(save, CookieOptions{Name: SessionCookie, TTL: time.Hour},
		&SessionData{Identity: Identity{Sub: "u1"}, Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	cookie := save.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet,
		"/auth/login?redirect="+url.QueryEscape("https://forge.example.com/"), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.loginEntry(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://forge.example.com/" {
		t.Fatalf("location = %q, want product URL", loc)
	}
}

// TestLoginEntryRejectsAbuse verifies an off-allowlist destination is refused.
func TestLoginEntryRejectsAbuse(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	srv := NewServer(testConfig(), codec, nil, nil)

	rec := httptest.NewRecorder()
	srv.loginEntry(rec, httptest.NewRequest(http.MethodGet,
		"/auth/login?redirect="+url.QueryEscape("http://evil.example.com"), nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// sessionCookie seeds a valid encrypted session cookie for endpoint tests:
// a suite administrator also assigned to every product, so neither the
// admin bypass nor the default-deny gate interferes with ticket-flow tests.
func sessionCookie(t *testing.T, codec *Codec, cfg *config.Config) *http.Cookie {
	t.Helper()
	groups := make([]string, 0, len(cfg.Products))
	for _, p := range cfg.Products {
		groups = append(groups, "/nereid"+p.ID+"/test-tenant/admin")
	}
	return sessionCookieWithRoles(t, codec, cfg, []string{"admin"}, groups)
}

// sessionCookieWithGroups seeds an ordinary-user session cookie carrying
// exactly the given suite groups, used to test the product-entry gate.
func sessionCookieWithGroups(t *testing.T, codec *Codec, cfg *config.Config, groups []string) *http.Cookie {
	return sessionCookieWithRoles(t, codec, cfg, []string{"user"}, groups)
}

// sessionCookieWithRoles seeds a session cookie with explicit realm roles
// and suite groups, used to test the suite-administrator bypass.
func sessionCookieWithRoles(t *testing.T, codec *Codec, cfg *config.Config, roles, groups []string) *http.Cookie {
	t.Helper()
	now := time.Now()
	rec := httptest.NewRecorder()
	data := &SessionData{
		Identity: Identity{
			Sub:    "u1",
			Email:  "a@b.c",
			Name:   "A",
			Roles:  roles,
			Groups: groups,
		},
		AccessExpiry: now.Add(time.Hour),
		LoginAt:      now,
		LastSeen:     now,
		Expiry:       now.Add(cfg.Session.AbsoluteDuration),
	}
	if err := codec.SaveSession(rec, CookieOptions{Name: SessionCookie, TTL: cfg.Session.AbsoluteDuration}, data); err != nil {
		t.Fatalf("save: %v", err)
	}
	return rec.Result().Cookies()[0]
}

// enterTestConfig returns a validated config with ticket secrets set.
func enterTestConfig() *config.Config {
	cfg := testConfig()
	cfg.Session.AbsoluteDuration = time.Hour
	cfg.Session.IdleDuration = time.Hour
	cfg.Session.Secure = false
	for i := range cfg.Products {
		cfg.Products[i].SSOSecret = "secret-" + cfg.Products[i].ID
		cfg.Products[i].SSOCallbackPath = "/auth/ns-callback"
	}
	return cfg
}

// TestVerifySessionForwardAuth verifies the ingress forward-auth contract:
// 204 with identity headers when authenticated, 401 when not.
func TestVerifySessionForwardAuth(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := enterTestConfig()
	srv := NewServer(cfg, codec, nil, nil)

	anonymous := httptest.NewRecorder()
	srv.verifySession(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anonymous.Code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify", nil)
	req.AddCookie(sessionCookie(t, codec, cfg))
	srv.verifySession(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("X-User-Id") != "u1" {
		t.Fatalf("X-User-Id = %q, want u1", rec.Header().Get("X-User-Id"))
	}
	if rec.Header().Get("X-User-Roles") != "admin" {
		t.Fatalf("X-User-Roles = %q, want admin", rec.Header().Get("X-User-Roles"))
	}
}

// TestEnterProductAnonymous verifies an anonymous user is sent to login.
func TestEnterProductAnonymous(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	srv := NewServer(enterTestConfig(), codec, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/enter/link?to=/x", nil)
	req.SetPathValue("product", "link")
	srv.enterProduct(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/auth/login?redirect=") {
		t.Fatalf("location = %q, want suite login redirect", loc)
	}
}

// TestEnterProductDeniedWithoutAssignment enforces suite default-deny: a
// valid NS session without a /nereid{product}/... group is bounced to the
// portal with a not_assigned error instead of receiving a ticket, even when
// the groups only grant other products.
func TestEnterProductDeniedWithoutAssignment(t *testing.T) {
	cases := []struct {
		name   string
		groups []string
	}{
		{"no groups", nil},
		{"only other products", []string{"/nereidforge/tnt-test/admin"}},
		{"engine default group", []string{"/user"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codec, _ := NewCodec(testSecret)
			cfg := enterTestConfig()
			srv := NewServer(cfg, codec, nil, nil)
			cookie := sessionCookieWithGroups(t, codec, cfg, tc.groups)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/auth/enter/link?to=/x", nil)
			req.SetPathValue("product", "link")
			req.AddCookie(cookie)
			srv.enterProduct(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			loc, err := url.Parse(rec.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse location: %v", err)
			}
			if loc.Path != "/" || loc.Query().Get("product_error") != "not_assigned" ||
				loc.Query().Get("product") != "link" {
				t.Fatalf("location = %q, want portal not_assigned redirect", loc.String())
			}
			if loc.Query().Get("ticket") != "" {
				t.Fatalf("unassigned entry must not mint a ticket: %s", loc.RawQuery)
			}
		})
	}
}

// TestEnterProductAllowedWithAssignment is the positive counterpart: one
// matching group is enough to enter, regardless of other products.
func TestEnterProductAllowedWithAssignment(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := enterTestConfig()
	srv := NewServer(cfg, codec, nil, nil)
	cookie := sessionCookieWithGroups(t, codec, cfg,
		[]string{"/nereidhorizon/myorg/partner", "/nereidlink/tenant-1/read_only"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/enter/link", nil)
	req.SetPathValue("product", "link")
	req.AddCookie(cookie)
	srv.enterProduct(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if !strings.HasPrefix(loc.String(), "http://localhost:8082/auth/ns-callback?") ||
		loc.Query().Get("ticket") == "" {
		t.Fatalf("assigned entry must mint a ticket, got %q", loc.String())
	}
}

// TestEnterProductSuiteAdminBypass keeps the bootstrap administrator from
// being locked out: the realm admin role enters every product without any
// assignment group (products map it to their administrative role). The
// "operator" console role is deliberately not enough.
func TestEnterProductSuiteAdminBypass(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		allow bool
	}{
		{"suite admin no groups", []string{"admin", "user"}, true},
		{"operator is not enough", []string{"operator", "user"}, false},
		{"plain user denied", []string{"user"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codec, _ := NewCodec(testSecret)
			cfg := enterTestConfig()
			srv := NewServer(cfg, codec, nil, nil)
			cookie := sessionCookieWithRoles(t, codec, cfg, tc.roles, nil)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/auth/enter/forge", nil)
			req.SetPathValue("product", "forge")
			req.AddCookie(cookie)
			srv.enterProduct(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			loc, _ := url.Parse(rec.Header().Get("Location"))
			minted := strings.Contains(loc.String(), "/auth/ns-callback") && loc.Query().Get("ticket") != ""
			if tc.allow && !minted {
				t.Fatalf("admin bypass must mint a ticket, got %q", loc.String())
			}
			if !tc.allow && (loc.Query().Get("product_error") != "not_assigned" || minted) {
				t.Fatalf("non-admin without assignment must bounce, got %q", loc.String())
			}
		})
	}
}

// TestEnterThenVerifyTicket is the full cross-product handoff: authenticated
// entry mints a ticket, the product backend exchanges it exactly once.
func TestEnterThenVerifyTicket(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := enterTestConfig()
	srv := NewServer(cfg, codec, nil, nil)
	cookie := sessionCookie(t, codec, cfg)

	// Enter: expect a 302 to the product callback carrying the ticket.
	enter := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/enter/link?to=/dashboard", nil)
	req.SetPathValue("product", "link")
	req.AddCookie(cookie)
	srv.enterProduct(enter, req)

	if enter.Code != http.StatusFound {
		t.Fatalf("enter status = %d, want 302", enter.Code)
	}
	loc, err := url.Parse(enter.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if !strings.HasPrefix(loc.String(), "http://localhost:8082/auth/ns-callback?") {
		t.Fatalf("callback location = %q", loc.String())
	}
	ticket := loc.Query().Get("ticket")
	if ticket == "" || loc.Query().Get("to") != "/dashboard" {
		t.Fatalf("missing ticket/to in %s", loc.RawQuery)
	}

	// Verify: product presents its id/secret and exchanges the ticket.
	body := `{"ticket":"` + ticket + `"}`
	verify := httptest.NewRecorder()
	vreq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/ticket/verify", strings.NewReader(body))
	vreq.Header.Set("Content-Type", "application/json")
	vreq.Header.Set(headerClientID, "link")
	vreq.Header.Set(headerClientSecret, "secret-link")
	srv.verifyTicket(verify, vreq)

	if verify.Code != http.StatusOK {
		t.Fatalf("verify status = %d body = %s", verify.Code, verify.Body.String())
	}
	if !strings.Contains(verify.Body.String(), `"sub":"u1"`) || !strings.Contains(verify.Body.String(), `"to":"/dashboard"`) {
		t.Fatalf("unexpected verify body: %s", verify.Body.String())
	}

	// Replay must fail.
	replay := httptest.NewRecorder()
	rreq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/ticket/verify", strings.NewReader(body))
	rreq.Header.Set("Content-Type", "application/json")
	rreq.Header.Set(headerClientID, "link")
	rreq.Header.Set(headerClientSecret, "secret-link")
	srv.verifyTicket(replay, rreq)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want 400", replay.Code)
	}
}

// TestTicketVerifyCarriesGroups ensures the product tenant/role groups travel
// through the handoff ticket identity to the product backend.
func TestTicketVerifyCarriesGroups(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := enterTestConfig()
	srv := NewServer(cfg, codec, nil, nil)

	ticket, err := srv.tickets.Issue("link", Identity{
		Sub:    "u1",
		Groups: []string{"/nereidlink/t1/admins"},
	}, "/dashboard")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/ticket/verify",
		strings.NewReader(`{"ticket":"`+ticket.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerClientID, "link")
	req.Header.Set(headerClientSecret, "secret-link")
	srv.verifyTicket(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"/nereidlink/t1/admins"`) {
		t.Fatalf("verify body missing groups: %s", rec.Body.String())
	}
}

// TestVerifyTicketRejectsBadSecretAndAudience verifies product auth.
func TestReadyzIncludesReplayStore(t *testing.T) {
	cfg := enterTestConfig()
	cfg.Tickets.ReplayStore = config.ReplayStoreMemory
	srv := NewServer(cfg, nil, nil, nil)

	rec := httptest.NewRecorder()
	srv.readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"replay":"memory"`) {
		t.Fatalf("body = %s, want replay memory", rec.Body.String())
	}
}

func TestReadyzRejectsUnavailableReplayStore(t *testing.T) {
	cfg := enterTestConfig()
	cfg.Tickets.ReplayStore = config.ReplayStoreValkey
	srv := NewServerWithReplay(cfg, nil, nil, nil, failingReplayStore{})

	rec := httptest.NewRecorder()
	srv.readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "NOT_READY") {
		t.Fatalf("body = %s, want NOT_READY", rec.Body.String())
	}
}

func TestVerifyTicketUnavailableReplayStoreFailsClosed(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := enterTestConfig()
	cfg.Tickets.ReplayStore = config.ReplayStoreValkey
	srv := NewServerWithReplay(cfg, codec, nil, nil, failingReplayStore{})
	ticket, err := srv.tickets.Issue("link", Identity{Sub: "u1"}, "/dashboard")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/ticket/verify",
		strings.NewReader(`{"ticket":"`+ticket.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerClientID, "link")
	req.Header.Set(headerClientSecret, "secret-link")
	srv.verifyTicket(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TICKET_STORE_UNAVAILABLE") {
		t.Fatalf("body = %s, want TICKET_STORE_UNAVAILABLE", rec.Body.String())
	}
}

func TestVerifyTicketRejectsBadSecretAndAudience(t *testing.T) {
	codec, _ := NewCodec(testSecret)
	cfg := enterTestConfig()
	srv := NewServer(cfg, codec, nil, nil)
	cookie := sessionCookie(t, codec, cfg)

	enter := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/enter/link", nil)
	req.SetPathValue("product", "link")
	req.AddCookie(cookie)
	srv.enterProduct(enter, req)
	loc, _ := url.Parse(enter.Header().Get("Location"))
	ticket := loc.Query().Get("ticket")

	// Wrong secret -> 401.
	bad := httptest.NewRecorder()
	breq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/ticket/verify",
		strings.NewReader(`{"ticket":"`+ticket+`"}`))
	breq.Header.Set("Content-Type", "application/json")
	breq.Header.Set(headerClientID, "link")
	breq.Header.Set(headerClientSecret, "wrong")
	srv.verifyTicket(bad, breq)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad secret status = %d, want 401", bad.Code)
	}

	// Wrong audience presenting another valid product's secret -> 400.
	other := httptest.NewRecorder()
	oreq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/ticket/verify",
		strings.NewReader(`{"ticket":"`+ticket+`"}`))
	oreq.Header.Set("Content-Type", "application/json")
	oreq.Header.Set(headerClientID, "forge")
	oreq.Header.Set(headerClientSecret, "secret-forge")
	srv.verifyTicket(other, oreq)
	if other.Code != http.StatusBadRequest {
		t.Fatalf("audience mismatch status = %d, want 400", other.Code)
	}
}
