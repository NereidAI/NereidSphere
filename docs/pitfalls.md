# NereidSphere pitfalls

Copyright (C) 2026 YIQISOFT
SPDX-License-Identifier: Apache-2.0

Lessons discovered while building and operating NereidSphere. New pitfalls
must be appended in the same change that fixes or records the issue; do not
rewrite history casually.

## 1. Identity-service setup is an operator responsibility

- **Symptom:** OIDC discovery succeeds, but client login or Admin REST calls
  fail after switching a fresh installation to `auth.mode: oidc`.
- **Root cause:** realm import alone does not necessarily configure exact
  redirect URIs, token mappers, or the Admin REST service-account role grant.
- **Rule:** create and configure both clients through the identity provider's
  supported administrative interface using `docs/oidc-integration.md`. NS does
  not bootstrap a realm, store an engine administrator password, or patch an
  identity-provider deployment.

## 2. Service-account token works but Admin REST returns 403

- **Symptom:** the `nereidsphere-admin` client obtains a
  client-credentials token, but every `/admin/realms/...` call returns
  403.
- **Root cause:** client_credentials returns the service-account token even
  without role mappings, and the `realm-management/realm-admin` composite
  grant is not represented by a normal realm import.
- **Rule:** NS treats this as an identity-plane deployment issue. It stores no
  admin/master password and never assigns realm roles itself; a 403 from
  Admin REST is a deployment/identity-plane issue, surfaced to operators as
  `IDENTITY_FORBIDDEN`.
- **Files:** `internal/admin/client.go`,
  `docs/oidc-integration.md`.

## 3. Keycloak relative path changes Admin root derivation

- **Symptom:** admin/issuer URLs 404 in dev even though discovery works.
- **Root cause:** production Keycloak may run with
  `KC_HTTP_RELATIVE_PATH=/auth`, adding one path segment before both
  `/realms/` and `/admin/`.
- **Permanent rule:** derive the Admin API root by splitting the issuer at
  `/realms/`: dev → `http://localhost:8090/admin/realms/nereidai`,
  ingress → `https://sso.example.com/auth/admin/realms/nereidai`. Never
  hard-code environment-specific hosts.
- **Files:** `internal/admin/client.go` (`parseRealmIssuer`).

## 4. curl cookie jar silently drops oversize cookies

- **Symptom:** PKCE login succeeds and `Set-Cookie: ns_session=...` is
  visible, but the next curl request appears unauthenticated. Browser
  behavior can differ depending on the exact cookie length.
- **Root cause:** the sealed cookie initially exceeded practical browser and
  HTTP-client size limits; cookie storage behavior varies by tool.
- **Workaround:** for command-line tests, copy the `ns_session` value
  manually from `Set-Cookie` and send it with a `Cookie:` header. Do not
  rely solely on a cookie jar.
- **Permanent rule:** keep session state compact and test the browser
  flow.

## 5. Sealed session cookie must fit below the browser 4 KB limit

- **Symptom:** real OIDC login returned to the portal and immediately looked
  unauthenticated; the browser never sent `ns_session`.
- **Root cause:** the session stores access, id and refresh JWTs. Before
  compression the sealed cookie was about 4365 bytes, above the approximate
  4096-byte per-cookie limit. Some browsers silently discard it.
- **Fix:** `Codec.seal/open` deflates the JSON before AEAD (version marker
  byte; legacy uncompressed blobs still open). Real NI session measured
  4365 -> 2659 bytes. Regression test seals three JWT-sized tokens and
  asserts < 4000.
- **Permanent rule:** anything stored in a cookie must have a size-budget
  test; verify browser flows in a browser, not only via curl.
- **Files:** `internal/auth/session.go`, `internal/auth/session_test.go`.

## 6. Redirect URI registration is exact

- **Symptom:** NI login shows `Invalid parameter: redirect_uri`.
- **Root cause:** Keycloak matches registered redirect values exactly,
  including scheme, host, port and path. Dev registrations include only
  `http://localhost:8081/auth/callback` and
  `http://localhost:5174/auth/callback` when Vite is used.
- **Rule:** client registrations belong to the identity-service operator. NS
  must not create clients at runtime. Register each required redirect exactly
  and keep NS redirect validation strict.

## 7. Standalone OIDC products do not use NS ticket secrets

- **Symptom:** products configured `mode: oidc` still required a ticket
  shared secret in prod.
- **Root cause:** only `mode: ns` products exchange NS handoff tickets.
  Standalone NI OIDC clients use authorization-code/PKCE directly and need no
  `sso_secret` in NS config.
