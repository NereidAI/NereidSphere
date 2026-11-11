// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Auth HTTP endpoints. In oidc mode the browser is redirected from
// /auth/login to the identity service's own login page and back to
// /auth/callback over authorization-code + PKCE; NS then sets the sealed
// session cookie. In local (emergency) mode the React login page posts
// credentials to /api/v1/auth/login. Cross-product entry uses one-time
// handoff tickets (mode ns) or direct redirection to a standalone OIDC
// product (mode oidc); browser-visible upstream tokens never appear.

package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
	"github.com/NereidAI/NereidSphere/internal/httpx"
)

// Header names used by the server-to-server ticket exchange.
const (
	headerClientID     = "X-NS-Client-Id"
	headerClientSecret = "X-NS-Client-Secret"
)

// Server exposes the SSO HTTP endpoints.
type Server struct {
	mode  string
	oidc  *Provider
	local *LocalAuthenticator

	codec    *Codec
	resolver *Resolver
	tickets  *TicketStore
	cfg      *config.Config
	// loginLimit wraps only the local login handler with a strict limiter.
	loginLimit func(http.Handler) http.Handler
	// allowedRedirects maps "scheme://host" accepted as post-login
	// destinations, derived from the configured product origins.
	allowedRedirects map[string]bool
}

// NewServer constructs the auth server. In oidc mode oidcProvider is set and
// localAuth is nil, and vice versa in local mode.
func NewServer(cfg *config.Config, codec *Codec, oidcProvider *Provider, localAuth *LocalAuthenticator) *Server {
	return NewServerWithReplay(cfg, codec, oidcProvider, localAuth, nil)
}

// NewServerWithReplay constructs an auth server with a shared ticket replay
// store. nil selects the process-local development store.
func NewServerWithReplay(cfg *config.Config, codec *Codec, oidcProvider *Provider, localAuth *LocalAuthenticator, replay ReplayStore) *Server {
	allowed := make(map[string]bool)
	for _, p := range cfg.Products {
		if u, err := url.Parse(p.URL); err == nil {
			allowed[u.Scheme+"://"+u.Host] = true
		}
	}
	var tm TokenManager
	if oidcProvider != nil {
		tm = oidcProvider
	}
	return &Server{
		mode:             cfg.Auth.Mode,
		oidc:             oidcProvider,
		local:            localAuth,
		codec:            codec,
		resolver:         NewResolver(tm, codec, cfg),
		tickets:          NewTicketStoreWithReplay(codec, TicketTTL, replay),
		cfg:              cfg,
		allowedRedirects: allowed,
	}
}

// Resolver exposes the session resolver so the gateway reuses the same
// idle/refresh policy as the portal API.
func (s *Server) Resolver() *Resolver { return s.resolver }

// ReplayStore exposes the ticket single-use store for readiness checks.
func (s *Server) ReplayStore() ReplayStore { return s.tickets.ReplayStore() }

