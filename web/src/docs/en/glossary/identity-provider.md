<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Identity Provider

An identity provider (IdP) is the OpenID Connect (OIDC) service that holds
users, roles and OAuth2 clients. In this suite that service is
**NereidIdentity** (NI). NereidSphere is a pure OIDC/OAuth2 relying party: it
only knows the standard connection settings (issuer, client id/secret,
admin service-account client, scopes) and never bootstraps or manages the
identity plane.

- The browser is redirected to the NI-branded login page over
  authorization-code + PKCE; NereidSphere never sees the upstream password
  and the ROPC password grant is not used.
- Customer IdPs (Entra ID, Okta, Auth0, …) are brokered **behind** NI;
  adding one requires no NereidSphere change.
- Keycloak-compatible engines can replace the implementation, using config
  only.
- `auth.mode=local` provides emergency bcrypt accounts without any identity
  service; the built-in admin console talks to NI Admin REST via a
  service-account client, not an admin password.

The identity service is deployed and maintained independently by the suite
operator; see [Signing in](help:page/sso-login) for the login flow.
