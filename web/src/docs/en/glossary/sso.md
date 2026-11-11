<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Single Sign-On (SSO)

Single sign-on means a user authenticates **once** at NereidSphere and can
then open every integrated product without signing in again.

In NereidSphere the browser is redirected from `/auth/login` to the
[NereidIdentity](help:glossary/identity-provider) login page and back through
`/auth/callback` using the OAuth2 authorization-code flow with PKCE.
NereidSphere verifies the returned tokens and keeps the identity in a sealed
[session](help:glossary/session) cookie; the upstream password and tokens
never appear in the browser. Emergency `local` mode instead uses the
break-glass username/password form backed by built-in bcrypt accounts.

When entering a product, NereidSphere either hands over a one-time sealed
[NS ticket](help:glossary/ns-ticket) (mode `ns`) or sends the browser to the
product's own OIDC login (mode `oidc`).