- **Rule:** config and Helm validation must distinguish the two modes.

## 8. Product mode and portal order are separate concepts

- **Symptom:** confusion about whether NereidEdge belongs in the central
  portal.
- **Rule:** the central catalogue is Forge → Horizon → Link. NereidEdge runs
  independently at edge sites and is intentionally not a central portal
  product. Do not add Edge to NS product config, OpenAPI or UI.

## 9. Effective Link tenant is not the all-zero UUID

- **Symptom:** temporary NereidLink OIDC testing failed during tenant
  resolution despite a valid NI login.
- **Root cause:** Link's seeded default tenant is
  `00000000-0000-0000-0000-000000000001`; the all-zero UUID does not exist.
- **Rule:** NS only supplies identity. Tenant/JIT mapping belongs to the
  product; do not invent tenant values in NS.

## 10. Deployment topology and prod `sso_secret`

- **Namespace:** NereidSphere is installed in the product-owned
  **`nereidsphere`** namespace. The OIDC realm is still `nereidai`; the realm
  name and Kubernetes namespace are different concepts. Image pull secret and
  the copied internal-CA Secret live in `nereidsphere`.
- **Symptom:** the pod CrashLoops with
  `products[forge].sso_secret is required in prod` even though every product
  is configured `mode: oidc`.
- **Root cause:** prod validation required an NS ticket shared secret for
  every product; standalone NI OIDC clients never exchange tickets.
- **Fix:** validation skips `sso_secret` when `mode=oidc`
  (`internal/config/config.go`); the Helm helper already only renders the
  key for `mode=ns` products.
- **Cluster realm facts (dev k3s):** issuer is
  `https://sso.example.com/auth/realms/nereidai` (Keycloak runs with
  `KC_HTTP_RELATIVE_PATH=/auth`; `/auth` appears once after the host),
  pods reach it over the same URL and need the `internal-ca` bundle mounted
  (`config.oidc.caBundleSecret`), and the realm ships with no users — the
  first user is created via the realm-admin `nereidsphere-admin` service
  account after the NI Helm bootstrap grants the role.

## 11. Account console must prevent administrator lockout

- **Risk:** an inline enable switch or role selector can let an administrator
  disable their own account, remove their own administrator role, or remove
  the last enabled administrator. Recovery then requires direct identity-plane
  intervention.
- **Rule:** NS refuses self-disable/delete/admin-removal and any operation
  that would remove the last enabled administrator. The stable codes are
  `SELF_ACTION_FORBIDDEN` and `LAST_ADMIN_PROTECTED`. The UI additionally
  confirms destructive or high-impact actions and requires typing the
  username before permanent deletion.
- **Boundary:** this is safety logic for NS's console surface; it does not
  replace audits or controls inside the identity engine itself.

## 12. Process memory cannot enforce single-use tickets across NS pods

- **Symptom:** two `ticket/verify` calls routed to different NS pods can both
  observe a fresh JTI during the 30-second ticket lifetime.
- **Root cause:** the ticket itself is sealed and therefore portable, but the
  consumed-JTI marker is only shared if all replicas use external storage.
- **Fix:** production uses Valkey. NS claims the audience-scoped key with
  `SET ns:ticket:v1:<audience>:<jti> 1 NX PX 30000` on the Sentinel-reported
  master and requires one replica acknowledgement with `WAIT 1 300`.
- **Failure rule:** discovery errors, write errors, timeouts or a zero replica
  acknowledgement are uncertain results and must return HTTP 503
  `TICKET_STORE_UNAVAILABLE`. Never accept an exchange when single-use cannot
  be confirmed.
- **Topology rule:** run two data pods and three Sentinels on at least three
  worker nodes. A two-node lab can leave the third Sentinel Pending; this is
  functional validation only, not node-loss HA.

## 13. Product groups are read from the access token, not the id token

- **Symptom:** the handoff ticket carried an empty `groups` array even though
  the user had product assignments in NI.
- **Root cause:** NS reads `groups` from the access token (mirroring
  `realm_access.roles`), and the NI group-membership mapper was only attached
  as a client scope without `access.token.claim=true`.
- **Rule:** NI emits `groups` (full paths) on the access token via the
  `nereidsphere` default client scope; NS keeps only the `/nereid*` suite
  namespace and drops engine defaults such as `/user`. Missing `groups` is
  not an error — the identity simply carries no product assignments.
- **Files:** `internal/auth/claims.go` (`GroupsFromAccessToken`),
  `docs/oidc-integration.md`.

## 14. `internal/auth` cannot import `internal/admin` (import cycle)

