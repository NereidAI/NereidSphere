<!--
Copyright (C) 2026 YIQISOFT
SPDX-License-Identifier: Apache-2.0
-->

# NereidSphere Helm Chart

NereidSphere (NS) is the thin suite layer: suite SSO portal, OIDC relying
party, one-time handoff ticket hub, unified API gateway and the built-in
account administration console. The chart deploys a stateless two-replica
Deployment (sealed cookie sessions) plus an ephemeral Valkey topology for
cross-replica ticket single-use enforcement.

NereidIdentity (NI) is **not** deployed by this chart. It is an independent
release (its own Helm chart) providing Keycloak; NS is a pure consumer of its
OIDC discovery and Admin REST endpoints.

## Prerequisites

- Kubernetes 1.24+
- At least three worker nodes for the production HA topology (two NS pods,
  two Valkey data pods and three Sentinels use hostname anti-affinity)
- An independently deployed NereidIdentity realm reachable from:
  - the browser (public issuer URL, e.g.
    `https://sso.example.com/auth/realms/nereidai`)
  - the NS pods (same URL; trust the cluster internal CA when applicable)
- Two confidential clients pre-registered in the realm by the identity-service
  operator, following `docs/oidc-integration.md`:
  - `nereidsphere` — authorization-code + PKCE, redirect URI
    `<config.server.baseUrl>/auth/callback`
  - `nereidsphere-admin` — service account granted `realm-management/realm-admin`
    through the provider's administrative interface
- Access to the public GHCR NereidSphere image and the upstream Valkey image
  (or their node-level mirrors); configure `imagePullSecrets` only when the
  selected registry requires authentication.

## Configuration

| Key | Description | Default |
| --- | --- | --- |
| `replicaCount` | NS pod replicas; values above 1 require Valkey replay storage. | `2` |
| `valkey.enabled` | Deploy the built-in Valkey primary/replica/Sentinel topology. | `true` |
| `valkey.auth.password` / `existingSecret` | Data-node password; provide a strong value or an existing Secret with key `password`. Required for built-in Valkey. | none |
| `valkey.dataReplicas` / `sentinelReplicas` | Valkey read replicas and Sentinel voters. | `1` / `3` |
| `valkey.external.*` | Standalone/Sentinel connection settings and optional data/Sentinel passwords when `valkey.enabled=false`. | none |
| `tickets.dialTimeout` / `operationTimeout` | Valkey client timeouts. Sentinel writes additionally use `WAIT 1 300ms`. | `2s` / `1s` |
| `image.repository` / `image.tag` | Container image; empty tag uses chart appVersion. | `ghcr.io/nereidai/nereidsphere` / appVersion |
| `imagePullSecrets` | Existing pull secret names. | `[]` |
| `ingress.enabled` | Create an Ingress. | `false` |
| `ingress.className` / `hosts` / `tls` / `annotations` | Standard Ingress settings; the first host is shown in NOTES. | host `nereidsphere.example.com` |
| `config.existingSecret` | Use an externally managed Secret (key `config.yaml`) instead of the rendered one. | `""` |
| `config.server.baseUrl` | Public origin; used for launch URLs and the OIDC redirect URI. | `https://nereidsphere.example.com` |
| `config.auth.mode` | `oidc` (NI browser login, default) or `local` (emergency bcrypt accounts). | `oidc` |
| `config.auth.localUsers` | Break-glass accounts for `mode: local`; values are bcrypt hashes, never plaintext. | `[]` |
| `config.oidc.issuer` | Realm issuer URL. Discovery is lazy; startup does not fail when NI is down. | `https://sso.example.com/auth/realms/nereidai` |
| `config.oidc.clientId` / `clientSecret` | Confidential browser client. | `nereidsphere` / `changeme` |
| `config.oidc.adminClientId` / `adminClientSecret` | Client-credentials service account for the account console Admin REST calls. | `nereidsphere-admin` / `changeme-admin` |
| `config.oidc.caBundleSecret` | Optional Secret name (key `ca.crt`) trusted as an extra root CA for OIDC/Admin REST TLS. The file is mounted into `/etc/ssl/certs`. | `""` |
| `config.session.secretKey` | **Required**, exactly 32 bytes for AES-256 session/ticket sealing. | placeholder |
| `config.session.idleTtl` / `absoluteTtl` / `secure` | Session policy. | `8h` / `24h` / `true` |
| `config.gateway.*` | CORS allowlist (empty = server + product origins), rate limits, upstream timeout. | see values |
| `config.products[]` | Central-suite product catalogue in portal order; each has `id`, `name`, `description`, `url`, `mode`, `oidcClientId`, optional `ssoSecret`. NereidEdge is deployed per edge site and is intentionally not listed. | Forge, Horizon, Link |
| `resources` | Container requests/limits. | 100m/128Mi – 1/256Mi |

