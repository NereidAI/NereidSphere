<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# NereidIdentity OIDC Integration

NereidSphere is a **pure consumer** of NereidIdentity (NI). It contains no
identity bootstrap, no realm administration and no customer IdP concepts;
swapping the identity engine changes one YAML block. Customer identity
providers (Microsoft Entra ID, Okta, Auth0, Authing, AD FS, …) are always
brokered **behind** NI — NS never knows they exist.

## Connection block

Selected with `auth.mode: oidc` in the config file (rendered from the Helm
Secret in deployments):

```yaml
auth:
  mode: oidc

oidc:
  issuer: "http://localhost:8090/realms/nereidai"   # dev
  client_id: "nereidsphere"
  client_secret: "changeme"
  admin_client_id: "nereidsphere-admin"
  admin_client_secret: "changeme-admin"
  scopes: ["openid", "profile", "email"]
```

The redirect URI is fixed to `<server.base_url>/auth/callback` and must be
registered exactly in the identity realm. For local development, register:

- `http://localhost:8081/auth/callback` (embedded portal)
- `http://localhost:5174/auth/callback` (Vite dev server)

Also register `<server.base_url>` without a trailing slash as a post-logout
redirect URI. Other origins, including LAN addresses, require their own exact
registrations and a matching `server.base_url`.

Discovery is **lazy**: performed on first login rather than at startup, so a
temporarily unreachable identity plane never blocks the NS process. The
`/readyz` probe and the admin Connection page report live issuer
reachability.

## Relying-party contract

- Flow: OAuth2 **authorization-code + PKCE (S256)**; `state` + `nonce`
  sealed in a short-lived HttpOnly cookie.
- Client `nereidsphere`: `standardFlowEnabled=true`,
  `directAccessGrantsEnabled=false` (no ROPC), PKCE required.
- Id token: RS256 via JWKS; verified for issuer, audience, expiry and
  nonce. Claims consumed: `sub`, `preferred_username`, `name`, `email`.
- Access token: suite roles are read from `realm_access.roles`; built-in
  roles (`offline_access`, `uma_authorization`, `default-roles-*`) are
  filtered out. Suite roles are exactly `user`, `operator`, `admin`.
- Access token `groups`: NS reads suite product tenant/role assignments
  from a `groups` claim holding NI realm-group paths
  (`/nereid{product}/{tenant}/{role}`) and keeps only the suite namespace
  (engine defaults such as `/user` are dropped). The identity plane emits it
  with a group-membership mapper (`claim.name=groups`, `full.path=true`,
  `access.token.claim=true`) attached as a default scope to the
  `nereidsphere` client. When the claim is absent NS simply carries no
  product groups; login is unaffected.
- Refresh: NS stores the refresh token inside the sealed session cookie and
  refreshes transparently; logout best-effort calls the revocation and
  end-session endpoints.

## Admin service account

User administration (the NS admin console) uses the separate
`nereidsphere-admin` client with `serviceAccountsEnabled=true` and the
OAuth2 **client_credentials** grant against NI Admin REST:

```
<admin-root>/admin/realms/nereidai/...
```

`<admin-root>` is derived from the issuer:

| Environment | Issuer | Admin root |
| --- | --- | --- |
| local development | `http://localhost:8090/realms/nereidai` | `http://localhost:8090/admin/realms/nereidai` |
| ingress/Keycloak | `https://sso.example.com/auth/realms/nereidai` | `https://sso.example.com/auth/admin/realms/nereidai` |

NS never holds a Keycloak `admin`/master password. For a compatible Keycloak
realm, open the `nereidsphere-admin` client's service-account role assignment,
select the `realm-management` client roles, and assign `realm-admin`. The
identity operator must automate the equivalent grant in managed deployments;
a realm import alone may not establish it. Until that grant exists, the SA can
mint tokens but Admin REST returns 403 — the NS console maps this to
`IDENTITY_FORBIDDEN`.

Console capabilities: server-side search/pagination and count, get one
account, create local users with partial-create cleanup, update profile
fields, delete, enable/disable, permanent or temporary password reset, and
assign suite roles. Email verification status and required actions are
displayed. The service also blocks self-disable/delete/admin-removal and
operations that would remove the last enabled administrator. Accounts from
brokered providers (`federationLink` / `federationIdentities`) are shown
read-only with the provider alias; mutations return
`FEDERATED_READ_ONLY` and are managed in the upstream provider.

## Product tenant/role assignment

Beyond suite roles, the console assigns a **tenant and role per product**.
The mapping is expressed purely with identity-service groups, so NS stores no
product-specific concept:

```
/nereid{product}/{tenant}/{role}
```

with `product` in `link`, `forge`, `horizon`, and `role` one of the product's
declared role keys (Link `admins|engineers|read_only`, Forge
`admin|operator|user`, Horizon `platform|partner`). Each product maps these
groups to its own roles with its own configuration; NS never interprets a
tenant id.

- `GET /api/v1/admin/product-catalog` lists the products that participate,
  their selectable roles and each product's `tenants_path`.
- `GET`/`PUT /api/v1/admin/users/{id}/product-roles` read and reconcile a
  user's group membership. Reconciliation is additive and idempotent and
  applies only to suite groups (`/nereid{product}/{tenant}/{role}`): groups
  outside that namespace are never created, joined or left. The hierarchy is
  resolved one level at a time, so an existing assignment group is reused
  instead of re-created.
- The console loads each product's tenant list at **runtime through the
  gateway** (`/p/{product}{tenants_path}`) with the administrator's session,
  so tenant data stays owned by the product. For all built-in products,
  `tenants_path` is `/v1/integration/ns/tenants`; the product's own
  `/api/v1/tenants` or `/api/organizations` APIs are not this integration
  contract. The tenant field also accepts a typed id when a product exposes
  no list. The gateway stamps
  `X-NS-Integration-Secret` (the product's `sso_secret`) on exactly that call
  so the product can authenticate the NS-originated request on its directly
  reachable origin (see `docs/gateway.md`).
- The assignment is delivered to the product at SSO time: NS carries the
  suite groups in the identity of the handoff ticket and in the gateway
  `X-User-*` headers, so a mode-`ns` product maps them to its own roles.
- Assignment requires the admin service account (OIDC mode); `auth.mode:
  local` answers `NOT_FOUND`. Federated accounts stay read-only.

## Local emergency fallback

`auth.mode: local` authenticates against bcrypt accounts declared in the
deployment config/Secret:

```yaml
auth:
  mode: local
  local_users:
    - username: admin
      password_bcrypt: "$2a$10$..."
      roles: ["admin", "user"]
```

- `make dev` defaults to local mode with seeded `admin/admin` and
  `user/user` accounts (dev only — change everywhere real).
- Local sessions carry no upstream tokens; there is no refresh/revocation
  and no admin user API (answers 404).
- Labeled "Emergency access" in the UI; regular users always authenticate
  through NI.

## Operational notes

- The identity plane is deployed independently (its own compose in dev, its
  own Helm release in K8s/K3s). NS ships no Keycloak container, chart,
  bootstrap job or admin credential.
- NS never registers clients/realms: client registrations, redirect
  allowlists, role mappings and brokers are managed by the identity-service
  operator through that provider's supported interfaces.
- Secrets arrive via the deployment Secret; never commit real values. NS
  never logs tokens or secrets and masks them out of every response.
- Interactive contract: `/api/docs` embeds the OpenAPI document and Swagger
  UI; see `docs/gateway.md` for the edge rules.
