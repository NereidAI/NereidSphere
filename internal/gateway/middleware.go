// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package gateway implements NereidSphere's thin unified API gateway. It
// authenticates the encrypted portal session (transparently refreshing it)
// and reverse-proxies product traffic, injecting verified identity headers.
// It holds no business logic: authorization decisions and all domain
// behaviour remain with the upstream products. Routes are namespaced as
// /p/{product}/... so one listener can front every suite product.

package gateway

import (
	"net/http"

	"github.com/NereidAI/NereidSphere/internal/auth"
	"github.com/NereidAI/NereidSphere/internal/config"
	"github.com/NereidAI/NereidSphere/internal/httpx"
	"log/slog"
)

// sessionResolver loads/refreshes the session for a request.
type sessionResolver interface {
	Resolve(w http.ResponseWriter, r *http.Request) (*auth.SessionData, error)
}

// Middleware authenticates proxied requests against the portal session.
type Middleware struct {
	resolver sessionResolver
}

// NewMiddleware builds the gateway authentication middleware.
func NewMiddleware(resolver sessionResolver) *Middleware {
	return &Middleware{resolver: resolver}
}

// RequireSession returns 401 with the standard error envelope when no valid
// session is present, otherwise stores the session on the request context.
func (m *Middleware) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := m.resolver.Resolve(w, r)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), session)))
	})
}

// Routes registers the product reverse-proxy routes. A request to
// /p/link/api/x is forwarded to <link-url>/api/x with identity headers.
// OPTIONS is left unauthenticated so the browser CORS preflight succeeds.
func Routes(mux *http.ServeMux, mw *Middleware, cfg *config.Config) {
	for _, p := range cfg.Products {
		product := p // capture per iteration for the closures
		proxy := NewReverseProxy(product, cfg.Gateway.UpstreamTimeoutDuration)

		pattern := "/p/" + product.ID + "/"
		mux.Handle(pattern, mw.RequireSession(proxy))
		mux.Handle("OPTIONS "+pattern, proxy)

		slog.Info("gateway route registered", "product", product.ID, "upstream", product.URL)
	}
}
