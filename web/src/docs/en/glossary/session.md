<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Session

A session is the server-issued record that you are authenticated. In
NereidSphere it is a single cookie named `ns_session`:

- **Encrypted** with AES-256-GCM using a server-side 32-byte key.
- **HttpOnly** — browser JavaScript cannot read it, so no token is exposed to
  the page.
- **Stateless** — there is no session database; any NereidSphere instance can
  decrypt the cookie with the shared secret.

The cookie carries your identity (subject, email, name, roles). Clearing the
cookie or choosing **Sign out** ends the session.
