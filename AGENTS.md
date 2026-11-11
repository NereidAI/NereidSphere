<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# NereidSphere AGENTS.md

These instructions are self-contained. Read `README.md`, `FEATURES.md`,
`CONTRIBUTING.md`, and the relevant documents under `docs/` before changing
the corresponding area. No external repository is required to interpret the
rules in this file.

## 1. Project Scope (NereidSphere)

NereidSphere is the **thin suite layer** — one portal, single sign-on (SSO), and a unified API
gateway. Cross-product event glue is a planned boundary capability, not a delivered feature. NS
holds **no domain logic** of its own.

- Without NS, other products can still authenticate and operate independently.
- When NS is enabled, it provides **suite-wide SSO**: after logging into NS, users can jump to
  any integrated product **without re-authentication** (SSO across products).
- NS is a **pure OIDC/OAuth2 relying party** for the suite. The identity service is a
  suite-level public component deployed and maintained independently in its own
  NereidIdentity repository (Keycloak is one supported engine); NS contains no IdP
  bootstrap and no realm/master admin credentials.
- Authentication is selected by **`auth.mode`**: `oidc` (default — authorization-code
  + PKCE browser flow against NereidIdentity) or `local` (emergency bcrypt accounts
  declared in config/Secret). There is no `oidc.enabled` flag and no ROPC.
- Deployment modes:
  - **Development**: no external dependency — local accounts work out of the box; an external
    identity service can run separately (its own compose, issuer on port 8090).
  - **Production/K8s/K3s**: the identity service is installed independently via **Helm**; NS is
    configured with its OIDC connection block, or runs on local accounts.

NS is intentionally thin: it federates identity, fronts traffic via the unified gateway, and
provides the suite portal entry point. It must never duplicate domain features owned by other
products (NereidLink, NereidForge, NereidHorizon, NereidEdge, NereidFleet, NereidMobile).

## 2. Core Responsibilities

1. **Portal (Suite Entry)** — unified landing/portal for all products; deep links to each product
   with SSO continuation (no re-auth on transition).
2. **SSO / Identity Federation** — the suite SSO entry. In oidc mode the browser is
   redirected to the NereidIdentity login page (authorization-code + PKCE) and back to
   `/auth/callback`; NS verifies the id token and holds the sealed NS session. Products
   integrate either via the one-time handoff ticket (`mode: ns`) or as standalone NI
   OIDC clients (`mode: oidc`).
3. **NereidIdentity via OIDC + Admin REST** — NS talks to the identity engine only
   through standard contracts: discovery/authorization-code/PKCE for login (issuer,
   client_id, client_secret) and OAuth2 client_credentials with the
   `nereidsphere-admin` service account for the built-in account console (account
   search/pagination, CRUD/enable/reset/temporary password/roles, self/last-admin
   guards and audit logging; federated users read-only). It never stores an engine admin
   password and contains no provider-specific concepts; swapping engines changes the
   `oidc:` block only. `auth.mode=local` is the emergency fallback.
4. **Unified API Gateway** — single public entry for cross-cutting concerns (authN/Z, routing,
   CORS, rate limiting, request/response logging, tracing headers, TLS termination as applicable).
   Gateway forwards to product services; business logic stays with products.
5. **Planned Closed-loop Event Glue** — future cross-product events/bus integration hooks
   (pub/sub) may propagate identity/context changes or suite-level notifications. There is no
   current `internal/events` implementation or delivered event capability; do not describe this
   boundary plan as shipped functionality or add product domain event processing logic.

## 3. Technology Choices & Conventions

- **Backend**: Go 1.27.3 with standard-library transport — `net/http` ServeMux method/pattern
  routing and `log/slog` structured logging — plus `spf13/cobra` for the `serve` CLI. There is no
  third-party router, logger, database layer, or migration command. Responses are bare JSON on
  success and `{"error":{"code","message","trace_id"}}` on failure.
- **Auth/OIDC**: NS owns the visible entry/error experience, but in oidc mode the browser is
  redirected to the **NereidIdentity-branded login page**; NS never collects the upstream
  password. Login is OAuth2 **authorization-code + PKCE (S256)** (`coreos/go-oidc/v3` +
  `golang.org/x/oauth2`) with lazy discovery (startup must not fail when NI is down;
  `/readyz` reports issuer reachability), sealed `state`/`nonce`/verifier in the
  short-lived HttpOnly `ns_auth_flow` cookie, JWKS id-token verification
  (iss/aud/exp/nonce), roles from the access-token `realm_access.roles`. In local mode
  credentials are checked against bcrypt accounts (`auth.local_users`) on an explicitly
  emergency-only form. The admin console uses the `nereidsphere-admin` client_credentials
  service account (see `internal/admin`). Session is a server-encrypted HttpOnly
  `ns_session` cookie; no bearer tokens in browser JS; browser mutations carry the
  `ns_csrf`/`X-Csrf-Token` double-submit (S2S ticket exchange exempt).
- **Portal (Frontend)**: TypeScript + Vite + React 18 + **antd 5** + react-router-dom 6, aligned with
  NereidLink/web. Minimal state, deep-link aware, SSO redirect handling.
