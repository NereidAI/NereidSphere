// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Restrictive CORS middleware. Only exact origins from the configured
// allowlist may make credentialed cross-origin calls; unknown origins receive
// no CORS headers at all (the browser then blocks the response). Preflight
// requests are answered directly. Same-origin browser requests carry no
// Origin header and pass through untouched.

package httpx

import (
	"net/http"
)

// defaultCORSMethods/Headers are the narrow set the NS portal needs.
var (
	corsMethods = "GET, POST, OPTIONS"
	corsHeaders = "Content-Type, Authorization, X-Request-ID"
	corsMaxAge  = "300"
)

// CORSMiddleware enforces an exact-match origin allowlist with credentials.
func CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			permitted := false
			if origin != "" {
				_, permitted = allowed[origin]
			}
			if permitted {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Vary", "Origin")
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
			}

			if r.Method == http.MethodOptions && origin != "" {
				if !permitted {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", corsMethods)
				h.Set("Access-Control-Allow-Headers", corsHeaders)
				h.Set("Access-Control-Max-Age", corsMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