- **Symptom:** populating `Identity.Groups` wanted `admin`'s group-naming
  helper; adding the import created a cycle.
- **Root cause:** `internal/admin` imports `internal/auth` (it builds on the
  session/resolver types), so `auth -> admin` is rejected by the compiler.
- **Rule:** the group namespace prefix `/nereid` is duplicated in
  `internal/auth/claims.go` (`suiteGroupPrefix`) with a comment pointing at
  `internal/admin`. Keep the two in sync; a neutral shared package would be
  the alternative if the convention grows.

## 15. Tenant-list integration must be authenticated, not just proxied

- **Risk:** a product's origin is directly reachable, so a hand-crafted
  request with `X-User-Roles: admin` could call the tenant-list path the
  console uses, unless the product can tell it came from NS.
- **Rule:** the gateway stamps `X-NS-Integration-Secret` (the product's own
  `sso_secret`, already shared for ticket exchange) on exactly
  `products[].tenants_path`, and strips any inbound copy. Products verify it
  with a constant-time compare on that path. `X-User-*` headers alone are not
  proof of origin.
- **Files:** `internal/gateway/proxy.go`, `docs/gateway.md`.

## 16. RP-initiated logout: post-logout redirect URI must match exactly

- **Symptom:** clicking logout in NS landed on the identity service's
  "We are sorry... Invalid redirect uri" error page (HTTP 400).
- **Root cause:** NS built `post_logout_redirect_uri` as
  `strings.TrimSuffix(server.base_url, "/")+"/"`, sending
  `https://nereidsphere.example.com/`, while the client's registered
  post-logout redirect URI is the origin with **no** trailing slash
  (`https://nereidsphere.example.com`). The engine validates
  `post_logout_redirect_uri` by exact match, unlike `redirect_uri` which
  allows a trailing `*` wildcard.
- **Rule:** send the trimmed `server.base_url` verbatim — no trailing slash.
  The identity plane derives the registered value from the client's origin, so
  NS must not decorate it. Verified the failing call returns 400 with the slash
  and 302 without it.
- **Files:** `internal/auth/handlers.go` (`logoutBrowser`),
  `docs/sso-flow.md`.

## 17. Keycloak 26 never expands nested `subGroups` in the group listing

- **Symptom:** creating a second user with the same product/tenant/role
  answered `502 IDENTITY_ERROR "failed to assign product roles"`; the identity
  service logged `duplicate key value violates unique constraint
  "sibling_names"` for a group that already existed.
- **Root cause:** `GET /admin/realms/{realm}/groups` returns only top-level
  groups with a `subGroupCount` — nested `subGroups` come back empty (verified
  on Keycloak 26.1: a parent reported `subGroupCount: 2` with
  `"subGroups": []`, and the count itself can lag). NS flattened that listing
  and so could never see `/nereid{product}/{tenant}/{role}` below the first
  level. Every assignment therefore looked "missing", re-created it, and the
  engine answered 400/500 (`sibling_names`); the adopt-on-conflict retry used
  the same blind listing, so it never saw the winner either. The first user of
  a product worked only because every segment was genuinely new.
- **Rule:** resolve group paths one level at a time — top level via
  `GET /groups`, deeper levels via `GET /groups/{id}/children` — and match by
  name. Never assume the listing is a full tree. The test harnesses now mirror
  the engine (top-level listing without `subGroups`) so unit tests cannot
  regress into the flat-tree assumption.
- **Related:** reconciliation must only touch suite groups. `extractGroupPaths`
  returns *all* of a user's groups, so the diff is filtered through
  `ParseProductGroup` — otherwise assigning a product role would silently
  strip the user from unrelated groups (engine defaults such as `/user`).
- **Files:** `internal/admin/niadmin.go` (`groupChildren`, `findChild`,
  `EnsureGroupExists`, `FindGroupByPath`), `internal/admin/handler.go`
  (`suiteGroupPaths`), `docs/oidc-integration.md`.

## 18. Keycloak 26 rejects a profile-update PUT carrying `"username":""`

- **Symptom:** editing a user's profile (email / first/last name) and saving
  returned `502 IDENTITY_ERROR "identity service returned status 400"`; NI
  answered `{"errorMessage":"User name is missing"}` (trace
  `50786eec78a09deb53250c5ee7e43f7b`). Creating users and changing enabled
  state were unaffected.
