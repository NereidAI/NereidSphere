// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package auth implements NereidSphere's login and session layer.
//
// NereidSphere is the single relying party facing NereidIdentity. The
// browser is redirected to the identity service's own branded login page
// and returns to NS over the standard OAuth2 authorization-code flow with
// PKCE (S256); the NS backend exchanges the code server to server, verifies
// the OIDC id token against the issuer JWKS, and establishes its own sealed
// session cookie. NS never collects the upstream password and never calls an
// IdP-specific management API. The local bcrypt account store is an
// emergency-only fallback selected by auth.mode=local.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// AuthResult is the outcome of a successful credential check: the
// authenticated identity plus the upstream token material the session
// resolver may renew. Local authenticators return zero token fields.
type AuthResult struct {
	Identity     Identity
	IDToken      string
	AccessToken  string
	RefreshToken string
	AccessExpiry time.Time
}

// TokenManager is the seam the session resolver uses to renew and revoke
// upstream tokens. The local backend has no upstream tokens and registers
// nothing here.
type TokenManager interface {
	// Refresh exchanges a refresh token for a fresh access/id token pair.
	Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error)
	// Revoke best-effort invalidates a refresh token at the issuer.
	Revoke(ctx context.Context, refreshToken string) error
	// Ready reports issuer reachability for the readiness probe.
	Ready(ctx context.Context) error
}

// Provider wraps lazy OIDC discovery, the authorization-code/PKCE browser
// flow, token verification and the Admin-API-adjacent endpoints (revocation,
// end session). Discovery happens on first use rather than at startup so a
// temporarily unreachable identity service never wedges NS boot.
type Provider struct {
	cfg        ProviderConfig
	httpClient *http.Client

	mu sync.Mutex
	rt *providerRuntime

	readyState atomic.Pointer[readyProbe]
}

// providerRuntime is the cached result of OIDC discovery.
type providerRuntime struct {
	oidc       *coreoidc.Provider
	oauth      *oauth2.Config
	verifier   *coreoidc.IDTokenVerifier
	endSession string
	revocation string
}

// readyProbe caches the last readiness result to avoid probing the issuer on
// every /readyz call.
type readyProbe struct {
	at    time.Time
	ready bool
}

// ProviderConfig is the relying-party registration used to build Provider.
type ProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// NewProvider constructs the relying-party client without performing any
// network I/O; discovery is deferred to the first login/refresh/ready call.
func NewProvider(cfg ProviderConfig) *Provider {
	scopes := cfg.Scopes
	hasOpenID := false
	for _, s := range scopes {
		if s == coreoidc.ScopeOpenID {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		scopes = append([]string{coreoidc.ScopeOpenID}, scopes...)
	}

	p := &Provider{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	// Keep the normalized scope list in the config used by the lazy
	// runtime so callers cannot bypass the openid requirement.
	p.cfg.Scopes = scopes
	return p
}

// runtime returns the cached discovery result, performing discovery once.
func (p *Provider) runtime(ctx context.Context) (*providerRuntime, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rt != nil {
		return p.rt, nil
	}

	discCtx, cancel := context.WithTimeout(coreoidc.ClientContext(ctx, p.httpClient), 15*time.Second)
	defer cancel()
	oidcProvider, err := coreoidc.NewProvider(discCtx, p.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %q: %w", p.cfg.Issuer, err)
	}
	var doc struct {
		EndSession string `json:"end_session_endpoint"`
		Revocation string `json:"revocation_endpoint"`
	}
	_ = oidcProvider.Claims(&doc)

	p.rt = &providerRuntime{
		oidc: oidcProvider,
		oauth: &oauth2.Config{
			ClientID:     p.cfg.ClientID,
			ClientSecret: p.cfg.ClientSecret,
			Endpoint:     oidcProvider.Endpoint(),
			RedirectURL:  p.cfg.RedirectURL,
			Scopes:       p.cfg.Scopes,
		},
		verifier:   oidcProvider.Verifier(&coreoidc.Config{ClientID: p.cfg.ClientID}),
		endSession: doc.EndSession,
		revocation: doc.Revocation,
	}
	return p.rt, nil
}

// AuthCodeURL starts the authorization-code + PKCE flow: it returns the
// identity-service authorization URL the browser is redirected to. state
// binds the round trip to the sealed NS flow cookie, nonce binds the id
// token to this login, and codeVerifier is the S256 PKCE verifier.
func (p *Provider) AuthCodeURL(ctx context.Context, state, nonce, codeVerifier string) (string, error) {
	rt, err := p.runtime(ctx)
	if err != nil {
		return "", err
	}
	challenge := pkceChallenge(codeVerifier)
	return rt.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	), nil
}

// pkceChallenge returns the BASE64URL(SHA-256(verifier)) S256 challenge.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Exchange performs the server-side code exchange with the PKCE verifier,
// verifies the returned id token (issuer, audience, expiry, nonce and JWKS
// signature) and assembles the identity. Roles are read from the verified
// access token's realm_access claim.
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, nonce string) (*AuthResult, error) {
	rt, err := p.runtime(ctx)
	if err != nil {
		return nil, err
	}

	exCtx, cancel := context.WithTimeout(coreoidc.ClientContext(ctx, p.httpClient), 15*time.Second)
	defer cancel()
	token, err := rt.oauth.Exchange(exCtx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return nil, fmt.Errorf("authorization code exchange: %w", err)
	}

	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		return nil, fmt.Errorf("issuer did not return an id token")
	}
	idToken, err := rt.verifier.Verify(exCtx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	if nonce != "" && idToken.Nonce != nonce {
		return nil, fmt.Errorf("id token nonce mismatch")
	}
	identity, err := identityFromClaims(idToken)
	if err != nil {
		return nil, fmt.Errorf("read identity claims: %w", err)
	}
	identity.Roles = RolesFromAccessToken(token.AccessToken)
	identity.Groups = GroupsFromAccessToken(token.AccessToken)

	return &AuthResult{
		Identity:     identity,
		IDToken:      rawIDToken,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		AccessExpiry: token.Expiry,
	}, nil
}

