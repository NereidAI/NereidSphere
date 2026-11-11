// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"
)

// validBase returns a minimally valid config for mutation tests. It runs in
// local (break-glass) mode; use withOIDC to switch to the identity service.
func validBase() *Config {
	c := &Config{}
	c.Server.BaseURL = "http://localhost:8081"
	c.Server.Env = "dev"
	c.Session.SecretKey = "test-session-key-do-not-use-0000"
	c.Products = []ProductSection{{ID: "link", URL: "http://localhost:8082"}}
	return c
}

// withOIDC switches a config to the authorization-code/PKCE OIDC mode.
func withOIDC(c *Config) *Config {
	c.Auth.Mode = AuthModeOIDC
	c.OIDC.Issuer = "http://localhost:8090/realms/nereidai"
	c.OIDC.ClientID = "nereidsphere"
	c.OIDC.ClientSecret = "changeme"
	c.OIDC.AdminClientID = "nereidsphere-admin"
	c.OIDC.AdminClientSecret = "changeme-admin"
	return c
}

// TestValidateAcceptsBase verifies the minimal config validates and applies
// defaults.
func TestValidateAcceptsBase(t *testing.T) {
	c := validBase()
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if c.Server.Listen != ":8081" {
		t.Fatalf("default listen = %q", c.Server.Listen)
	}
	if c.Auth.Mode != AuthModeLocal {
		t.Fatalf("default auth mode = %q", c.Auth.Mode)
	}
	if c.Products[0].Mode != ProductModeNS {
		t.Fatalf("default product mode = %q", c.Products[0].Mode)
	}
	if c.Session.CookieName != "ns_session" {
		t.Fatalf("default cookie name = %q", c.Session.CookieName)
	}
	if c.Session.IdleDuration.String() != "8h0m0s" {
		t.Fatalf("default idle ttl = %v", c.Session.IdleDuration)
	}
	if c.Session.AbsoluteDuration.String() != "24h0m0s" {
		t.Fatalf("default absolute ttl = %v", c.Session.AbsoluteDuration)
	}
	if c.Session.TTLDuration != c.Session.AbsoluteDuration {
		t.Fatalf("cookie ttl must equal the absolute lifetime")
	}
	if c.Products[0].SSOCallbackPath != "/auth/ns-callback" {
		t.Fatalf("default sso callback path = %q", c.Products[0].SSOCallbackPath)
	}
	if c.Products[0].SSOSecret != "changeme-link" {
		t.Fatalf("dev sso secret default = %q", c.Products[0].SSOSecret)
	}
}

// TestRedirectURL verifies absolute callback construction.
func TestValidateValkeyReplayStore(t *testing.T) {
	c := validBase()
	c.Tickets.ReplayStore = ReplayStoreValkey
	c.Tickets.Valkey.Address = "valkey:6379"
	if err := c.Validate(); err != nil {
		t.Fatalf("standalone valkey config: %v", err)
	}
	if c.Tickets.Valkey.DialDuration.String() != "2s" || c.Tickets.Valkey.OperationDuration.String() != "1s" {
		t.Fatalf("unexpected valkey timeouts: %v/%v", c.Tickets.Valkey.DialDuration, c.Tickets.Valkey.OperationDuration)
	}

	sentinel := validBase()
	sentinel.Tickets.ReplayStore = ReplayStoreValkey
	sentinel.Tickets.Valkey.Sentinel.MasterName = "nereidsphere-valkey"
	sentinel.Tickets.Valkey.Sentinel.Addresses = []string{"sentinel-0:26379", "sentinel-1:26379", "sentinel-2:26379"}
	if err := sentinel.Validate(); err != nil {
		t.Fatalf("sentinel valkey config: %v", err)
	}
}

func TestValidateRejectsInvalidValkeyReplayStore(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "unknown store",
			mutate:  func(c *Config) { c.Tickets.ReplayStore = "redis" },
			wantErr: "replay_store",
		},
		{
			name:    "no endpoint",
			mutate:  func(c *Config) { c.Tickets.ReplayStore = ReplayStoreValkey },
			wantErr: "address",
		},
		{
			name: "sentinel missing master",
			mutate: func(c *Config) {
				c.Tickets.ReplayStore = ReplayStoreValkey
				c.Tickets.Valkey.Sentinel.Addresses = []string{"a:26379", "b:26379", "c:26379"}
			},
			wantErr: "master_name",
		},
		{
			name: "sentinel quorum",
			mutate: func(c *Config) {
				c.Tickets.ReplayStore = ReplayStoreValkey
				c.Tickets.Valkey.Sentinel.MasterName = "nereidsphere-valkey"
				c.Tickets.Valkey.Sentinel.Addresses = []string{"a:26379", "b:26379"}
			},
			wantErr: "three sentinels",
		},
		{
			name: "negative db",
			mutate: func(c *Config) {
				c.Tickets.ReplayStore = ReplayStoreValkey
				c.Tickets.Valkey.Address = "valkey:6379"
				c.Tickets.Valkey.DB = -1
			},
			wantErr: "db",
		},
		{
			name: "bad timeout",
			mutate: func(c *Config) {
				c.Tickets.ReplayStore = ReplayStoreValkey
				c.Tickets.Valkey.Address = "valkey:6379"
				c.Tickets.Valkey.OperationTimeout = "soon"
			},
			wantErr: "operation_timeout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validBase()
			tt.mutate(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRedirectURL(t *testing.T) {
	c := withOIDC(validBase())
	c.Server.BaseURL = "http://localhost:8081/"
	_ = c.Validate()
	if got := c.RedirectURL(); got != "http://localhost:8081/auth/callback" {
		t.Fatalf("redirect url = %q", got)
	}
}

// TestProdRejectsWeakSecret enforces the strong session key in production.
func TestProdRejectsWeakSecret(t *testing.T) {
	c := validBase()
	c.Server.Env = "prod"
	c.Session.SecretKey = "short"
	if err := c.Validate(); err == nil {
		t.Fatal("expected weak secret rejected in prod")
	}
}

// TestProdRequiresSSOSecret enforces per-product ticket secrets in prod.
func TestProdRequiresSSOSecret(t *testing.T) {
	c := validBase()
	c.Server.Env = "prod"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "sso_secret") {
		t.Fatalf("expected sso_secret error in prod, got %v", err)
	}
}

// TestProdOIDCModeProductSkipsSSOSecret covers standalone NI clients: they
// never exchange NS tickets, so no per-product secret is required.
func TestProdOIDCModeProductSkipsSSOSecret(t *testing.T) {
	c := validBase()
	c.Server.Env = "prod"
	c.Products[0].Mode = ProductModeOIDC
	c.Products[0].OIDCClientID = "nereidlink"
	if err := c.Validate(); err != nil {
		t.Fatalf("oidc-mode product must not require sso_secret in prod: %v", err)
	}
}

// TestRejectsDuplicateProduct verifies product ids are unique.
func TestRejectsDuplicateProduct(t *testing.T) {
	c := validBase()
	c.Products = append(c.Products, ProductSection{ID: "link", URL: "http://localhost:9000"})
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate product rejected")
	}
}

