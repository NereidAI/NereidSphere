<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Delivered Features Inventory

Protected inventory of shipped NereidSphere capabilities. Do not weaken or
remove a listed feature without an explicit request; keep this file updated
in the same change that delivers a feature.

## Authentication & SSO

- **Suite SSO entry** — `GET /auth/login?redirect=…`, strict redirect
  allowlist (relative NS paths or exact configured product origins).
- **Authorization-code + PKCE login** (`auth.mode=oidc`) against
  NereidIdentity: S256 verifier, sealed `state`/`nonce`/PKCE handshake
  cookie (`ns_auth_flow`, 10-minute TTL), server-side code exchange, JWKS
  verification of issuer/audience/expiry/nonce, roles from access-token
  `realm_access.roles`, suite product groups from the access-token `groups`
  claim (filtered to the `/nereid*` namespace). ROPC is removed; the browser
  uses the NI login page.
- **Emergency local accounts** (`auth.mode=local`) — bcrypt accounts from
  the config/Secret, timing-equalized unknown-user checks, labeled as
  emergency in the UI.
- **Sealed session** — AES-256-GCM HttpOnly `ns_session` cookie, idle TTL
  (8h) and absolute TTL (24h), transparent OIDC refresh with role and
  product-group re-read, auto-cleanup of invalid cookies. No session database.
- **Logout** — browser `GET /auth/logout` (RP-initiated end-session redirect
  in oidc mode) and XHR `POST /api/v1/auth/logout`; refresh-token
  revocation best effort.
- **Login-method discovery** — `GET /api/v1/auth/method` for the SPA.
- **Startup resilience** — OIDC discovery is lazy; an unreachable identity
  plane never blocks startup; `/readyz` reports issuer reachability.

## Cross-product SSO (ticket hub)

- **Product entry** — `GET /auth/enter/{id}?to=…`; per-product
  `mode: ns|oidc`; `to` restricted to relative paths.
- **Default-deny product authorization** — an NS session is identity only;
  entry requires at least one `/nereid{product}/{tenant}/{role}` group,
  **or the realm `admin` role** (implicit suite-wide administrator, so the
  bootstrap admin is never locked out; `operator` is not exempt).
  `/auth/enter/{id}` redirects unassigned non-admin sessions to the portal
  with `?product_error=not_assigned` and mints no ticket (both modes);
  `GET /api/v1/products` reports per-product `assigned` so the portal greys
  unassigned products out. Products map realm `admin` to their own
  administrative role on the ticket callback.
- **Stateless sealed tickets** — 30-second TTL, single-use `jti`, audience
  binding, purpose tag, AEAD-sealed with the session key so any replica can
  verify; failed exchanges still burn the ticket.
- **Shared replay storage** — process-local memory for dev/single replica or
  Valkey for multi-replica deployments. Valkey Sentinel mode uses `SET NX PX`
  plus `WAIT 1`, and ticket exchange fails closed with
  `TICKET_STORE_UNAVAILABLE` when the write/replica acknowledgement is
  unavailable.
- **S2S exchange** — `POST /api/v1/auth/ticket/verify` with
  `X-NS-Client-Id`/`X-NS-Client-Secret` (constant-time compare), stable
  error codes (`UNAUTHORIZED`, `BAD_REQUEST`, `TICKET_INVALID`,
  `TICKET_EXPIRED`, `TICKET_AUDIENCE`, `TICKET_UNSUPPORTED`).
- **Forward-auth** — `GET /api/v1/auth/verify` → 204 + `X-User-*` headers /
  401 for ingress-nginx/Traefik.

## Admin console (oidc mode)

- `nereidsphere-admin` service account via client_credentials; no realm
  master password stored anywhere.
- Accounts: server-side search/pagination/count, get, create with cleanup,
  profile update, delete, enable/disable, permanent or temporary password
  reset, suite-role assignment (`user`/`operator`/`admin`), email-verified
  and required-action status.
