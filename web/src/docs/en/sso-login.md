<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Signing In

NereidSphere is the single sign-on entry for the suite. How you sign in
depends on the configured authentication mode.

## OIDC mode (default)

1. Open the NereidSphere portal; unauthenticated visitors land on the login
   page showing **Sign in with NereidIdentity**.
2. Click the button. Your browser is redirected to the NereidIdentity login
   page over the standard OAuth2 **authorization-code + PKCE** flow.
3. Enter your organization credentials there. NereidSphere never sees or
   stores this password, and the browser never receives upstream tokens.
4. You return to NereidSphere (`/auth/callback`), which establishes the
   encrypted suite session cookie and sends you to the portal.

External identity providers (Entra ID, Okta, Auth0, …), if configured, are
brokered behind NereidIdentity; nothing in NereidSphere changes when one is
added. See [Identity provider](help:glossary/identity-provider).

## Local emergency mode

When the deployment runs with `auth.mode=local`, the login page shows an
**Emergency access** username/password form instead. These accounts are
bcrypt-hashed entries from the deployment Secret and are intended only for
break-glass access; regular users always sign in through NereidIdentity.

## Errors

- **Sign-in cancelled or rejected** — the identity service denied the login;
  retry or use an account assigned in the suite realm.
- **Identity service unreachable** — NereidIdentity may be starting or down.
  In an emergency ask the administrator for local access.
- **Single sign-on failed** — the handshake expired or state did not match;
  start the login again.
- Local mode: **invalid username or password** — re-check both fields; the
  response does not reveal which was wrong.

If sign-in repeatedly fails, see [Troubleshooting](help:page/troubleshooting).

See also [Product single sign-on](help:page/product-sso) and
[Session and logout](help:page/session-and-logout).