- **Root cause:** the shared `keycloakUser` representation (`internal/admin/users.go`)
  serialised `Username` as `json:"username"` **without `omitempty`**. A partial
  update built from an empty struct (only email/firstName/lastName set) therefore
  produced a body with an explicit empty-string username. Keycloak's
  `PUT /users/{id}` treats present-but-blank username as "missing", whereas a
  body that simply omits the field is an accepted partial update (verified
  directly against NI: no-username body → 204, `"username":""` → 400).
- **Rule:** partial Admin REST updates must never send blank scalar fields as
  explicit values; put `omitempty` on every scalar JSON tag of a shared create+update
  representation, or use dedicated request bodies.
- **Files:** `internal/admin/users.go` (`keycloakUser.Username`),
  `internal/admin/updateuser_test.go`.

## 19. Default-deny must not lock out the bootstrap suite administrator

- **Symptom:** immediately after the default-deny product gate shipped, the
  bootstrap realm `admin` could open the portal but every product card was
  greyed out; hand-typed `/auth/enter/{product}` bounced back with
  `product_error=not_assigned`. Fresh deployments have no assignment groups
  yet, so a strict "groups only" gate locks the administrator out of every
  product on day one.
- **Rule:** identity-only sessions are denied, **except** holders of the
  realm `admin` role. `auth.CanEnterProduct(groups, roles, product)` is the
  single decision point: assignment group OR suite admin. `operator`
  deliberately stays denied (it is the account-console role, not a product
  grant). Products independently map the ticket's realm `admin` role to
  their highest privilege on the callback; they must not do so for
  `operator`.
- **Files:** `internal/auth/claims.go` (`CanEnterProduct`,
  `IsSuiteAdministrator`, `SuiteAdminRole`), `internal/auth/handlers.go`
  (`enterProduct`), `internal/portal/portal.go` (`assigned`),
  `docs/sso-flow.md` (Default-deny product authorization).

## 20. Suite roles read/write mismatch: the baseline `user` is inherited, not direct

- **Symptom:** changing an account's suite roles showed the success toast
  but the list column stayed empty (most obvious when saving just `user`).
- **Root cause:** two different views of the same role mapping. The realm
  default role `default-roles-nereidai` composites the `user` role, so
  `GET /users/{id}/role-mappings/realm/composite` returns `user` for
  **every** account even though nobody (ordinarily) holds it directly.
  `SetRoles` diffed against that composite view — saving only `user` found
  the role "already present" and wrote nothing (204 no-op) — while the list
  projected roles from `GET /roles/{role}/users`, which lists **direct**
  members only, so the inherited baseline never appeared.
- **Rule:** treat `user` as an implicit baseline NS neither grants nor
  removes; reconcile only the privileged roles (`operator`/`admin`) as
  direct mappings, and render the list from **effective** roles
  (`effectiveSuiteRoles` = direct suite grants + baseline, in
  `user|operator|admin` order). The mock realm mirrors the real one
  (composite always carries the baseline) so this cannot regress.
- **Files:** `internal/admin/client.go` (`ListUsers`, `SetRoles`,
  `effectiveSuiteRoles`, `BaselineSuiteRole`),
  `internal/admin/roles_view_test.go`, `internal/admin/admin_test.go`.

## 21. Portal API text bypassed i18n and product launch replaced the portal

- **Symptom:** the product cards stayed English after switching to Chinese,
  common suite roles also appeared as raw English identifiers, and launching
  a product replaced the portal window.
- **Root cause:** `PortalPage.tsx` rendered `product.description` and role
  identifiers directly from the API and called `window.open` with `_self`.
  Locale-key parity checks cannot detect API text rendered without a `t(...)`
  call site.
- **Permanent rule:** translate built-in product descriptions and common
  roles through static locale keys, retaining API text only as the fallback
  for unknown site extensions. Open product handoff URLs with `_blank` and
  `noopener,noreferrer` so the portal remains available without exposing
  `window.opener`. Update both registered portal help pages and verify with
  `npm run check`, `npm run typecheck`, and `npm run build`.
- **Files:** `web/src/pages/PortalPage.tsx`, `web/src/locales/{en,zh}.ts`,
  `web/src/docs/{en,zh}/{portal-overview,product-sso}.md`, `FEATURES.md`.

## 22. Tenant picker pointed at product-domain APIs instead of the NS integration contract

- **Symptom:** creating a suite account loaded
  `/p/forge/api/v1/tenants` and `/p/horizon/api/organizations`, both
  returning 401, while `/p/link/v1/integration/ns/tenants` worked.
- **Root cause:** Forge and Horizon implemented the shared
  `GET /v1/integration/ns/tenants` endpoint, but NS retained their old
  product-domain paths in `tenants_path`. The gateway deliberately stamps
  `X-NS-Integration-Secret` only on the exact configured path, so these
  product-domain calls had no integration credential and their own product
  session authentication returned 401.
