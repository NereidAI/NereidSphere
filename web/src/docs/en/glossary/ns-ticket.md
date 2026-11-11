<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# NS Ticket

An NS ticket is a **short-lived, single-use sealed credential** used to hand a
verified identity securely from NereidSphere to a product backend when the
product is integrated in **`ns` mode**. It appears only during the single
redirect that takes the user from the portal into the product, so the product
can learn "who you are and which tenants/roles you hold" without ever seeing
the NereidSphere [session](help:glossary/session) cookie or talking to the
identity service directly.

## Why a ticket is needed

Products (NereidLink, NereidForge, NereidHorizon) are independently hosted on
their own domains, which gives two hard constraints:

- When the browser jumps to a product domain it carries only that domain's
  cookies — **it cannot read the NS session cookie**, and the product is not
  allowed to read or validate the NS session directly.
- The redirect is a "front-channel" path: it passes through browser history,
  proxy/ingress access logs and the Referer header, so **real identity data must
  not travel through it**.

The browser therefore carries only an opaque ticket string in the redirect;
the user's real identity data never travels through that browser redirect —
it is exchanged exclusively in a server-to-server request between the
**product backend and the NereidSphere backend**.

## The handoff

1. Already signed in to NereidSphere, the user clicks a product card, opening
   `GET /auth/enter/{product}`.
2. NS validates the session and product authorization (default-deny: a
   `/nereid{product}/...` assignment group or the suite `admin` role), mints
   the ticket and returns `302` to the product callback
   `https://product/auth/ns-callback?ticket=…`.
3. The **product backend** immediately calls
   `POST /api/v1/auth/ticket/verify` with its own server credentials
   (`X-NS-Client-Id` / `X-NS-Client-Secret`).
4. NS validates the ticket and returns the identity snapshot (`sub`, username,
   email, suite roles, product groups); the product then establishes **its own
   local session**. The ticket is consumed at that moment.

This is the same well-established pattern family as the CAS Service Ticket,
the OAuth2 authorization-code separation of front/back channels, and the SAML
Artifact Binding — not a bespoke cryptographic protocol.

## What the ticket contains

The ticket is an **AES-256-GCM authenticated-encryption** envelope sealed with
the 32-byte server key, containing:

- a `kind` purpose marker (isolated from session cookies and login-flow cookies
  so ciphertext cannot be replayed across purposes);
- a unique `jti`, the target product (`aud`), the identity snapshot, an
  in-product deep link (`to`), and issue/expiry timestamps.

The browser sees only opaque ciphertext. The ticket is **stateless**: the
envelope carries everything, any NS replica can verify it, and minting writes
nothing to a database.

## Security

- **Forgery/tampering:** AES-256-GCM provides confidentiality and integrity
  together — a single flipped bit fails decryption. The sealing key lives only
  on the NS server side; neither products nor browsers hold it, so nobody else
  can mint tickets.
- **Replay:** every ticket gets a random `jti`, consumed with an atomic claim
  (shared Valkey `SET NX PX` across replicas); a second exchange is rejected
  (`TICKET_NOT_FOUND`). When the replay store is unavailable NS **fails closed**
  (503 `TICKET_STORE_UNAVAILABLE`) — it would rather reject a login than risk
  honouring the same ticket twice.
- **Theft:** the lifetime is **30 seconds** and usage is one-time; the legit
  callback typically consumes it within a second. It travels over HTTPS, never
  through browser JavaScript and never into local storage. Even if a URL leaks
  into a log, the ticket is long expired and already consumed when found.
- **Product isolation (audience binding):** one ticket is minted for exactly
  one product (`aud`). A product can only exchange tickets issued to itself
  using its own secret; cross-product use returns `TICKET_AUDIENCE`, and **the
  ticket is burned even on an audience mismatch** to stop probing products one
  by one with the same ticket. Per-product secrets are independent and compared
  in constant time; compromising one product lets an attacker neither forge
  tickets nor touch other products' tickets.
- **Open redirect:** post-login targets and product callbacks accept only
  exact, configured origins; deep links inside a product must be relative paths.
- **Timely revocation:** roles/groups in the ticket are a snapshot at mint time;
  the 30-second lifetime makes the stale-authorization window negligible, and
  products reconcile memberships from the latest groups on every SSO login.

### Known limits / future hardening

- The sealing key is currently static; automated key rotation (`kid` with
  overlapping new/old keys) is not implemented yet.
- The server-to-server verify endpoint currently relies on HTTPS plus a
  per-product shared secret; mTLS on the internal path is a future
  defense-in-depth option.
- Tickets currently ride in the redirect URL (the standard approach for CAS and
  authorization codes); a stricter baseline could switch to an auto-submitted
  POST form moving the ticket into the request body without changing the
  protocol shape.

The OIDC authorization-code + PKCE flow happens between NereidSphere and
NereidIdentity and follows the generic standard; the NS ticket is the
suite-level mechanism on the product-integration side. See also
[Single sign-on (SSO)](help:glossary/sso) and the [Gateway](help:glossary/gateway).
