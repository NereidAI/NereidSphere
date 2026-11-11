// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Browser-facing security response headers for NS-owned responses. Headers
// are not applied to gateway-proxied product content, which is owned by the
// products themselves.

package httpx

import "net/http"

// SecurityHeadersMiddleware adds conservative, static browser hardening
// headers. HSTS is emitted only when hsts is true (HTTPS deployments), since
// sending it over plain HTTP is counterproductive.
func SecurityHeadersMiddleware(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("X-XSS-Protection", "0")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ApplySecurityHeaders wraps next so security headers cover NS-owned routes
// while /p/ gateway traffic reaches products untouched.
func ApplySecurityHeaders(hsts bool, next http.Handler) http.Handler {
	headers := SecurityHeadersMiddleware(hsts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isProxyPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		headers(next).ServeHTTP(w, r)
	})
}

// isProxyPath reports whether path targets the product gateway prefix.
func isProxyPath(path string) bool {
	return len(path) >= 3 && path[:3] == "/p/"
}
