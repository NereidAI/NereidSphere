<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Session and Logout

Your suite session is stored in a single encrypted, HttpOnly cookie
(`ns_session`). It contains your identity and the upstream token data; no
token is ever exposed to browser JavaScript.

## Duration

The session follows the configured lifetime (8 hours in development). When it
expires you are asked to sign in again.

## Logout

1. Open the user menu in the portal header (top-right avatar).
2. Choose **Sign out**.
3. NereidSphere revokes the upstream refresh token best-effort, clears the
   session/CSRF cookies, and (in oidc mode) continues to the identity
   service logout page, which returns you to the portal afterwards.

Logging out of NereidSphere ends the suite session. Products that established
their own local sessions may require their own logout.

## Cookie requirements

Cookies must be enabled for the NereidSphere origin. If you appear signed out
on every request, see [Troubleshooting](help:page/troubleshooting).

See also [Session](help:glossary/session).