// TestRejectsBadProductURL enforces absolute product URLs.
func TestRejectsBadProductURL(t *testing.T) {
	c := validBase()
	c.Products[0].URL = "not-a-url"
	if err := c.Validate(); err == nil {
		t.Fatal("expected bad product url rejected")
	}
}

// TestCORSDefaults verifies the allowlist defaults to the server and product
// origins and that malformed explicit entries are rejected.
func TestCORSDefaults(t *testing.T) {
	c := validBase()
	c.Products[0].URL = "https://nereidlink.example.com"
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	joined := strings.Join(c.Gateway.CORSAllowedOrigins, ",")
	if !strings.Contains(joined, "https://nereidlink.example.com") ||
		!strings.Contains(joined, "http://localhost:8081") {
		t.Fatalf("default CORS origins = %v", c.Gateway.CORSAllowedOrigins)
	}

	bad := validBase()
	bad.Gateway.CORSAllowedOrigins = []string{"https://ok.example.com", "/not-an-origin"}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "cors_allowed_origins") {
		t.Fatalf("expected CORS origin error, got %v", err)
	}
}

// TestMissingIssuerRejected verifies required OIDC fields in oidc mode.
func TestMissingIssuerRejected(t *testing.T) {
	c := withOIDC(validBase())
	c.OIDC.Issuer = ""
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("expected issuer error, got %v", err)
	}
}

// TestOIDCRequiresAdminSecret verifies the admin service-account block.
func TestOIDCRequiresAdminSecret(t *testing.T) {
	c := withOIDC(validBase())
	c.OIDC.AdminClientSecret = ""
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "admin_client_secret") {
		t.Fatalf("expected admin secret error, got %v", err)
	}
}

// TestBadAuthModeRejected rejects unknown auth modes.
func TestBadAuthModeRejected(t *testing.T) {
	c := validBase()
	c.Auth.Mode = "saml"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "auth.mode") {
		t.Fatalf("expected auth.mode error, got %v", err)
	}
}

// TestLocalModeNeedsNoIssuer verifies local mode skips every OIDC field.
func TestLocalModeNeedsNoIssuer(t *testing.T) {
	c := validBase()
	if err := c.Validate(); err != nil {
		t.Fatalf("local mode must validate, got %v", err)
	}
}

// TestLocalUsersValidated covers the local fallback account store.
func TestLocalUsersValidated(t *testing.T) {
	// A valid bcrypt hash is accepted and defaults are filled in.
	c := validBase()
	c.Auth.LocalUsers = []LocalUser{{
		Username:       "admin",
		PasswordBcrypt: "$2a$10$exEAXUhCDksllP4grplAU.Tfg/pJAdLwnvFAIS60pe09U4jGuIwwW",
	}}
	if err := c.Validate(); err != nil {
		t.Fatalf("local user validate: %v", err)
	}
	u := c.Auth.LocalUsers[0]
	if u.Email != "admin@local" || u.Name != "admin" || len(u.Roles) != 1 || u.Roles[0] != "user" {
		t.Fatalf("local user defaults not applied: %+v", u)
	}

	// Plaintext / malformed hashes are rejected.
	bad := validBase()
	bad.Auth.LocalUsers = []LocalUser{{Username: "x", PasswordBcrypt: "plaintext"}}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "password_bcrypt") {
		t.Fatalf("expected bcrypt error, got %v", err)
	}

	// Duplicate usernames are rejected.
	dup := validBase()
	dup.Auth.LocalUsers = []LocalUser{
		{Username: "a", PasswordBcrypt: c.Auth.LocalUsers[0].PasswordBcrypt},
		{Username: "a", PasswordBcrypt: c.Auth.LocalUsers[0].PasswordBcrypt},
	}
	if err := dup.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate user error, got %v", err)
	}
}