### Ticket replay storage

Ticket JTIs live for 30 seconds. The default built-in topology creates one
Valkey primary, one replica and three Sentinels, all with `emptyDir` storage
and persistence disabled. NS uses `SET key 1 NX PX ttl` to claim a JTI on the
Sentinel-reported master, then uses `WAIT 1 300` to require acknowledgement
from a replica. If discovery, the write, or the acknowledgement cannot be
confirmed, ticket verification fails closed with HTTP 503
`TICKET_STORE_UNAVAILABLE`.

The data password is supplied to NS through the `NS_VALKEY_PASSWORD`
environment variable from the named Secret and is not rendered in
`config.yaml`. A two-worker lab can run all NS and data pods, but the third
Sentinel remains Pending; it demonstrates function, not node-loss quorum.

Set `valkey.enabled=false`, `tickets.replayStore=valkey` and
`valkey.external.address` or `valkey.external.sentinel*` to use an externally
managed Valkey service. `tickets.replayStore=memory` is supported only for
single-replica development and the chart refuses to render multiple NS
replicas with that setting.

### Product modes

- `mode: ns` — NS mints a 30-second single-use sealed ticket at
  `GET /auth/enter/{id}` and redirects the browser to the product callback;
  the product backend exchanges it server-to-server at
  `POST /api/v1/auth/ticket/verify` using `ssoSecret` (`X-NS-Client-Id` /
  `X-NS-Client-Secret`). The chart fails rendering without `ssoSecret` for
  every `ns` product.
- `mode: oidc` — the product is a standalone NI OIDC client; the browser is
  redirected straight to the product URL. No `ssoSecret` required.

### Trusting an internal certificate authority

When the issuer is served with a cluster-internal CA (for example the
cert-manager `internal-ca` ClusterIssuer), copy its CA Secret into the
NereidSphere namespace and set `config.oidc.caBundleSecret`:

```sh
kubectl create namespace nereidsphere
kubectl -n cert-manager get secret identity-ca-tls -o yaml \
  | sed 's/namespace: cert-manager/namespace: nereidsphere/' \
  | kubectl apply -f -
```

## Install

Each product owns a namespace named after that product. NereidSphere is
installed in **`nereidsphere`**; the OIDC realm remains `nereidai`, while
NereidIdentity itself runs under its own independently managed release and
namespace.

Keep actual values outside Git and readable only by the operator. For example,
store them at `$HOME/.config/nereidsphere/values.yaml` with mode `0600`, then
run from the repository root:

```sh
export NS_VALUES="$HOME/.config/nereidsphere/values.yaml"
helm upgrade --install nereidsphere deploy/helm \
  --namespace nereidsphere --create-namespace \
  --values "$NS_VALUES"
```

Prerequisites inside `nereidsphere`:
- registry pull secret (e.g. `registry-pull-secret`)
- when the issuer uses the cluster internal CA, a copy of the CA Secret
  (`kubectl -n cert-manager get secret identity-ca-tls -o yaml | sed
  's/namespace: cert-manager/namespace: nereidsphere/' | kubectl apply -f -`)

Example production values (secrets injected at deploy time):

