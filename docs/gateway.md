<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Unified API Gateway

## Responsibilities

NereidSphere gateway is a thin, stdlib reverse proxy providing cross-cutting
concerns only:

- Session authentication against the sealed `ns_session` cookie (with
  transparent token refresh / idle and absolute lifetime enforcement)
- Routing to product services under `/p/{product}/...`
- CORS with an exact origin allowlist
- Per-client rate limiting plus a stricter limit on the local login endpoint
- CSRF double-submit enforcement on browser-driven state-changing API calls
- Structured request logging and correlation ID propagation
- Browser security headers on NS-owned responses
- Upstream timeouts with a standard error envelope

There is no business logic: authorization decisions remain with products.

## Routing

A request to `/p/link/api/devices` is forwarded to
`{products[link].url}/api/devices`. The `/p/{product}/` prefix is stripped,
the `Host` header is set to the upstream, and the gateway authenticates the
session before proxying. `OPTIONS` preflight requests bypass authentication.
Each upstream call is bounded by `gateway.upstream_timeout` (default 30s); an
unreachable upstream returns `502 UPSTREAM_UNAVAILABLE`, a slow one
`504 UPSTREAM_TIMEOUT`.

### WebSocket

The stdlib reverse proxy passes `Upgrade`/`Connection` headers and the
`Sec-WebSocket-*` negotiation through unchanged, so WebSocket connections to
`/p/{product}/...` work once the `ns_session` cookie authenticates the
upgrade request. v1 limitation: the 30-second upstream timeout and per-client
rate limiter apply to the handshake; idle long-lived frames are not
additionally policed by NS. Cross-product WebSocket SSO is covered by the
product-local pair the product mints after ticket verification.

## Identity Headers

Inbound copies are always removed first, so a client cannot spoof identity:

| Header | Source |
| --- | --- |
| `X-User-Id` | session identity `sub` |
| `X-User-Email` | identity `email` |
| `X-User-Name` | display `name` |
| `X-User-Roles` | comma-joined application roles |
| `X-NS-Integration-Secret` | `products[].sso_secret`, set **only** on the configured `tenants_path` |
| `X-Request-Id` | correlation/trace id (honored inbound, else generated) |

Products must treat these headers as trustworthy **only** when the request
arrives from the gateway, and must never expose their direct origin to end
users.

`X-NS-Integration-Secret` proves the call originated at NS on a product's
directly reachable origin. The gateway stamps it on exactly the
`products[].tenants_path` request (the admin console's tenant picker calls
`/p/{id}{tenants_path}`) using the same shared secret the product already
holds for the SSO ticket exchange; products verify it with a constant-time
compare. The built-in products use `/v1/integration/ns/tenants` as
`tenants_path`; their domain APIs such as `/api/v1/tenants` and
`/api/organizations` require their own product sessions and must not be used
for this integration. The secret is never set on any other proxied path, and
an inbound copy is always stripped.

## CORS

`gateway.cors_allowed_origins` lists exact origins permitted to make
credentialed cross-origin calls. Empty defaults to the NS origin plus every
configured product origin. Responses for unknown origins carry no CORS
headers, and their preflight returns 403.

## Rate Limiting

- General traffic: token bucket per client IP
  (`gateway.requests_per_minute`, `gateway.burst`; default 300/60).
- Login: in `local` mode the emergency login endpoint gets a separate,
  stricter bucket (`gateway.login_requests_per_minute`,
  `gateway.login_burst`; default 10/5) to slow credential stuffing. In
  `oidc` mode login happens at NereidIdentity and this limit is unused.
  Over-budget responses use `429 RATE_LIMITED` with `Retry-After`.

## CSRF

Browser-driven mutating requests under `/api/` must carry the
`X-Csrf-Token` header matching the JavaScript-readable `ns_csrf` cookie
(double-submit). Mismatches are rejected with `403 CSRF_FAILED`; CORS
preflight `OPTIONS` and the server-to-server ticket exchange
(authenticated by `X-NS-Client-Id/Secret`) are exempt.

## API documentation

`GET /api/docs` serves a self-contained, offline Swagger UI (embedded
vendored assets — no CDN), and `GET /api/docs/openapi.yaml` serves the
embedded OpenAPI 3 contract describing every NS endpoint.

## Security Headers

On every non-`/p/` response NS sends `X-Content-Type-Options: nosniff`,
`X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`. HSTS is emitted
automatically when the environment is `prod`.

## Health

- `GET /healthz` — liveness, 200 once the process serves.
- `GET /readyz` — readiness. In `local` mode always 200; in `oidc` mode it
  probes the identity issuer discovery endpoint (successful result cached
  for 10s) and returns 503 when the issuer is unreachable. Startup itself
  never fails when the issuer is down (discovery is lazy).

## Forward auth (ingress integration)

Products deployed behind a shared ingress can delegate authentication to NS
without implementing the ticket exchange. Point the ingress external-auth
mechanism at:

```
GET https://<ns-origin>/api/v1/auth/verify
```
- Valid session → `204 No Content` with identity headers `X-User-Id`,
  `X-User-Email`, `X-User-Name`, `X-User-Roles` and `X-Request-Id`, which the
  ingress must forward to the upstream product.
- No/invalid session → `401`, and the ingress blocks the request.

ingress-nginx sketch:

```yaml
nginx.ingress.kubernetes.io/auth-url: "https://<ns-origin>/api/v1/auth/verify"
nginx.ingress.kubernetes.io/auth-response-headers:
  "X-User-Id, X-User-Email, X-User-Name, X-User-Roles, X-Request-Id"
```

The NS origin must be reachable by the ingress controller, and products must
treat the identity headers as trusted only on that path.

---

## NS Health & Emergency Login

The gateway relies on NS being healthy for SSO. When NS is unavailable,
products in `mode: ns` cannot complete the ticket exchange. Each product
implements its own NS health check (`GET /readyz` via a local proxy) and
falls back to local emergency accounts.

See [Emergency Local Login Design](emergency-local-login.md) for the
product-side integration guide.