// CSRFMiddleware enforces the double-submit cookie on browser-driven
// mutating API calls. The server-to-server ticket exchange authenticates
// with per-product credentials and is exempt.
func (s *Server) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(CSRFCookie); err != nil {
			if token, tokenErr := randomToken(32); tokenErr == nil {
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookie,
					Value:    token,
					Path:     "/",
					HttpOnly: false,
					Secure:   s.cfg.Session.Secure,
					SameSite: http.SameSiteLaxMode,
				})
			}
		}
		if s.requiresCSRF(r) {
			cookie, err := r.Cookie(CSRFCookie)
			header := r.Header.Get(CSRFHeader)
			if err != nil || cookie.Value == "" || header == "" ||
				subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
				httpx.WriteError(w, r, http.StatusForbidden, "CSRF_FAILED", "missing or invalid CSRF token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requiresCSRF reports whether a request must carry the double-submit token.
func (s *Server) requiresCSRF(r *http.Request) bool {
	if !csrfProtectedMethods[r.Method] {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	// S2S product backends authenticate with X-NS-Client-Id/Secret.
	return r.URL.Path != "/api/v1/auth/ticket/verify"
}

// WithLoginRateLimit wraps the local login handler with an extra, stricter
// limiter (defence in depth against credential stuffing).
func (s *Server) WithLoginRateLimit(mw func(http.Handler) http.Handler) *Server {
	s.loginLimit = mw
	return s
}

// Routes registers the auth endpoints on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	// Suite SSO entry: starts the NI redirect in oidc mode, or the local
	// login page in local mode.
	mux.HandleFunc("GET /auth/login", s.loginEntry)
	mux.HandleFunc("GET /auth/callback", s.oidcCallback)
	mux.HandleFunc("GET /auth/logout", s.logoutBrowser)
	// Cross-product SSO entry: mints a one-time ticket (mode ns) or sends
	// the browser to a standalone OIDC product (mode oidc).
	mux.HandleFunc("GET /auth/enter/{product}", s.enterProduct)
	// Server-to-server ticket exchange called by product backends.
	mux.HandleFunc("POST /api/v1/auth/ticket/verify", s.verifyTicket)
	loginHandler := http.HandlerFunc(s.login)
	var finalLoginHandler http.Handler = loginHandler
	if s.loginLimit != nil {
		finalLoginHandler = s.loginLimit(loginHandler)
	}
	mux.Handle("POST /api/v1/auth/login", finalLoginHandler)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	// Login-method discovery for the SPA.
	mux.HandleFunc("GET /api/v1/auth/method", s.authMethod)
	mux.HandleFunc("GET /api/v1/me", s.Me)
	// Forward-auth decision endpoint for ingress-nginx auth_request /
	// Traefik forwardAuth: 204 carrying identity headers on success, 401
	// otherwise. The ingress forwards the listed headers to the product.
	mux.HandleFunc("GET /api/v1/auth/verify", s.verifySession)
	mux.HandleFunc("GET /readyz", s.readyz)
}

// loginRequest is the login form payload posted by the NS login page in
// local (emergency) mode.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Redirect string `json:"redirect"`
}

// loginResponse returns the established identity and the safe destination.
type loginResponse struct {
	Identity Identity `json:"identity"`
	Redirect string   `json:"redirect"`
}

// authMethod tells the SPA which login experience to render.
func (s *Server) authMethod(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"mode": s.mode})
}

