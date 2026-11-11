// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Session policy for authenticated requests. The resolver is the single
// place that turns an incoming cookie into a live session: it decrypts the
// cookie, enforces the idle and absolute lifetimes, transparently refreshes
// expired upstream access tokens with the refresh token, and slides the
// idle deadline by re-sealing the cookie. Both the portal API and the
// gateway middleware go through it.

package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
)

// Resolver loads and maintains authenticated sessions.
type Resolver struct {
	tokens TokenManager
	codec  *Codec
	cfg    *config.Config
}

// NewResolver builds the session resolver. tokens is nil for local sessions,
// which carry no upstream refresh tokens.
func NewResolver(tokens TokenManager, codec *Codec, cfg *config.Config) *Resolver {
	return &Resolver{tokens: tokens, codec: codec, cfg: cfg}
}

// Resolve returns the session for the request, refreshing and sliding it
// when needed. The response writer is used to re-seal the cookie; callers
// that only want to read may pass nil, in which case a needed refresh fails
// closed.
func (r *Resolver) Resolve(w http.ResponseWriter, req *http.Request) (*SessionData, error) {
	session, err := r.codec.LoadSession(req, r.cfg.Session.CookieName)
	if err != nil {
		// Drop a present-but-invalid cookie so the client does not replay a
		// dead session forever; do nothing when no cookie was sent at all.
		r.clearIfCookiePresent(w, req)
		return nil, err
	}

	now := time.Now()
	// Absolute deadline is enforced by LoadSession too; the idle deadline is
	// enforced here because only the resolver knows the configured policy.
	if !session.LastSeen.IsZero() && now.Sub(session.LastSeen) > r.cfg.Session.IdleDuration {
		r.clearIfCookiePresent(w, req)
		return nil, ErrNoSession
	}

	// Refresh the upstream access token shortly before/after it expires.
	refreshed := false
	skew := 30 * time.Second
	if !session.AccessExpiry.IsZero() && now.Add(skew).After(session.AccessExpiry) && session.RefreshToken != "" {
		if err := r.refresh(req.Context(), session); err != nil {
			slog.WarnContext(req.Context(), "session refresh failed", "error", err)
			r.clearIfCookiePresent(w, req)
			return nil, ErrNoSession
		}
		refreshed = true
	}

	// Slide the idle deadline. Re-seal at most every quarter of the idle
	// window so active traffic does not rewrite the cookie per request; a
	// token refresh always requires persisting the new tokens.
	previousSeen := session.LastSeen
	session.LastSeen = now
	if w != nil && (refreshed || previousSeen.IsZero() || now.Sub(previousSeen) > r.cfg.Session.IdleDuration/4) {
		if err := r.codec.SaveSession(w, r.cookieOpts(), session); err != nil {
			return nil, err
		}
	}
	return session, nil
}

// refresh exchanges the refresh token and updates the sealed session.
func (r *Resolver) refresh(ctx context.Context, session *SessionData) error {
	if r.tokens == nil {
		return ErrNoSession
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	token, err := r.tokens.Refresh(refreshCtx, session.RefreshToken)
	if err != nil {
		return err
	}
	session.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		session.RefreshToken = token.RefreshToken
	}
	session.AccessExpiry = token.Expiry
	session.Expiry = session.LoginAt.Add(r.cfg.Session.AbsoluteDuration)

	// Re-read roles and suite product groups from the fresh access token in
	// case the assignment changed.
	session.Identity.Roles = RolesFromAccessToken(token.AccessToken)
	session.Identity.Groups = GroupsFromAccessToken(token.AccessToken)
	return nil
}

// cookieOpts builds the session cookie security settings from config.
func (r *Resolver) cookieOpts() CookieOptions {
	return CookieOptions{
		Name:   r.cfg.Session.CookieName,
		Secure: r.cfg.Session.Secure,
		TTL:    r.cfg.Session.AbsoluteDuration,
	}
}

// clearIfCookiePresent expires the cookie only when the request actually
// carried one, avoiding a Set-Cookie on every anonymous request.
func (r *Resolver) clearIfCookiePresent(w http.ResponseWriter, req *http.Request) {
	if w == nil {
		return
	}
	if _, err := req.Cookie(r.cfg.Session.CookieName); err == nil {
		clear(w, r.cfg.Session.CookieName, "")
	}
}