```yaml
ingress:
  enabled: true
  className: traefik
  tls:
    - hosts: ["nereidsphere.example.com"]
      secretName: nereidsphere-tls
imagePullSecrets:
  - name: registry-pull-secret

valkey:
  enabled: true
  auth:
    password: "<from sealed secret pipeline>"

config:
  server:
    baseUrl: "https://nereidsphere.example.com"
  auth:
    mode: "oidc"
  oidc:
    issuer: "https://sso.example.com/auth/realms/nereidai"
    clientId: "nereidsphere"
    clientSecret: "<from sealed secret pipeline>"
    adminClientId: "nereidsphere-admin"
    adminClientSecret: "<from sealed secret pipeline>"
    caBundleSecret: "identity-ca-tls"
  session:
    secretKey: "<exactly-32-bytes>"
  products:
    - {id: forge,   name: NereidForge,   url: "https://nereidforge.example.com",   mode: oidc, oidcClientId: nereidforge}
    - {id: horizon, name: NereidHorizon, url: "https://nereidhorizon.example.com", mode: oidc, oidcClientId: nereidhorizon}
    - {id: link,    name: NereidLink,    url: "https://nereidlink.example.com",    mode: oidc, oidcClientId: nereidlink}
```

Emergency local mode (no NI dependency; dev/disaster recovery only):

```yaml
replicaCount: 1
valkey:
  enabled: false
tickets:
  replayStore: memory
config:
  auth:
    mode: "local"
    localUsers:
      - username: admin
        name: Admin User
        passwordBcrypt: "$2a$10$..."  # htpasswd -bnBC 10 "" password | tr -d ':\n'
        roles: ["admin", "user"]
```

The chart fails fast when: built-in Valkey is enabled without a password or
existing Secret; external Valkey lacks an endpoint; multiple NS replicas use
the process-memory replay store; `oidc` mode lacks any of the five NI
connection values; `local` mode has no accounts; `session.secretKey` is not
32 bytes; or an `ns` product lacks `ssoSecret`.

## Upgrade / rollback

```sh
helm upgrade nereidsphere deploy/helm \
  --namespace nereidsphere --values "$NS_VALUES"
helm rollback -n nereidsphere nereidsphere
```

The rendered configuration is stored in the `nereidsphere-config` Secret
(key `config.yaml`, mounted read-only at `/app/config/config.yaml`). Changing
a `config.*` value rolls the Deployment; changing only a referenced
existing-Secret value requires a manual rollout restart. Check
`kubectl rollout status deploy/nereidsphere`.

## Verify

```sh
kubectl -n nereidsphere rollout status deploy/nereidsphere
kubectl -n nereidsphere rollout status statefulset/nereidsphere-valkey-primary
kubectl -n nereidsphere rollout status statefulset/nereidsphere-valkey-replica
kubectl -n nereidsphere rollout status statefulset/nereidsphere-valkey-sentinel
curl -fsS https://nereidsphere.example.com/readyz
# {"auth":"oidc","replay":"valkey","status":"ok"} once Valkey and issuer are reachable
curl -fsS https://nereidsphere.example.com/api/v1/auth/method
# {"mode":"oidc"}
```

Then open `https://nereidsphere.example.com` in a browser: it redirects to
the NI login page (NS never sees the upstream password), returns to
`/auth/callback`, and the portal loads. The account console lives under
`/admin/users`; API documentation is embedded at `/api/docs`.

## Troubleshooting

- **`Invalid parameter: redirect_uri` on the NI login page** —
  `<baseUrl>/auth/callback` is missing from the realm client redirect
  allowlist. Register the exact URI through the identity provider's
  administrative interface.
- **`/readyz` reports issuer unreachable / x509 unknown authority** — set
  `config.oidc.caBundleSecret` to a copied internal-CA Secret.
- **Admin console returns `IDENTITY_FORBIDDEN`** — the
  `nereidsphere-admin` service account lacks the `realm-management/realm-admin`
  role mapping; assign it through the provider's administrative interface.
- **Helm install fails with a `fail` message** — read the rendered
  configuration requirement named in the message; the chart refuses
  incomplete `oidc`/`local`/product configuration on purpose.
- **Login works with curl but the browser returns to the login page** —
  sealed session cookies must stay under the browser 4 KB limit; the image
  compresses session payloads. Do not add additional large claims to the
  cookie session.
