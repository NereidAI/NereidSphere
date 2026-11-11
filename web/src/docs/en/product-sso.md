<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Opening Products with SSO

After signing in to NereidSphere, you can jump to NereidForge, NereidHorizon
or NereidLink **without signing in again**. The portal order follows the
closed-loop pipeline: Forge (model factory) → Horizon (distribution to edge
sites) → Link (device platform). NereidEdge is deployed independently at
every edge site and is not a central portal entry.

## From the portal

1. Sign in to NereidSphere (the NereidIdentity login page in oidc mode, or
   the emergency local form).
2. Click a product card. It opens `/auth/enter/{product}` in a new window,
   so the portal remains available in the original window.
3. Products in **ticket mode (`ns`)** receive a one-time, 30-second sealed
   [NS ticket](help:glossary/ns-ticket) and exchange it server to server for
   your verified identity, then open their own session — the ticket works only
   once.
4. Products in **OIDC mode (`oidc`)** are standalone NereidIdentity clients;
   NereidSphere redirects you straight to the product and the existing SSO
   session makes entry seamless.

## Arriving from a product

If a product does not recognize you, it may send you to

```
/auth/login?redirect=<product-url>
```

With a valid NereidSphere session you are returned to the product
immediately; otherwise you first complete the identity-service login.

## Allowed destinations

Only exact, configured product origins (for example
`https://nereidlink.example.com`) are accepted as post-login redirects, and
in-product deep links are relative paths only. Unknown or tampered URLs are
rejected — this prevents open redirects.

See also [Single sign-on (SSO)](help:glossary/sso), the
[NS ticket](help:glossary/ns-ticket) and the
[Gateway](help:glossary/gateway).
