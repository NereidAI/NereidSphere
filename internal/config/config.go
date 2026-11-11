// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package config loads and validates the NereidSphere declarative
// configuration.
//
// NereidSphere is a thin suite layer, so its configuration is limited to
// the HTTP listener, the connection to the external OIDC identity service,
// session/cookie security, and the catalogue of integrated products it
// portals and proxies. Configuration is read from a single YAML file
// selected by the global -c flag, mirroring NereidLink. The built-in Valkey
// password is injected by the NS_VALKEY_PASSWORD environment variable.
package config

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// Config is the root configuration object consumed by the composition root.
type Config struct {
	// Server holds the user-facing HTTP listener settings.
	Server ServerSection `yaml:"server"`

	// Auth selects the authentication backend: AuthModeOIDC delegates the
	// browser login to the external identity service via authorization-code
	// + PKCE; AuthModeLocal (the default/break-glass path) checks static
	// bcrypt accounts from the deployment Secret.
	Auth AuthSection `yaml:"auth"`

	// OIDC holds the single connection block to NereidIdentity when
	// auth.mode is oidc. It is the only coupling to the identity engine;
	// swapping engines changes this block only.
	OIDC OIDCSection `yaml:"oidc"`

	// Session configures the encrypted SSO session cookie.
	Session SessionSection `yaml:"session"`

	// Tickets configures shared single-use ticket state.
	Tickets TicketsSection `yaml:"tickets"`

	// Gateway configures the edge hardening of the unified gateway and the
	// NS-owned HTTP API: CORS, per-client rate limits and upstream timeouts.
	Gateway GatewaySection `yaml:"gateway"`

	// Products lists the integrated suite products surfaced on the portal
	// and proxied by the gateway.
	Products []ProductSection `yaml:"products"`
}

// ServerSection configures the HTTP listener.
type ServerSection struct {
	// Listen is the bind address, e.g. ":8081".
	Listen string `yaml:"listen"`

	// BaseURL is the externally reachable origin of NereidSphere, used to
	// build absolute redirect/post-logout URLs.
	BaseURL string `yaml:"base_url"`

	// Env is the runtime environment: dev or prod. Dev relaxes secure
	// cookie requirements and enables noisy logging.
	Env string `yaml:"env"`
}

// Authentication and product-integration mode constants.
const (
	AuthModeLocal = "local"
	AuthModeOIDC  = "oidc"

	ProductModeNS   = "ns"
	ProductModeOIDC = "oidc"

	// CallbackPath is the fixed NS OIDC redirect URI, registered with the
	// identity service as <server.base_url>/auth/callback.
	CallbackPath = "/auth/callback"

	ReplayStoreMemory = "memory"
	ReplayStoreValkey = "valkey"
)

// OIDCSection configures the connection to the external OpenID Connect
// identity service (NereidIdentity). NS is a pure consumer: it performs the
// browser authorization-code + PKCE flow and, for the admin console, mints
// service-account tokens for the Admin API. Every field is required when
// auth.mode is oidc.
type OIDCSection struct {
	// Issuer is the OIDC issuer URL, e.g.
	// "http://localhost:8090/realms/nereidai" in dev or
	// "https://sso.example.com/auth/realms/nereidai" behind the ingress.
	Issuer string `yaml:"issuer"`

	// ClientID is NS's confidential, authorization-code client identifier.
	ClientID string `yaml:"client_id"`

	// ClientSecret authenticates the code/refresh/revocation requests.
	ClientSecret string `yaml:"client_secret"`

	// AdminClientID is the client-credentials service-account client the
	// admin console uses for the identity service Admin REST API.
	AdminClientID string `yaml:"admin_client_id"`

	// AdminClientSecret is that service account's secret. It is rendered
	// from the deployment Secret and must never be logged.
	AdminClientSecret string `yaml:"admin_client_secret"`

	// Scopes are the OAuth2 scopes requested; openid is always required.
	Scopes []string `yaml:"scopes"`
}

// AuthSection selects the login backend and declares the emergency local
// accounts. Local accounts are bcrypt-hashed static entries from the
// deployment values/Secret; they are a break-glass path, never the default
// in production.
type AuthSection struct {
	// Mode is AuthModeOIDC or AuthModeLocal (default).
	Mode string `yaml:"mode"`

	// LocalUsers are the accounts accepted by the local authenticator.
	LocalUsers []LocalUser `yaml:"local_users"`
}

