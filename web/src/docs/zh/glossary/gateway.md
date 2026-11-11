<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 网关

NereidSphere 统一 API 网关是访问集成产品的唯一反向代理入口，路径为 `/p/{product}/...`。

它负责：

- 校验 NereidSphere [会话](help:glossary/session)；
- 向下游产品转发可信身份头（`X-User-Id`、`X-User-Email`、`X-User-Name`、`X-User-Roles`）；
- 保留链路追踪头；
- 绝不向浏览器泄露密钥或 bearer 令牌。

业务逻辑仍归属各产品，网关只负责认证与路由。当产品不可达时返回 `502 Bad Gateway`。