// loginEntry is the suite SSO entry URL. With a valid session it jumps
// straight to the allowlist-checked destination; otherwise it starts the
// authorization-code/PKCE redirect (oidc mode) or shows the local login
// page (local mode).
func (s *Server) loginEntry(w http.ResponseWriter, r *http.Request) {
	redirect, err := s.safeRedirect(r.URL.Query().Get("redirect"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_REDIRECT", err.Error())
		return
	}

	if _, sessionErr := s.resolver.Resolve(nil, r); sessionErr == nil {
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}

	if s.mode != config.AuthModeOIDC {
		target := "/login"
		if redirect != "/" {
			target = "/login?redirect=" + url.QueryEscape(redirect)
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	flow, err := s.startFlow(w, r, redirect)
	if err != nil {
		slog.ErrorContext(r.Context(), "start oidc flow failed", "error", err)
		http.Redirect(w, r, "/login?error=identity_unavailable", http.StatusFound)
		return
	}
	authURL, err := s.oidc.AuthCodeURL(r.Context(), flow.State, flow.Nonce, flow.Verifier)
	if err != nil {
		slog.ErrorContext(r.Context(), "oidc authorization url failed", "error", err)
		clear(w, FlowCookie, "")
		http.Redirect(w, r, "/login?error=identity_unavailable", http.StatusFound)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// oidcCallback handles the browser return from the identity service: it
// validates state against the sealed flow cookie, exchanges the code with
// the PKCE verifier, verifies the id token and establishes the NS session.
func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(reason string) {
		clear(w, FlowCookie, "")
		loc := "/login?error=" + reason
		http.Redirect(w, r, loc, http.StatusFound)
	}

	if s.mode != config.AuthModeOIDC {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "oidc mode disabled")
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		slog.InfoContext(r.Context(), "identity service returned an error",
			"error", errParam, "description", r.URL.Query().Get("error_description"))
		fail("sso_denied")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		fail("sso_failed")
		return
	}
	flow, err := s.consumeFlow(w, r, state)
	if err != nil {
		slog.InfoContext(r.Context(), "oidc callback rejected", "error", err)
		fail("sso_failed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.oidc.Exchange(ctx, code, flow.Verifier, flow.Nonce)
	if err != nil {
		slog.InfoContext(r.Context(), "oidc token exchange failed", "error", err,
			"trace_id", httpx.TraceID(r.Context()))
		fail("sso_failed")
		return
	}

	if err := s.establishSession(w, r, result); err != nil {
		slog.ErrorContext(r.Context(), "save session failed", "error", err)
		fail("sso_failed")
		return
	}
	destination := flow.Redirect
	if destination == "" {
		destination = "/"
	}
	http.Redirect(w, r, destination, http.StatusFound)
}

// enterProduct mints a one-time handoff ticket for one mode-ns product and
// redirects the browser to that product's NS callback. Mode-oidc products
// run their own OIDC code flow against the identity service, so the browser
// is sent straight to the product (the NI SSO cookie makes this seamless).
// Unauthenticated visitors are sent to the login entry and returned here.
func (s *Server) enterProduct(w http.ResponseWriter, r *http.Request) {
	productID := r.PathValue("product")
	product, ok := s.cfg.ProductByID(productID)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, "UNKNOWN_PRODUCT", "unknown product "+productID)
		return
	}

	session, err := s.resolver.Resolve(w, r)
	if err != nil {
		returnTo := r.URL.Path
		if to := r.URL.Query().Get("to"); to != "" {
			returnTo += "?to=" + url.QueryEscape(to)
		}
		http.Redirect(w, r, "/auth/login?redirect="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	_ = session

	// `to` is a path inside the product, never an absolute URL: the product
	// host is fixed by configuration, which keeps the redirect unforgeable.
	to := r.URL.Query().Get("to")
	if !safeProductPath(to) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_REDIRECT", "to must be a relative path")
		return
	}

	// Default-deny product authorization: an NS session only proves the
	// identity. Entry into a product requires an explicit suite assignment
	// (/nereid{product}/{tenant}/{role}) or the suite administrator role,
	// which implicitly administers every product so the bootstrap admin is
	// never locked out of a fresh deployment. The portal greys unassigned
	// products out; this entry is the enforcement boundary for hand-typed
	// URLs.
	if !CanEnterProduct(session.Identity.Groups, session.Identity.Roles, product.ID) {
		slog.InfoContext(r.Context(), "product entry denied: no assignment",
			"sub", session.Identity.Sub, "product", product.ID)
		q := url.Values{}
		q.Set("product_error", "not_assigned")
		q.Set("product", product.ID)
		http.Redirect(w, r, "/?"+q.Encode(), http.StatusFound)
		return
	}

	if product.Mode == config.ProductModeOIDC {
		target := strings.TrimSuffix(product.URL, "/")
		if to != "" {
			target += to
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	ticket, err := s.tickets.Issue(product.ID, session.Identity, to)
	if err != nil {
		slog.ErrorContext(r.Context(), "issue ticket failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "TICKET", "failed to issue ticket")
		return
	}

	target, err := url.Parse(product.SSOCallbackURL())
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "CONFIG", "invalid product callback url")
		return
	}
	q := target.Query()
	q.Set("ticket", ticket.ID)
	if to != "" {
		q.Set("to", to)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// ticketVerifyRequest is the product backend's exchange payload.
type ticketVerifyRequest struct {
	Ticket string `json:"ticket"`
}

// ticketVerifyResponse returns the verified identity to the product backend.
type ticketVerifyResponse struct {
	Identity Identity `json:"identity"`
	To       string   `json:"to"`
}

// verifyTicket authenticates the calling product with its shared secret and
// exchanges a single-use ticket for the identity snapshot.
func (s *Server) verifyTicket(w http.ResponseWriter, r *http.Request) {
	product, ok := s.authenticateProduct(r)
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "invalid client credentials")
		return
	}
	if product.Mode != config.ProductModeNS {
		httpx.WriteError(w, r, http.StatusBadRequest, "TICKET_UNSUPPORTED", "product uses direct OIDC mode")
		return
	}

	var req ticketVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Ticket) == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "ticket is required")
		return
	}

	ticket, err := s.tickets.Consume(r.Context(), req.Ticket, product.ID)
	if err != nil {
		if errors.Is(err, ErrReplayUnavailable) {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "TICKET_STORE_UNAVAILABLE",
				"the shared ticket store is unavailable; try again later")
			return
		}
		code := "TICKET_INVALID"
		switch {
		case errors.Is(err, ErrTicketExpired):
			code = "TICKET_EXPIRED"
		case errors.Is(err, ErrTicketAudience):
			code = "TICKET_AUDIENCE"
		case errors.Is(err, ErrTicketNotFound):
			code = "TICKET_NOT_FOUND"
		}
		httpx.WriteError(w, r, http.StatusBadRequest, code, err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ticketVerifyResponse{Identity: ticket.Identity, To: ticket.To})
}

// authenticateProduct validates the server-to-server product credentials.
func (s *Server) authenticateProduct(r *http.Request) (config.ProductSection, bool) {
	clientID := r.Header.Get(headerClientID)
	clientSecret := r.Header.Get(headerClientSecret)
	product, ok := s.cfg.ProductByID(clientID)
	if !ok || product.SSOSecret == "" {
		return config.ProductSection{}, false
	}
	if subtle.ConstantTimeCompare([]byte(clientSecret), []byte(product.SSOSecret)) != 1 {
		return config.ProductSection{}, false
	}
	return product, true
}

// login authenticates posted credentials against the emergency local account
// store. It is disabled (404) in oidc mode, where the browser uses the
// identity service's login page instead.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.mode != config.AuthModeLocal || s.local == nil {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "local login is disabled")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "username and password are required")
		return
	}

	redirect, err := s.safeRedirect(req.Redirect)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_REDIRECT", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := s.local.Authenticate(ctx, req.Username, req.Password)
	if err != nil {
		// Do not reveal whether the user or the password was wrong.
		slog.InfoContext(r.Context(), "local login failed", "user", req.Username, "trace_id", httpx.TraceID(r.Context()))
		httpx.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}

	if err := s.establishSession(w, r, result); err != nil {
		slog.ErrorContext(r.Context(), "save session failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "SESSION", "failed to establish session")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, loginResponse{Identity: result.Identity, Redirect: redirect})
}

// establishSession seals the authenticated identity into the session cookie.
func (s *Server) establishSession(w http.ResponseWriter, r *http.Request, result *AuthResult) error {
	now := time.Now()
	session := &SessionData{
		Identity:     result.Identity,
		AccessToken:  result.AccessToken,
		IDToken:      result.IDToken,
		RefreshToken: result.RefreshToken,
		AccessExpiry: result.AccessExpiry,
		LoginAt:      now,
		LastSeen:     now,
		Expiry:       now.Add(s.cfg.Session.AbsoluteDuration),
	}
	return s.codec.SaveSession(w, s.cookieOpts(), session)
}

// endSession revokes the upstream refresh token best effort and clears the
// NS cookies. It returns the id token hint when an OIDC session existed.
func (s *Server) endSession(w http.ResponseWriter, r *http.Request) string {
	idTokenHint := ""
	if session, err := s.codec.LoadSession(r, s.cfg.Session.CookieName); err == nil {
		idTokenHint = session.IDToken
		if s.oidc != nil && session.RefreshToken != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			if revErr := s.oidc.Revoke(ctx, session.RefreshToken); revErr != nil {
				slog.WarnContext(r.Context(), "token revocation failed", "error", revErr)
			}
			cancel()
		}
	}
	s.codec.ClearSession(w, "")
	clear(w, CSRFCookie, "")
	return idTokenHint
}

// logout is the XHR logout: revoke upstream tokens, clear cookies, stay on
// the NS origin.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.endSession(w, r)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// logoutBrowser is the link-followable logout: after clearing the NS session
// it continues to RP-initiated logout at the identity service, which returns
// the browser to the NS portal when configured.
func (s *Server) logoutBrowser(w http.ResponseWriter, r *http.Request) {
	idTokenHint := s.endSession(w, r)
	target := "/"
	if s.oidc != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		// The identity plane registers the client's post-logout redirect URI
		// as the product origin without a trailing slash, and the engine
		// matches it exactly, so send the trimmed base URL verbatim (a stray
		// "/" causes an "Invalid redirect uri" error page at logout).
		endURL, err := s.oidc.EndSessionURL(ctx, idTokenHint, strings.TrimSuffix(s.cfg.Server.BaseURL, "/"))
		cancel()
		if err != nil {
			slog.WarnContext(r.Context(), "end session url unavailable", "error", err)
		} else {
			target = endURL
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// Me returns the authenticated identity, transparently refreshing the
// session, or 401 when no valid session exists.
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	session, err := s.resolver.Resolve(w, r)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, session.Identity)
}

// verifySession is the forward-auth decision endpoint used by an ingress
// controller (nginx auth_request, Traefik forwardAuth). A valid session
// yields 204 and the identity headers the ingress should propagate to the
// upstream product; anything else yields 401 and the ingress blocks the
// request before it reaches the product.
func (s *Server) verifySession(w http.ResponseWriter, r *http.Request) {
	session, err := s.resolver.Resolve(w, r)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	h := w.Header()
	h.Set("X-User-Id", session.Identity.Sub)
	h.Set("X-User-Email", session.Identity.Email)
	h.Set("X-User-Name", session.Identity.Name)
	h.Set("X-User-Roles", session.RolesHeader())
	w.WriteHeader(http.StatusNoContent)
}

// readyz reports NS liveness and identity-service readiness. Local mode is
// always ready; oidc mode requires the issuer discovery endpoint to answer.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.tickets.ReplayStore().Ready(ctx); err != nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "ticket replay store unavailable")
		return
	}
	if s.oidc == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "auth": config.AuthModeLocal, "replay": s.cfg.Tickets.ReplayStore})
		return
	}
	if err := s.oidc.Ready(ctx); err != nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "identity service unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "auth": config.AuthModeOIDC, "replay": s.cfg.Tickets.ReplayStore})
}

// cookieOpts builds the session cookie security settings from config.
func (s *Server) cookieOpts() CookieOptions {
	return CookieOptions{
		Name:   s.cfg.Session.CookieName,
		Secure: s.cfg.Session.Secure,
		TTL:    s.cfg.Session.AbsoluteDuration,
	}
}

// safeRedirect validates a post-login destination. Relative NS paths and
// exact matches against configured product origins are allowed; everything
// else is rejected to prevent open redirects.
func (s *Server) safeRedirect(raw string) (string, error) {
	if raw == "" {
		return "/", nil
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errInvalidRedirect
	}
	if !s.allowedRedirects[u.Scheme+"://"+u.Host] {
		return "", errInvalidRedirect
	}
	return raw, nil
}

// safeProductPath reports whether to is a benign same-origin path for the
// product: it must start with a single slash and carry no scheme/host.
func safeProductPath(to string) bool {
	if to == "" {
		return true
	}
	if !strings.HasPrefix(to, "/") || strings.HasPrefix(to, "//") {
		return false
	}
	if strings.Contains(to, "\\") {
		return false
	}
	return true
}
