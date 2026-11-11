<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# SSO Flow

This document describes the suite-wide SSO model implemented by NereidSphere
(NS): authorization-code + PKCE login against NereidIdentity (NI) or a
compatible OIDC provider, the cross-product ticket hub, sessions and logout.
The provider-facing contract is specified in `docs/oidc-integration.md`.

## Overview

NereidSphere is the central SSO entry point for the suite. In `oidc` product
mode, integrated products may also act as standalone OIDC clients of the same
identity provider:

- `auth.mode=oidc` (default): the browser is redirected to NI's own branded
  login page over OAuth2 authorization-code + PKCE. NS never collects the
  upstream password and never exposes upstream tokens to JavaScript.
- `auth.mode=local`: emergency break-glass login against bcrypt accounts
  declared in the deployment Secret. Labeled as emergency in the UI and
  intended only for incidents/recovery.

After signing in once at NS, users open every integrated product without
signing in again.

## Login Flow (authorization-code + PKCE)

1. An unauthenticated visitor hits a protected page or a product redirects to
   `GET /auth/login?redirect=<allowlisted destination>`.
2. NS resolves the `redirect` against the strict allowlist (relative NS path
   or an exact configured product origin) and, when no session exists,
   generates a `state`, a `nonce` and a 64-byte PKCE verifier. These are
   sealed (AES-256-GCM) into the short-lived HttpOnly `ns_auth_flow` cookie
   (10-minute TTL).
3. The browser is redirected (302) to the NI authorization endpoint:

   ```
   GET <issuer>/protocol/openid-connect/auth
     ?response_type=code
     &client_id=nereidsphere
     &redirect_uri=<ns-base>/auth/callback
     &state=<state>
     &nonce=<nonce>
     &code_challenge=BASE64URL(SHA256(verifier))
     &code_challenge_method=S256
     &scope=openid profile email
   ```

4. The user authenticates on the NI-branded page (including any brokered
   customer IdP or MFA). NI redirects back to `GET /auth/callback?code&state`.
5. NS validates `state` against the sealed flow cookie (CSRF on the
   redirect), burns the cookie, and exchanges the code **server to server**
   with `code_verifier` at the token endpoint.
6. NS verifies the id token against the issuer JWKS: signature, issuer,
   audience (`nereidsphere`), expiry and `nonce`. Identity claims
   (`sub`, `preferred_username`, `name`, `email`) come from the id token;
   suite roles (`user`, `operator`, `admin`) are read from the access
   token's `realm_access.roles`, and product tenant/role assignments from
   its `groups` claim (`/nereid{product}/{tenant}/{role}`, filtered to the
   suite namespace).
7. NS seals the identity and the upstream tokens into the `ns_session`
   HttpOnly cookie (AES-256-GCM) and redirects to the original destination.

Failure handling:

- `error=...` from NI → redirect `/login?error=sso_denied`.
- State mismatch / missing flow cookie / exchange or verification failure →
  `/login?error=sso_failed`.
- NI unreachable at flow start → `/login?error=identity_unavailable`; startup
  itself never fails (discovery is lazy), and `/readyz` reports the issuer.

The legacy ROPC (`grant_type=password`) path has been removed: the NI realm
disables direct-access grants for `nereidsphere`, and NS never sees
passwords in oidc mode.

## Product integration modes

Each configured product declares `mode: ns` (default) or `mode: oidc`. The
portal launch URL is always `/auth/enter/{id}`; NS decides internally how to
continue.

### Mode `ns` — one-time handoff ticket (default)

The browser holds only an NS session cookie, so an independently hosted
product cannot rely on an NI cookie. NS hands off each product entry with a
short-lived, single-use, audience-bound **sealed ticket**.

