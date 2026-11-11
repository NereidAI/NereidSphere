// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Reverse proxy for one integrated product. The proxy strips the
// /p/{product}/ prefix, forwards the remaining path to the product origin,
// and injects the verified identity as headers. WebSocket upgrades are
// supported transparently by httputil.ReverseProxy.

package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
	"github.com/NereidAI/NereidSphere/internal/httpx"
)

// identityHeaders are the headers NereidSphere sets from the verified
// session. Any inbound copies from the client are removed first so a caller
// cannot spoof identity.
var identityHeaders = []string{
	"X-User-Id",
	"X-User-Email",
	"X-User-Name",
	"X-User-Roles",
	"X-NS-Integration-Secret",
	"X-Request-Id",
}

// NewReverseProxy builds a handler that forwards /p/{id}/<rest> to the
// product origin at /<rest>, bounding each upstream call with timeout.
func NewReverseProxy(product config.ProductSection, timeout time.Duration) http.Handler {
	target, err := url.Parse(product.URL)
	if err != nil {
		// Configuration is validated at startup; a bad URL is a programming
		// error, not a runtime condition.
		panic("invalid product url for " + product.ID + ": " + err.Error())
	}

	prefix := "/p/" + product.ID
	proxy := &httputil.ReverseProxy{}
	proxy.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
	}

	// Rewrite replaces the deprecated Director hook. SetURL applies the target
	// origin and base path; the product prefix is then removed as before.
	proxy.Rewrite = func(proxyRequest *httputil.ProxyRequest) {
		proxyRequest.SetURL(target)
		req := proxyRequest.Out
		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}

		// Match NewSingleHostReverseProxy by suppressing the default Go
		// User-Agent when the caller did not provide one.
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", "")
		}
		// Rewrite strips inbound forwarding headers. Restore the client chain,
		// then regenerate forwarding metadata from the actual inbound request.
		req.Header["X-Forwarded-For"] = proxyRequest.In.Header["X-Forwarded-For"]
		proxyRequest.SetXForwarded()

		for _, h := range identityHeaders {
			req.Header.Del(h)
		}
		if session, ok := SessionFromContext(req.Context()); ok {
			req.Header.Set("X-User-Id", session.Identity.Sub)
			req.Header.Set("X-User-Email", session.Identity.Email)
			req.Header.Set("X-User-Name", session.Identity.Name)
			req.Header.Set("X-User-Roles", session.RolesHeader())
		}
		// Prove origin on the admin console's tenant-list call: the gateway
		// stamps the product's own shared secret (already used for the SSO
		// ticket exchange) so the product can reject a forged X-User-* identity
		// arriving at its directly reachable origin. Scoped to the configured
		// tenants_path so the secret is not sent on every proxied request.
		if product.TenantsPath != "" && product.SSOSecret != "" && req.URL.Path == product.TenantsPath {
			req.Header.Set("X-NS-Integration-Secret", product.SSOSecret)
		}
		if trace := httpx.TraceID(req.Context()); trace != "" {
			req.Header.Set("X-Request-Id", trace)
		}
	}

	// Surface upstream failures as the standard error envelope: a deadline
	// is a 504, every other connection failure a 502.
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(r.Context().Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			httpx.WriteError(w, r, http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT",
				"product "+product.ID+" did not respond in time")
			return
		}
		httpx.WriteError(w, r, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE",
			"product "+product.ID+" is not reachable")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}