// LocalUser is one statically configured local account.
type LocalUser struct {
	// Username is the login identifier, unique within the file.
	Username string `yaml:"username"`
	// Name is the display name.
	Name string `yaml:"name"`
	// Email defaults to "<username>@local" when empty.
	Email string `yaml:"email"`
	// PasswordBcrypt is the bcrypt hash of the password. Plaintext is
	// never accepted.
	PasswordBcrypt string `yaml:"password_bcrypt"`
	// Roles are the application roles assigned to the session.
	Roles []string `yaml:"roles"`
}

// SessionSection configures the encrypted SSO session cookie.
type SessionSection struct {
	// SecretKey encrypts session blobs. Must be at least 32 bytes in prod.
	SecretKey string `yaml:"secret_key"`

	// CookieName is the name of the session cookie.
	CookieName string `yaml:"cookie_name"`

	// IdleTTL bounds inactivity: a session whose last activity is older is
	// closed even if the absolute lifetime has not been reached.
	IdleTTL string `yaml:"idle_ttl"`

	// AbsoluteTTL hard-caps the session regardless of activity; the user
	// must sign in again afterwards.
	AbsoluteTTL string `yaml:"absolute_ttl"`

	// TTL is retained as an alias for the idle lifetime; IdleTTL wins.
	TTL string `yaml:"ttl"`

	// Secure marks the cookie Secure. Must be true in prod.
	Secure bool `yaml:"secure"`

	// IdleDuration and AbsoluteDuration are populated by Validate.
	IdleDuration     time.Duration `yaml:"-"`
	AbsoluteDuration time.Duration `yaml:"-"`
	TTLDuration      time.Duration `yaml:"-"`
}

// TicketsSection configures ticket JTI replay storage.
type TicketsSection struct {
	ReplayStore string        `yaml:"replay_store"`
	Valkey      ValkeySection `yaml:"valkey"`
}

// ValkeySection configures a standalone or Sentinel-managed Valkey endpoint.
type ValkeySection struct {
	Address  string          `yaml:"address"`
	Password string          `yaml:"password"`
	DB       int             `yaml:"db"`
	Sentinel SentinelSection `yaml:"sentinel"`

	DialTimeout       string        `yaml:"dial_timeout"`
	OperationTimeout  string        `yaml:"operation_timeout"`
	DialDuration      time.Duration `yaml:"-"`
	OperationDuration time.Duration `yaml:"-"`
}

// SentinelSection configures Redis/Valkey Sentinel discovery.
type SentinelSection struct {
	Addresses  []string `yaml:"addresses"`
	MasterName string   `yaml:"master_name"`
	Password   string   `yaml:"password"`
}

// GatewaySection configures edge hardening shared by the API and the proxy.
type GatewaySection struct {
	// CORSAllowedOrigins lists exact browser origins allowed to call the NS
	// API with credentials. When empty it defaults to every product origin
	// plus the server base URL origin.
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins"`

	// RequestsPerMinute is the general per-client-IP token budget.
	RequestsPerMinute int `yaml:"requests_per_minute"`

	// Burst is the short-term surplus allowed on top of the steady rate.
	Burst int `yaml:"burst"`

	// LoginRequestsPerMinute is the stricter budget for the login endpoint.
	LoginRequestsPerMinute int `yaml:"login_requests_per_minute"`

	// LoginBurst is its short-term surplus.
	LoginBurst int `yaml:"login_burst"`

	// UpstreamTimeout bounds a single proxied product request.
	UpstreamTimeout string `yaml:"upstream_timeout"`

	// UpstreamTimeoutDuration is populated by Validate.
	UpstreamTimeoutDuration time.Duration `yaml:"-"`
}

