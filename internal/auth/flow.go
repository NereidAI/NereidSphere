// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Authorization-code/PKCE handshake state. The browser is redirected to the
// identity service and back, so the per-login secrets (state, nonce, PKCE
// verifier, return destination) are sealed into a short-lived, HttpOnly
// cookie on the NS origin. The identity service never sees these values
// except through the standard state/nonce/challenge parameters.

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"
)

// FlowCookie is the name of the sealed OIDC handshake cookie.
const FlowCookie = "ns_auth_flow"

// flowTTL bounds the whole browser round trip through the identity login
// page. Ten minutes comfortably covers credential entry plus MFA.
const flowTTL = 10 * time.Minute

// loginFlow is the sealed state of one authorization-code attempt.
type loginFlow struct {
	// State binds the callback to this browser (CSRF on the redirect).
	State string `json:"state"`
	// Nonce binds the id token to this login (replay/injection defence).
	Nonce string `json:"nonce"`
	// Verifier is the S256 PKCE code verifier.
	Verifier string `json:"verifier"`
	// Redirect is the allowlist-checked post-login destination.
	Redirect string `json:"redirect"`
	// Expires hard-caps the handshake cookie independently of Max-Age.
	Expires time.Time `json:"expires"`
}

// randomToken returns n bytes of CSPRNG output as unpadded base64url.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newVerifier returns a 64-byte high-entropy PKCE code verifier.
func newVerifier() (string, error) { return randomToken(64) }

// newState returns the opaque state parameter.
func newState() (string, error) { return randomToken(32) }

// newNonce returns the opaque id-token nonce.
func newNonce() (string, error) { return randomToken(32) }

// startFlow mints a fresh login flow and seals it into the handshake cookie.
func (s *Server) startFlow(w http.ResponseWriter, r *http.Request, redirect string) (*loginFlow, error) {
	state, err := newState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	nonce, err := newNonce()
	if err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	verifier, err := newVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate pkce verifier: %w", err)
	}
	flow := &loginFlow{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Redirect: redirect,
		Expires:  time.Now().Add(flowTTL),
	}
	if err := s.codec.write(w, CookieOptions{
		Name:   FlowCookie,
		Secure: s.cfg.Session.Secure,
		TTL:    flowTTL,
	}, flow); err != nil {
		return nil, fmt.Errorf("seal flow cookie: %w", err)
	}
	return flow, nil
}

// consumeFlow validates the callback state against the sealed cookie and
// returns the flow, expiring the cookie so it cannot be replayed.
func (s *Server) consumeFlow(w http.ResponseWriter, r *http.Request, state string) (*loginFlow, error) {
	var flow loginFlow
	if err := s.codec.read(r, FlowCookie, &flow); err != nil {
		return nil, fmt.Errorf("missing login handshake cookie")
	}
	clear(w, FlowCookie, "")
	if time.Now().After(flow.Expires) {
		return nil, fmt.Errorf("login handshake expired")
	}
	if state == "" || state != flow.State {
		return nil, fmt.Errorf("state mismatch")
	}
	return &flow, nil
}