// Refresh renews the access/id tokens with a refresh token. Used by the
// session resolver to slide a session without prompting the user again.
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token")
	}
	rt, err := p.runtime(ctx)
	if err != nil {
		return nil, err
	}
	refreshCtx, cancel := context.WithTimeout(coreoidc.ClientContext(ctx, p.httpClient), 15*time.Second)
	defer cancel()
	token, err := rt.oauth.TokenSource(refreshCtx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	return token, nil
}

// EndSessionURL builds the RP-initiated logout URL at the identity service.
// postLogoutRedirect must be registered at the identity plane; an empty
// id token hint falls back to client_id, which every tested engine accepts.
func (p *Provider) EndSessionURL(ctx context.Context, idTokenHint, postLogoutRedirect string) (string, error) {
	rt, err := p.runtime(ctx)
	if err != nil {
		return "", err
	}
	if rt.endSession == "" {
		return "", fmt.Errorf("issuer does not publish end_session_endpoint")
	}
	q := url.Values{}
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	} else {
		q.Set("client_id", p.cfg.ClientID)
	}
	q.Set("client_id", p.cfg.ClientID)
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
	}
	return rt.endSession + "?" + q.Encode(), nil
}

// Ready reports whether the issuer discovery endpoint answers, caching a
// successful probe for 10 seconds so readiness stays cheap.
func (p *Provider) Ready(ctx context.Context) error {
	if cached := p.readyState.Load(); cached != nil && time.Since(cached.at) < 10*time.Second {
		if cached.ready {
			return nil
		}
	}

	endpoint := strings.TrimSuffix(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.readyState.Store(&readyProbe{at: time.Now(), ready: false})
		return fmt.Errorf("issuer unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		p.readyState.Store(&readyProbe{at: time.Now(), ready: false})
		// Force a fresh discovery attempt after the issuer recovers.
		p.mu.Lock()
		p.rt = nil
		p.mu.Unlock()
		return fmt.Errorf("issuer discovery returned status %d", resp.StatusCode)
	}
	p.readyState.Store(&readyProbe{at: time.Now(), ready: true})
	return nil
}

var _ TokenManager = (*Provider)(nil)

// Revoke best-effort revokes a refresh token at the issuer revocation
// endpoint during logout, so ending the NS session also invalidates the
// upstream tokens. Errors are returned for logging but are not fatal.
func (p *Provider) Revoke(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	rt, err := p.runtime(ctx)
	if err != nil || rt.revocation == "" {
		return nil
	}

	form := url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {p.cfg.ClientID},
		"client_secret":   {p.cfg.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rt.revocation, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("revocation returned status %d", resp.StatusCode)
	}
	return nil
}
