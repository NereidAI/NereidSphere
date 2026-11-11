<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Troubleshooting

Common sign-in and access problems in NereidSphere.

## "invalid username or password"

- This only applies to emergency **local** mode (`auth.mode=local`).
  Confirm the username and password; the error intentionally does not say
  which field was wrong.
- In development use the seeded accounts (`admin/admin`, `user/user`).
- Make sure Caps Lock is off.
- In oidc mode credentials are entered on the NereidIdentity page; account
  problems there are handled by the identity service/administrator.

## SSO sign-in failed or was rejected

- **Sign-in cancelled or rejected** — the identity service denied the
  login; start again and use an account assigned in the suite realm.
- **Single sign-on failed** — the short handshake may have expired or the
  state did not match; start the login again from the portal.
- **Identity service unreachable** — NereidSphere itself stays up, but the
  identity plane may be restarting. Retry later or ask an administrator for
  emergency local access.

## The page keeps returning to login

- Cookies must be allowed for the NereidSphere origin; the session lives in
  the `ns_session` HttpOnly cookie.
- Do not use incognito/private mode if it blocks cookies.
- An expired session simply requires signing in again.

## A product does not open (bad gateway)

- The product service may be stopped or unreachable from the gateway. Check
  the product itself and the NereidSphere server logs.
- Requests to products flow through the unified
  [Gateway](help:glossary/gateway).

## Redirect rejected

Post-login URLs must match a configured product origin exactly. A modified or
foreign URL is refused to prevent open redirects. Open the product from the
portal grid instead.

## Language did not persist

The language choice is stored in browser localStorage under the key
`i18next`. Clear site data or disabled storage resets it to browser-language
detection.

## Still stuck

Contact your suite administrator. See [Signing in](help:page/sso-login).