// ProductSection describes one integrated suite product.
type ProductSection struct {
	// ID is the stable machine identifier used in routes and bootstrap
	// (e.g. "link", "forge", "horizon").
	ID string `yaml:"id"`

	// Name is the human-readable portal title.
	Name string `yaml:"name"`

	// Description is the short portal subtitle.
	Description string `yaml:"description"`

	// URL is the product's own origin; the portal deep-links here and the
	// gateway uses it as the proxy upstream.
	URL string `yaml:"url"`

	// Mode controls how this product joins the suite SSO. ProductModeNS
	// (default) accepts the NS one-time handoff ticket at its own
	// sso_callback_path; ProductModeOIDC is a standalone OIDC client of the
	// identity service. The portal launch URL is always /auth/enter/{id}.
	Mode string `yaml:"mode"`

	// OIDCClientID is the OAuth2 client identifier registered for this
	// product at the external identity service. Declarative metadata only;
	// the identity plane owns the registration.
	OIDCClientID string `yaml:"oidc_client_id"`

	// OIDCRedirectPaths are appended to URL to form the product's redirect
	// URI allowlist registered at the identity service.
	OIDCRedirectPaths []string `yaml:"oidc_redirect_paths"`

	// SSOCallbackPath is the product endpoint that receives the one-time
	// NereidSphere handoff ticket during cross-product SSO.
	SSOCallbackPath string `yaml:"sso_callback_path"`

	// SSOSecret is the shared secret the product's backend presents when
	// exchanging a handoff ticket at the NS verify endpoint (server to
	// server). Required in prod; a dev default is supplied otherwise.
	SSOSecret string `yaml:"sso_secret"`

	// TenantsPath is the product-relative API path that lists tenants for
	// the admin console's product-role picker, e.g.
	// "/v1/integration/ns/tenants", the NS-authenticated tenant-list contract
	// implemented by the suite products. The console fetches it through the gateway at
	// /p/{id}{tenants_path} with the admin's session, so tenants stay owned by
	// the product. When SSOSecret is also set the gateway stamps
	// X-NS-Integration-Secret on exactly this path so the product can
	// authenticate the NS-originated call. Empty hides the tenant picker.
	TenantsPath string `yaml:"tenants_path"`
}

