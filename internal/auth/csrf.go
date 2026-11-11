// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// CSRF defence for browser-driven state changes. NS issues a readable
// double-submit cookie (ns_csrf) on first GET and the SPA echoes its value
// in the X-Csrf-Token header on every mutating API call. The server accepts
// a request only when header and cookie match, which a cross-site attacker
// cannot read or forge. Server-to-server calls (the per-product ticket
// exchange) authenticate with X-NS-Client-Id/Secret instead and are exempt.

package auth

import "net/http"

const (
	// CSRFCookie is the JavaScript-readable double-submit cookie.
	CSRFCookie = "ns_csrf"
	// CSRFHeader is the request header the SPA sets to the cookie value.
	CSRFHeader = "X-Csrf-Token"
)

var csrfProtectedMethods = map[string]bool{
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}