1. Product card → `GET /auth/enter/{product}?to=<relative path>`.
2. NS resolves/refreshes the session; anonymous visitors are sent to
   `/auth/login?redirect=...` and return afterwards. A signed-in user
   **without** a `/nereid{product}/...` assignment group is redirected back
   to the portal with `?product_error=not_assigned&product=...` (see
   [Default-deny product authorization](#default-deny-product-authorization)).
3. NS mints a ticket: the payload (`aud` product id, `jti`, identity
   snapshot, `to`, `iat`, `exp`, 30-second TTL, fixed
   `ns-handoff-ticket-v1` purpose tag) is sealed with the same AES-256-GCM
   session key. Tickets are **stateless**: any NS replica sharing the key
   can mint or verify them.
4. The browser is 302-redirected to
   `{product.url}/auth/ns-callback?ticket=...&to=...`. `to` is restricted to
   a relative path, so the destination host cannot be forged.
5. The product backend immediately exchanges the ticket **server to server**:

   ```http
   POST /api/v1/auth/ticket/verify
   X-NS-Client-Id: link
   X-NS-Client-Secret: <products[].sso_secret>
   Content-Type: application/json

   {"ticket":"..."}
   ```

   Success:

   ```json
   {"identity":{"sub":"...","preferred_username":"admin",
                "email":"...","name":"...","roles":["admin","user"],
                "groups":["/nereidlink/acme/admins"]},
    "to":"/dashboard"}
   ```

   `groups` carries the user's suite product tenant/role assignments
   (`/nereid{product}/{tenant}/{role}`); the product selects the entries for
   its own token and maps the role suffix to its local role model.

6. The ticket is consumed exactly once: the audience-scoped `jti` is recorded
   in the configured replay store (single-use enforced even on failed
   exchanges). The product establishes its **own** local session. NS does not
   dictate product session mechanics.

Error codes: `UNAUTHORIZED` (bad/missing client secret), `BAD_REQUEST` (no
ticket), `TICKET_INVALID` (malformed/undecryptable/used), `TICKET_EXPIRED`,
`TICKET_AUDIENCE` (issued for another product), `TICKET_UNSUPPORTED` (the
product runs mode `oidc`), `TICKET_STORE_UNAVAILABLE` (the shared replay
store cannot safely confirm single use).

Multi-replica note: any replica verifies any ticket because the payload is
key-sealed. Development/single-replica installs may use process-local memory;
production installs use shared Valkey. In Sentinel mode NS performs
`SET key 1 NX PX ttl` on the master and waits for one replica acknowledgement
before accepting the exchange. If Valkey is unavailable or acknowledgement
cannot be confirmed, verification fails closed with HTTP 503; the product
should retry by sending the visitor through `/auth/enter/{product}` again.

### Mode `oidc` — standalone NI client

The product is itself a standard OIDC client of NI (its own
`oidc_client_id` and registered redirect URIs). `/auth/enter/{id}` redirects
the browser straight to the product origin; the NI SSO session makes entry
seamless. No ticket is minted and no `sso_secret` is required. There is no
mode 3 (direct-to-customer-IdP) in suite installs — customer IdPs are always
brokered behind NI.

## Default-deny product authorization

An NS session proves **identity**, not product authorization. Product
access is granted through the suite assignment groups
`/nereid{product}/{tenant}/{role}`, with a single exception for the realm
administrator. This is the suite-wide default-deny policy:

- A user with **no group for a product cannot enter it**. "Account exists in
  the console" only enables signing in to the NS portal and receiving
  assignments.
- **Exception — suite administrator.** Holders of the realm `admin` role
  (`IsSuiteAdministrator`) implicitly administer every product and enter
  without an assignment group, so the bootstrap admin is never locked out
  of a fresh deployment. Products map the ticket's realm `admin` role to
  their highest privilege (platform admin / tenant admin / `platform`).
  The `operator` role only grants account-console access and is **not**
  exempt; operators still need a product group to enter a product.
- Both modes are gated at `GET /auth/enter/{id}`: without a matching group
  (and without the realm `admin` role) NS responds
  `302 /?product_error=not_assigned&product=<id>` and never mints a ticket
  (mode `ns`) or redirects into the product (mode `oidc`). This is the
  enforcement boundary for hand-typed/deep links; the portal also greys the
  product out.
- `GET /api/v1/products` returns `assigned: true|false` per product from the
  session's groups and realm roles (false for anonymous callers; true for
  realm admins). The portal renders unassigned products disabled with a
  "Not assigned" tag.
- Products must enforce the same boundary on their own ticket callback:
  consume only their `/nereid{product}/...` entries and reject the login
  (do not issue a local session) when none is present. The only role-based
  exception is realm `admin`, mapped to the product's administrative role;
  `operator` and a fallback/default tenant must not be treated as product
  grants. The historical realm-role fallback existed for pre-groups NS and
  is being removed across products.
- Removing the last assignment for a product revokes access on the next
  login (products reconcile memberships from groups each SSO login); realm
  admins keep their implicit access.
- Emergency `local` accounts (break-glass) carry no NI groups; only a local
  account with the `admin` role may enter products. Local mode is otherwise
  for NS administration only.

## Gateway and forward-auth

- Authenticated requests may also traverse the unified reverse proxy
  `/p/{product}/...`, which verifies `ns_session`, strips inbound spoofed
  `X-User-*` headers and injects the verified identity headers.
- `GET /api/v1/auth/verify` is the ingress forward-auth endpoint: 204 with
  `X-User-Id/Email/Name/Roles` for a valid session, 401 otherwise.

## Logout

- `GET /auth/logout` (browser-followable, used by mode-1 product logout
  chains): clears `ns_session` and `ns_csrf`, best-effort revokes the
  upstream refresh token, then continues to NI RP-initiated logout
  (`end_session_endpoint`) with `id_token_hint` and
  `post_logout_redirect_uri=<ns-base>` (the trimmed `server.base_url`, **no**
  trailing slash), which returns the browser to NS. The engine matches this
  value exactly, so it must equal the client's registered post-logout redirect
  URI — the identity plane registers the product origin without a trailing
  slash, and a stray `/` yields an "Invalid redirect uri" error page.
- `POST /api/v1/auth/logout` (SPA XHR): clears cookies and revokes best
  effort, keeping the browser on the NS origin.
- In local mode both variants simply clear the cookies and (for GET)
  redirect to `/`.

## CSRF protection

All browser-driven state-changing requests under `/api/` must carry the
double-submit token: NS sets the JavaScript-readable `ns_csrf` cookie on
first contact and the SPA echoes it in `X-Csrf-Token`; requests without a
matching header/cookie pair are rejected with 403 `CSRF_FAILED`. The
server-to-server ticket exchange is exempt (it authenticates with
`X-NS-Client-Id/Secret`), as are CORS preflight `OPTIONS` requests.

## Session and token hygiene

- One sealed, stateless HttpOnly cookie (`ns_session`, AES-256-GCM, 32-byte
  key); no session database.
- Two lifetimes: **idle** timeout (default 8h, slid by activity) and an
  **absolute** cap (default 24h) after which re-login is required.
- The upstream access token is transparently refreshed with the refresh
  token while both windows allow; roles and product groups are re-read on
  refresh. Local sessions carry no upstream tokens.
- No bearer tokens in browser JavaScript or `localStorage`; no
  tokens/secrets logged.
- Redirects are allowlist-checked; product deep links (`to`) are relative
  paths on fixed configured hosts. Cookies use HttpOnly plus
  environment-appropriate Secure/SameSite flags.

---

## Emergency Local Login (NS Unavailable)

When NS is down (maintenance, failure, network partition), all products
running in `mode: ns` lose their SSO entry point. Each product independently
detects NS health via `GET /readyz` and gracefully degrades to a local
emergency login form.

See [Emergency Local Login Design](emergency-local-login.md) for the
complete contract, product integration guide, and deployment checklist.