- **Permanent rule:** every built-in product uses
  `/v1/integration/ns/tenants` as `tenants_path`. Never point the suite
  picker at a product's own tenant/organization API. When the integration
  contract changes, align development config, chart defaults, the deployed
  values, and the config comment in one change. Verify authenticated 200
  responses through `/p/{product}{tenants_path}`; an empty JSON array is a
  valid product-owned tenant list, not an authentication failure.
- **Files:** `configs/dev.yaml`, `deploy/helm/values.yaml`,
  `internal/config/config.go`, `docs/gateway.md`,
  `docs/oidc-integration.md`.

## 23. Composite form field generated a dangling label target

- **Symptom:** opening an account Modal reports that a label's `for`
  attribute does not match any element id; the target ends in
  `product_roles`.
- **Root cause:** one `Form.Item` generated a single field id for a custom
  editor containing multiple tenant and role inputs. The editor is not a
  native labelled control and never rendered that generated id, leaving the
  label target unresolved.
- **Permanent rule:** a multi-input form value must not use the implicit
  single-control `label for`. Suppress that association on the composite
  item, give the editor a named group, and provide a stable id and accessible
  name for every tenant/role control. Name each account form so ids from
  separate create, edit and reset forms cannot collide. Verify rendered DOM,
  not only TypeScript: every `label[for]` must resolve, ids must be unique,
  and every input must have an accessible name. The temporary jsdom smoke
  harness used for this fix checked those invariants against both the legacy
  failing pattern and the corrected pattern.
- **Files:** `web/src/pages/AdminUsersPage.tsx`,
  `web/src/components/ProductRolesEditor.tsx`.

## 24. A multi-architecture manifest can still contain the wrong binary

- **Symptom:** an arm64 pod entered `CrashLoopBackOff` with exit code 255
  after pulling a newly published image. The registry showed both amd64 and
  arm64 variants, but the arm64 variant contained an x86-64 ELF executable.
- **Root cause:** the Dockerfile redeclared BuildKit's automatic platform
  arguments as `ARG TARGETOS=linux` and `ARG TARGETARCH=amd64`. Those defaults
  overrode the per-platform values, so the arm64 stage ran `GOARCH=amd64`.
- **Permanent rule:** declare `ARG TARGETOS` and `ARG TARGETARCH` without
  defaults inside the build stage. A manifest-list inspection alone is not
  sufficient: check the build log's `GOOS`/`GOARCH` values and inspect the
  executable from each variant (`docker pull --platform`, `docker create`,
  `docker cp`, then `file`) before rolling out to mixed-architecture nodes.
- **Files:** `build/Dockerfile`, `Makefile`.

## 25. Containerized OIDC requires a shared issuer address

- **Symptom:** an OIDC browser login can display the identity provider login
  page, but NS discovery, code exchange, token refresh, or `/readyz` fails from
  the container. The reverse mismatch is also possible.
- **Root cause:** the issuer is both a browser-facing and a server-facing URL.
  `localhost` in the browser means the host, while `localhost` inside the NS
  container means that container. A Docker service name may work only inside
  the container network and fail in the browser.
- **Permanent rule:** use one issuer hostname/origin that resolves and routes
  to the same identity service from both network contexts. Do not silently
  rewrite the issuer between redirects and server-side calls: OIDC issuer and
  discovery metadata must match exactly. The Compose health check uses
  `/healthz` for process health; check `/readyz` separately for issuer
  reachability. Treat `docker compose config` output as sensitive because its
  mounted configuration contains expanded secrets.
- **Files:** `docker-compose.yaml`, `.env.example`,
  `docs/oidc-integration.md`.

## 26. Compose inline config content conflicts with a read-only service

- **Symptom:** container creation fails with `file is the sole supported option`
  after `docker compose config` has already accepted the model.
- **Root cause:** Compose supports top-level `configs[].content` for inline
  environment-expanded configuration, but cannot materialize that content in a
  service marked `read_only`. Static validation does not catch this runtime
  restriction.
- **Permanent rule:** run an actual container smoke test in addition to
  `docker compose config`. For the inline OIDC configuration, keep the mounted
  config file read-only but leave the service's remaining filesystem writable;
  retain the image's non-root user, dropped capabilities, and `/tmp` tmpfs.
  A service-wide `read_only` setting requires a pre-rendered `configs[].file`
  instead.
- **Files:** `docker-compose.yaml`, `Makefile` (`compose-config`).
