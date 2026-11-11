<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Connection Status

The **Connection** page (admin navigation → Connection) reports how
NereidSphere is wired to the identity plane. It is visible to suite
`operator` and `admin` roles.

## Fields

- **Authentication mode**
  - `NereidIdentity (OIDC + PKCE)` — browser login uses authorization-code
    with PKCE against NereidIdentity;
  - `Local emergency accounts` — break-glass bcrypt accounts from the
    deployment Secret; no identity service is contacted.
- **Identity issuer** — the OIDC issuer URL (discovery is read from
  `<issuer>/.well-known/openid-configuration`).
- **Issuer reachable** — whether the discovery endpoint answers right now.
  Startup never fails when the issuer is down; readiness (`/readyz`) and this
  page reflect the live state.
- **Admin REST API** — whether the `nereidsphere-admin` service-account
  client is configured for user management.

## Product integration

Each product has one of two modes:

| Mode | Meaning |
| --- | --- |
| NS ticket hub (`ns`) | Launching the product goes through `/auth/enter/{id}` with a 30-second, single-use, audience-bound sealed ticket. |
| Standalone OIDC (`oidc`) | The product is itself a NereidIdentity OIDC client; NS redirects straight to it and the existing SSO session makes entry seamless. |

If the issuer shows unreachable while the identity service is healthy, check
network policy/egress and the configured issuer URL. See
[Troubleshooting](help:page/troubleshooting).