- Federated/brokered users shown read-only with provider alias; mutations
  refused with `FEDERATED_READ_ONLY`.
- Safety guards refuse self-disable/delete/admin-removal and any operation
  that would remove/disable the last enabled administrator
  (`SELF_ACTION_FORBIDDEN`, `LAST_ADMIN_PROTECTED`).
- Successful and failed account changes emit structured audit logs with
  actor, target, action, source address and trace ID; credentials are never
  logged.
- Product tenant/role assignment: create and edit dialogs assign a
  tenant+role per suite product, stored as identity-service groups
  `/nereid{product}/{tenant}/{role}`; catalog of products/roles and
  runtime-fetched tenant lists via the gateway (authenticated with
  `X-NS-Integration-Secret` on the product's `tenants_path`); additive/
  idempotent group diff; assignments delivered to products at SSO time via
  the ticket identity `groups` claim and gateway headers; federated accounts
  remain read-only.
- Connection status API/UI: auth mode, issuer, issuer reachability, admin
  API availability, per-product integration mode.
- Authorization: session required; read for `admin`/`operator`; mutations
  `admin` only; `IDENTITY_FORBIDDEN` maps a missing SA role grant.

## Gateway

- Stdlib reverse proxy `/p/{product}/…` with session middleware, inbound
  `X-User-*` stripping and verified identity-header injection.
- Restrictive exact-origin CORS (defaults to NS + product origins),
  per-IP token-bucket limits, stricter local-login bucket, 30s upstream
  timeout, tracing headers, browser security headers (HSTS in prod).

## CSRF

- Double-submit `ns_csrf` cookie + `X-Csrf-Token` header on every
  browser-driven mutating `/api/` call; preflight OPTIONS and the S2S
  ticket exchange exempt.

## Portal / frontend

- React 18 + antd 5 + Vite SPA, embedded via `embed.FS` with history
  fallback; bilingual (English/中文) i18n with key-parity checks and a
  suite-branded SVG browser favicon.
- Product catalogue ordered along the closed-loop pipeline:
  **Forge → Horizon → Link** (NereidEdge is deployed per edge site and is
  not a central portal entry); launch URL always `/auth/enter/{id}` and opens
  in a new window with opener isolation. Built-in product descriptions and
  suite role labels are localized in English and Chinese.
- SSO login entry + emergency local form; commercial account console
  (search, server-side pagination, lifecycle actions, safety confirmations),
  connection page, and contextual bilingual help center.

## API documentation

- Embedded OpenAPI 3 contract (`docs/api/openapi.yaml`) and offline Swagger
  UI at `/api/docs` (vendored assets, no external CDN).

## Operations

- YAML config (`configs/dev.yaml`) with per-section validation; `make dev`
  works with zero external dependencies in local mode.
- Helm chart renders the runtime config Secret from values
  (`config.auth.mode`, `config.oidc.*` including the admin SA secret,
  per-product mode), or accepts an external `existingSecret`.
- Optional `config.oidc.caBundleSecret` mounts a cluster-internal CA
  (`ca.crt`) into the pod trust directory for issuer/Admin REST TLS.
- Installs into the product-owned `nereidsphere` namespace; NereidIdentity
  remains an independently deployed service, see `deploy/helm/README.md`.
- Helm defaults to two NS replicas with hostname anti-affinity, rolling
  update surge and a PodDisruptionBudget.
- Built-in Valkey topology deploys one primary, one replica and three
  Sentinels; ticket keys are ephemeral and persistence is disabled.
- No NS business database; identity plane (NereidIdentity) deployed independently.
- Secret-free GitHub Actions CI runs the full Makefile verification gate;
  matching `v*` tags publish the three-tag GHCR image set (VERSION,
  VERSION-timestamp and VERSION-git-sha — never `latest`) through the
  organization reusable release workflow and create a GitHub Release with the
  built-in token.
