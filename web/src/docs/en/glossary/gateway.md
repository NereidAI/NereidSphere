<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Gateway

The NereidSphere unified API gateway is the single reverse-proxy entry for
requests to integrated products, exposed under `/p/{product}/...`.

It:

- verifies the NereidSphere [session](help:glossary/session);
- forwards trusted identity headers (`X-User-Id`, `X-User-Email`,
  `X-User-Name`, `X-User-Roles`) to the downstream product;
- preserves tracing headers;
- never passes secrets or bearer tokens to the browser.

Products keep their own business logic; the gateway only authenticates and
routes. An unreachable product results in a `502 Bad Gateway`.