// Load reads, parses and validates the YAML configuration at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Validate checks every section and applies defaults. It is the single
// place where configuration invariants are enforced.
func (c *Config) Validate() error {
	if c.Server.Listen == "" {
		c.Server.Listen = ":8081"
	}
	if c.Server.BaseURL == "" {
		return fmt.Errorf("server.base_url is required")
	}
	if _, err := url.Parse(c.Server.BaseURL); err != nil {
		return fmt.Errorf("server.base_url is not a valid URL: %w", err)
	}
	if c.Server.Env == "" {
		c.Server.Env = "dev"
	}

	if c.Auth.Mode == "" {
		c.Auth.Mode = AuthModeLocal
	}
	if c.Auth.Mode != AuthModeLocal && c.Auth.Mode != AuthModeOIDC {
		return fmt.Errorf("auth.mode must be %q or %q", AuthModeLocal, AuthModeOIDC)
	}

	if c.Auth.Mode == AuthModeOIDC {
		if c.OIDC.Issuer == "" {
			return fmt.Errorf("oidc.issuer is required when auth.mode is oidc")
		}
		if _, err := url.Parse(c.OIDC.Issuer); err != nil {
			return fmt.Errorf("oidc.issuer is not a valid URL: %w", err)
		}
		if c.OIDC.ClientID == "" {
			return fmt.Errorf("oidc.client_id is required when auth.mode is oidc")
		}
		if c.OIDC.ClientSecret == "" {
			return fmt.Errorf("oidc.client_secret is required when auth.mode is oidc")
		}
		if c.OIDC.AdminClientID == "" {
			return fmt.Errorf("oidc.admin_client_id is required when auth.mode is oidc")
		}
		if c.OIDC.AdminClientSecret == "" {
			return fmt.Errorf("oidc.admin_client_secret is required when auth.mode is oidc")
		}
		if len(c.OIDC.Scopes) == 0 {
			c.OIDC.Scopes = []string{"openid", "profile", "email"}
		}
	}

	seenUsers := make(map[string]bool, len(c.Auth.LocalUsers))
	for i := range c.Auth.LocalUsers {
		u := &c.Auth.LocalUsers[i]
		if u.Username == "" {
			return fmt.Errorf("auth.local_users[%d].username is required", i)
		}
		if seenUsers[u.Username] {
			return fmt.Errorf("duplicate local user %q", u.Username)
		}
		seenUsers[u.Username] = true
		if _, err := bcrypt.Cost([]byte(u.PasswordBcrypt)); err != nil {
			return fmt.Errorf("auth.local_users[%s].password_bcrypt is not a bcrypt hash", u.Username)
		}
		if u.Email == "" {
			u.Email = u.Username + "@local"
		}
		if u.Name == "" {
			u.Name = u.Username
		}
		if len(u.Roles) == 0 {
			u.Roles = []string{"user"}
		}
	}

	if len(c.Session.SecretKey) < 32 {
		if c.Server.Env == "prod" {
			return fmt.Errorf("session.secret_key must be at least 32 bytes in prod")
		}
	}
	if c.Session.CookieName == "" {
		c.Session.CookieName = "ns_session"
	}
	if c.Session.IdleTTL == "" {
		c.Session.IdleTTL = c.Session.TTL
	}
	if c.Session.IdleTTL == "" {
		c.Session.IdleTTL = "8h"
	}
	if c.Session.AbsoluteTTL == "" {
		c.Session.AbsoluteTTL = "24h"
	}
	idle, err := time.ParseDuration(c.Session.IdleTTL)
	if err != nil {
		return fmt.Errorf("session.idle_ttl is not a valid duration: %w", err)
	}
	absolute, err := time.ParseDuration(c.Session.AbsoluteTTL)
	if err != nil {
		return fmt.Errorf("session.absolute_ttl is not a valid duration: %w", err)
	}
	if absolute < idle {
		return fmt.Errorf("session.absolute_ttl (%s) must be >= session.idle_ttl (%s)", absolute, idle)
	}
	c.Session.IdleDuration = idle
	c.Session.AbsoluteDuration = absolute
	// TTLDuration is kept as the cookie Max-Age (the absolute lifetime).
	c.Session.TTLDuration = absolute

	if c.Tickets.ReplayStore == "" {
		c.Tickets.ReplayStore = ReplayStoreMemory
	}
	if c.Tickets.ReplayStore != ReplayStoreMemory && c.Tickets.ReplayStore != ReplayStoreValkey {
		return fmt.Errorf("tickets.replay_store must be %q or %q", ReplayStoreMemory, ReplayStoreValkey)
	}
	if c.Tickets.ReplayStore == ReplayStoreValkey {
		if c.Tickets.Valkey.Address == "" && len(c.Tickets.Valkey.Sentinel.Addresses) == 0 {
			return fmt.Errorf("tickets.valkey.address or tickets.valkey.sentinel.addresses is required")
		}
		if c.Tickets.Valkey.Address == "" {
			if c.Tickets.Valkey.Sentinel.MasterName == "" {
				return fmt.Errorf("tickets.valkey.sentinel.master_name is required")
			}
			if len(c.Tickets.Valkey.Sentinel.Addresses) < 3 {
				return fmt.Errorf("tickets.valkey.sentinel.addresses should contain at least three sentinels for production HA")
			}
		}
		if c.Tickets.Valkey.DB < 0 {
			return fmt.Errorf("tickets.valkey.db must be non-negative")
		}
	}
	if c.Tickets.Valkey.DialTimeout == "" {
		c.Tickets.Valkey.DialTimeout = "2s"
	}
	if c.Tickets.Valkey.OperationTimeout == "" {
		c.Tickets.Valkey.OperationTimeout = "1s"
	}
	c.Tickets.Valkey.DialDuration, err = time.ParseDuration(c.Tickets.Valkey.DialTimeout)
	if err != nil {
		return fmt.Errorf("tickets.valkey.dial_timeout is not a valid duration: %w", err)
	}
	c.Tickets.Valkey.OperationDuration, err = time.ParseDuration(c.Tickets.Valkey.OperationTimeout)
	if err != nil {
		return fmt.Errorf("tickets.valkey.operation_timeout is not a valid duration: %w", err)
	}

	if c.Gateway.RequestsPerMinute == 0 {
		c.Gateway.RequestsPerMinute = 300
	}
	if c.Gateway.Burst == 0 {
		c.Gateway.Burst = 60
	}
	if c.Gateway.LoginRequestsPerMinute == 0 {
		c.Gateway.LoginRequestsPerMinute = 10
	}
	if c.Gateway.LoginBurst == 0 {
		c.Gateway.LoginBurst = 5
	}
	if c.Gateway.UpstreamTimeout == "" {
		c.Gateway.UpstreamTimeout = "30s"
	}
	c.Gateway.UpstreamTimeoutDuration, err = time.ParseDuration(c.Gateway.UpstreamTimeout)
	if err != nil {
		return fmt.Errorf("gateway.upstream_timeout is not a valid duration: %w", err)
	}

	seen := make(map[string]bool, len(c.Products))
	for i := range c.Products {
		p := &c.Products[i]
		if p.ID == "" {
			return fmt.Errorf("products[%d].id is required", i)
		}
		if seen[p.ID] {
			return fmt.Errorf("duplicate product id %q", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" {
			p.Name = p.ID
		}
		u, err := url.Parse(p.URL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("products[%s].url must be an absolute URL", p.ID)
		}
		if p.Mode == "" {
			p.Mode = ProductModeNS
		}
		if p.Mode != ProductModeNS && p.Mode != ProductModeOIDC {
			return fmt.Errorf("products[%s].mode must be %q or %q", p.ID, ProductModeNS, ProductModeOIDC)
		}
		if p.OIDCClientID == "" {
			p.OIDCClientID = "nereid" + p.ID
		}
		if len(p.OIDCRedirectPaths) == 0 {
			p.OIDCRedirectPaths = []string{"/auth/callback", "/oidc/callback"}
		}
		if p.SSOCallbackPath == "" {
			p.SSOCallbackPath = "/auth/ns-callback"
		}
		if !strings.HasPrefix(p.SSOCallbackPath, "/") {
			return fmt.Errorf("products[%s].sso_callback_path must start with /", p.ID)
		}
		if p.SSOSecret == "" && p.Mode != ProductModeOIDC {
			if c.Server.Env == "prod" {
				return fmt.Errorf("products[%s].sso_secret is required in prod (or set mode=oidc for standalone NI clients)", p.ID)
			}
			p.SSOSecret = "changeme-" + p.ID
		}
		if p.TenantsPath != "" && !strings.HasPrefix(p.TenantsPath, "/") {
			return fmt.Errorf("products[%s].tenants_path must start with /", p.ID)
		}
	}

	// Default the CORS allowlist to the server and product origins; when an
	// explicit list is supplied, validate every entry is a bare origin.
	if len(c.Gateway.CORSAllowedOrigins) > 0 {
		for _, origin := range c.Gateway.CORSAllowedOrigins {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || u.RawQuery != "" {
				return fmt.Errorf("gateway.cors_allowed_origins entry %q must be a bare scheme://host origin", origin)
			}
		}
	} else {
		origins := map[string]struct{}{}
		if base, err := url.Parse(c.Server.BaseURL); err == nil && base.Scheme != "" {
			origins[base.Scheme+"://"+base.Host] = struct{}{}
		}
		for _, p := range c.Products {
			if u, err := url.Parse(p.URL); err == nil && u.Scheme != "" {
				origins[u.Scheme+"://"+u.Host] = struct{}{}
			}
		}
		for origin := range origins {
			c.Gateway.CORSAllowedOrigins = append(c.Gateway.CORSAllowedOrigins, origin)
		}
		sort.Strings(c.Gateway.CORSAllowedOrigins)
	}

	return nil
}

// RedirectURL builds the absolute OIDC redirect URL from the server base URL.
// It is the single redirect URI registered with the identity service.
func (c *Config) RedirectURL() string {
	return strings.TrimSuffix(c.Server.BaseURL, "/") + CallbackPath
}

// IsDev reports whether the server runs in development mode.
func (c *Config) IsDev() bool {
	return c.Server.Env != "prod"
}

// ProductByID returns the product section for id and whether it exists.
func (c *Config) ProductByID(id string) (ProductSection, bool) {
	for _, p := range c.Products {
		if p.ID == id {
			return p, true
		}
	}
	return ProductSection{}, false
}

// SSOCallbackURL is the absolute product-side URL NS redirects to, carrying
// the one-time handoff ticket.
func (p ProductSection) SSOCallbackURL() string {
	return strings.TrimSuffix(p.URL, "/") + p.SSOCallbackPath
}