- **Gateway**: Reverse proxy + auth middleware (NS session verification; OIDC token refresh when
  enabled). Preserve trace headers; pass identity (sub/tenant/org/roles) to downstream services
  via headers (never leak secrets).
- **Config**: YAML config file selected by `-c` (`configs/dev.yaml`), snake_case keys, and
  per-section `Validate()`. The only environment override is `NS_VALKEY_PASSWORD`, which supplies
  the built-in Valkey Secret without putting it in `config.yaml`. No database schema or migration
  component exists. Web assets are embedded via `embed.FS` and served with SPA fallback.
- **Ticket replay**: cross-replica JTI single-use state uses Valkey via a small standard-library
  RESP/Sentinel client (`internal/valkey`), no third-party Redis SDK. Sentinel mode uses
  `SET NX PX` plus `WAIT 1 300`; uncertain replies fail closed as `TICKET_STORE_UNAVAILABLE`.
- **Security**: mTLS for internal service-to-service where applicable; short-lived tokens, secure
  cookies (HttpOnly/SameSite/secure), PKCE, exact redirect allowlists, no static long-lived secrets
  in repo.

## 4. Directory Layout

```text
NereidSphere/
├── AGENTS.md                # self-contained project instructions
├── README.md                # overview, development, deployment, and SSO flow
├── CONTRIBUTING.md          # contributor workflow and verification rules
├── FEATURES.md              # protected delivered-features inventory
├── LICENSE / NOTICE / THIRD_PARTY_NOTICES
├── go.mod                   # Go module and dependency declarations
├── cmd/
│   └── sphere/              # cobra entrypoint: serve
├── internal/
│   ├── config/              # YAML config loading/validation
│   ├── auth/                # OIDC/local auth, sessions, tickets, and CSRF
│   ├── admin/               # Admin REST client and account console API
│   ├── apidocs/             # embedded OpenAPI/Swagger UI handler
│   ├── gateway/             # routing, auth middleware, and reverse proxy
│   ├── httpx/               # CORS, security headers, and rate limiting
│   ├── portal/              # portal API and embedded SPA serving
│   └── valkey/              # RESP/Sentinel client for ticket JTI claims
├── web/
│   ├── assets.go            # go:embed of web/dist
│   ├── src/                 # React 18/antd 5/TypeScript portal source
│   ├── public/
│   ├── scripts/             # i18n and bilingual-help consistency checks
│   ├── package.json
│   └── vite.config.ts
├── configs/dev.yaml         # development configuration selected by -c
├── deploy/helm/
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
└── docs/
    └── api/                 # OpenAPI specification and vendored Swagger UI
```

## 5. SSO Behavior (Core Requirement)

- After login to NS, user can **directly jump to each integrated product
  without re-authentication**. In oidc mode the browser is redirected to the
  NereidIdentity login page and back (`/auth/login` → `/auth/callback`,
  authorization-code + PKCE), and NS sets the sealed session cookie; NS never
  sees the upstream password. In local mode the emergency form posts to the NS
  backend, which checks bcrypt accounts. For mode-`ns` products NS mints a
  **30-second single-use sealed ticket** at `GET /auth/enter/{product}`,
  redirects the browser to the product callback, and the product backend
  exchanges the ticket server to server at `POST /api/v1/auth/ticket/verify`
  (per-product shared secret) for the verified identity, then builds its own
  session. The JTI is claimed once in process memory only for single-replica
  dev and in shared Valkey for production. Mode-`oidc` products are standalone
  NI clients and NS redirects straight to them. Avoid token passthrough that
  leaks scopes.
- NS acts as the **suite SSO entry point**. Products may redirect unauthenticated
  users to NS `/auth/login?redirect=<product-url>`; NS completes the PKCE round
  trip (or local emergency login) and returns the user to the allowlist-checked
  product URL with a validated NS session. The exact handoff is documented in
  `docs/sso-flow.md`.
- **Standard contracts only**: login uses OIDC authorization-code/PKCE; the
  account console uses NI Admin REST via the `nereidsphere-admin`
  client_credentials service account. The identity engine is independently
  deployed and swappable (Keycloak is one implementation); see
  `docs/oidc-integration.md`.

## 6. External Identity Service via OIDC

- The identity service is **not part of NereidSphere**: no compose stack, chart,
  bootstrap job or admin credentials live in this repository.
- **Dev**: start the identity plane independently (its own dev compose publishes
  the issuer on host port 8090), set `auth.mode: oidc` with
  `issuer=http://localhost:8090/realms/nereidai`, and grant the admin service
  account its role through that plane's documented bootstrap process; or use
  the built-in local accounts with `auth.mode: local`. Register the exact
  `http://localhost:8081/auth/callback` URI for the embedded portal. The Vite
  server listens on port 5174, so `http://localhost:5174/auth/callback` must
  also be registered explicitly when that origin is used for OIDC.
  - **K8s/K3s (Helm)**: the identity service is installed independently via its own
  Helm release and namespace. NS uses its own product-owned **`nereidsphere`
  namespace**; the OIDC realm remains `nereidai`. The NS chart consumes
  `config.auth.mode`, `config.oidc.issuer/clientId/clientSecret/
  adminClientId/adminClientSecret` (or `config.auth.localUsers`) and may deploy
  the product-local ephemeral Valkey ticket store. Do not embed hardcoded
  internal endpoints.
