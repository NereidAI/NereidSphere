// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package portal serves the suite portal: the thin product-catalogue API
// the SPA renders and the embedded static application itself. It contains no
// domain logic; the catalogue is derived directly from configuration.
package portal

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/NereidAI/NereidSphere/internal/auth"
	"github.com/NereidAI/NereidSphere/internal/config"
	"github.com/NereidAI/NereidSphere/internal/httpx"
	webassets "github.com/NereidAI/NereidSphere/web"
)

// productView is the portal-facing projection of a configured product. The
// launch URL goes through the NS cross-product entry, which authenticates the
// user and hands the product a one-time ticket, so no re-login is needed.
type productView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	// Mode is "ns" (one-time handoff ticket) or "oidc" (standalone RP).
	Mode string `json:"mode"`
	// LaunchURL always goes through the NS cross-product entry; NS decides
	// internally whether to mint a ticket or redirect to the product.
	LaunchURL string `json:"launch_url"`
	// Assigned reports whether the current session holds at least one
	// /nereid{product}/{tenant}/{role} group; unassigned products are shown
	// greyed out and the /auth/enter boundary enforces the same default-deny.
	Assigned bool `json:"assigned"`
}

// Handler serves portal API endpoints and the embedded SPA.
type Handler struct {
	cfg      *config.Config
	resolver *auth.Resolver
}

// NewHandler builds the portal handler.
func NewHandler(cfg *config.Config, resolver *auth.Resolver) *Handler {
	return &Handler{cfg: cfg, resolver: resolver}
}

// Routes registers the public portal API. SPA serving is mounted separately
// by the composition root as the catch-all route.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/products", h.listProducts)
	mux.HandleFunc("GET /healthz", h.health)
}

// listProducts returns the configured product catalogue as bare JSON, with
// per-product assignment state derived from the session's groups. The
// catalogue itself is public; assigned is simply false for anonymous callers.
func (h *Handler) listProducts(w http.ResponseWriter, r *http.Request) {
	var groups []string
	var roles []string
	if h.resolver != nil {
		if session, err := h.resolver.Resolve(w, r); err == nil {
			groups = session.Identity.Groups
			roles = session.Identity.Roles
		}
	}
	views := make([]productView, 0, len(h.cfg.Products))
	for _, p := range h.cfg.Products {
		views = append(views, productView{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			URL:         p.URL,
			Mode:        p.Mode,
			// Cross-product SSO: NS authenticates and either mints a
			// one-time ticket (mode ns) or redirects straight to the
			// product's own OIDC login (mode oidc).
			LaunchURL: strings.TrimSuffix(h.cfg.Server.BaseURL, "/") + "/auth/enter/" + p.ID,
			Assigned:  auth.CanEnterProduct(groups, roles, p.ID),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, views)
}

// health is a liveness probe that always succeeds once the server runs.
func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SPAHandler serves the embedded single-page application with history-mode
// fallback, mirroring NereidLink: existing files are served directly,
// extension-less unknown paths fall back to index.html, and missing static
// assets return a real 404.
func SPAHandler() http.Handler {
	assets, err := webassets.DistFS()
	if err != nil {
		// Only reachable if the embedded layout changes, which is a build
		// defect rather than a client error.
		panic("embedded web assets unavailable: " + err.Error())
	}
	fileServer := http.FileServerFS(assets)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Normalize to prevent traversal via segments such as "/../".
		clean := path.Clean(r.URL.Path)
		if !strings.HasPrefix(clean, "/") {
			clean = "/" + clean
		}

		relative := strings.TrimPrefix(clean, "/")
		if relative == "" {
			relative = "index.html"
		}

		if _, err := fs.Stat(assets, relative); err != nil {
			if path.Ext(relative) != "" {
				// Missing fingerprinted asset: never masquerade as HTML.
				http.NotFound(w, r)
				return
			}
			// Client-side route: serve index.html for the React router.
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}