- NS never provisions clients/realms: client registrations, redirect allowlists,
  role mappings and brokers are owned declaratively by the identity-plane
  repository.

## 7. Build, Lint, Type-check, Test

**Go (backend):**

```bash
go fmt ./...
go vet ./...
go build ./...
go test -v ./...
# if golangci-lint exists in repo or common: golangci-lint run ./...
```

**Frontend (web/):**

```bash
cd web
npm ci
npm run check       # i18n key/call-site and bilingual-help assertions
npm run typecheck   # tsc --noEmit
npm run build       # typecheck plus the Vite production build
```

The current `lint` and `test` npm scripts are placeholders, not substitutes for
the checks above.

NS is thin; prefer **unit tests** around auth/gateway logic, PKCE flow helpers
(use a mock issuer; never hit a real identity service in unit tests), stateless
ticket issue/replay across independently constructed verifiers, Valkey
`SET NX PX`/Sentinel failure handling and fail-closed 503s, admin
authorization/federation gating, local account verification, and redirect
validation. Integration tests only when an external identity service is
required and easily isolated (dev-only). No end-to-end across all products in
NS unit scope. The embedded OpenAPI spec under `docs/api/openapi.yaml` must
stay valid YAML with an OpenAPI 3 version (`docs/api` test enforces it).

**Verification rule**: Run `make verify` before committing a complete change; it checks gofmt,
vet, optional golangci-lint, Go tests, locked frontend installation, i18n/help assertions,
TypeScript, production builds, and Helm lint/rendering. Use narrower commands during iteration.
Always run `tsc --noEmit` through `npm run typecheck` or the build; Vite alone is not a type check.

## 8. Security & Boundaries

- **Thin layer only**: Never implement product domain logic. If tempted, push down to owning product.
- **Redirect allowlist**: Strict allowlist for post-login/product redirects (prevent open redirect).
- **Token hygiene**: Short-lived access, rotation via refresh; secure storage (HttpOnly cookies).
  Never log tokens/secrets/keys. No secrets in code/config committed.
- **Cross-repo boundary**: Only edit within `NereidSphere/`. Treat sibling repositories as
  read-only. Shared scratch is `.tmp/NereidSphere/` only.
- **No infrastructure mutation**: Manage the external identity service and other deployed
  infrastructure through its own repository, configuration, and Helm release; never perform ad-hoc
  destructive operations.
- **CORS**: Restrictive, per product origin allowlist.
- **CSRF**: All browser-driven state-changing `/api/` requests require the
  `ns_csrf`/`X-Csrf-Token` double-submit; only the S2S ticket exchange (product
  credentials) and CORS preflight are exempt.

## 9. Docs & Design Artifacts

- `docs/sso-flow.md` — PKCE login/callback, stateless tickets, mode ns/oidc, CSRF,
  token/session, logout, error cases.
- `docs/oidc-integration.md` — OIDC contract (discovery, authorization-code/PKCE,
  client, scopes, roles claim), the admin client_credentials service account,
  and local-account emergency fallback.
- `docs/gateway.md` — routing rules, auth middleware, CSRF, forward auth, rate
  limits, identity headers, embedded `/api/docs`.
- `docs/pitfalls.md` — recorded pitfalls/lessons; append the relevant lesson in
  the same change that resolves a non-obvious problem.
- `FEATURES.md` — protected delivered-features inventory; read before touching
  an area and update when shipping a feature.
- `docs/api/openapi.yaml` — the HTTP contract; served with the embedded Swagger
  UI at `/api/docs`; keep every endpoint documented here.
- `README.md` — quickstart (local accounts by default, NereidIdentity via
  `auth.mode=oidc`), config, Helm install notes.

## 10. Dev Workflow Notes

- **Dev first**: built-in local accounts need no external service. Run `make web-install` once
  after checkout. `make dev` starts the backend only; run `make web-dev` in a second terminal for
  the Vite portal, or run `make build-local` followed by
  `./bin/nereidsphere serve -c configs/dev.yaml` for the embedded portal. Use an independently
  deployed identity service only when testing OIDC.
- **Config**: YAML only; `auth.mode` selects `local`/`oidc`, the `oidc:` block holds the NI
  connection + admin service account, `auth.local_users` declares bcrypt accounts. Document
  defaults; no IdP admin settings anywhere.
- **Portal product order** reflects the closed loop: Forge (train/optimize) → Horizon
  (distribute to edge sites) → Link (device platform, last). NereidEdge is deployed
  independently at every edge site and is never a central portal entry.
- **No speculative complexity**: Keep thin. Solve at boundary (gateway/auth/portal), delete>replace>add.

License header: All hand-written source files start with `// Copyright (C) 2026 YIQISOFT` and
`// SPDX-License-Identifier: Apache-2.0` (use language-appropriate comment style).