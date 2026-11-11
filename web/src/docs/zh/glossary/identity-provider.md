<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 身份提供方

身份提供方（IdP）是持有用户、角色与 OAuth2 客户端的 OpenID Connect（OIDC）
服务。本套件中该服务即 **NereidIdentity（NI）**。NereidSphere 是纯粹的
OIDC/OAuth2 依赖方：只知晓标准连接参数（issuer、client id/secret、管理
服务账号客户端、scopes），绝不引导或管理身份平面。

- 浏览器通过授权码 + PKCE 跳转到 NI 品牌登录页；NereidSphere 不接触上游
  密码，也不使用 ROPC 密码模式。
- 客户 IdP（Entra ID、Okta、Auth0 等）统一在 NI 之后做代理（broker）；
  新增客户 IdP 时 NereidSphere 无需改动。
- 可通过仅修改配置替换为兼容 Keycloak 的实现。
- `auth.mode=local` 提供不依赖身份服务的应急 bcrypt 账号；内置管理控制台
  通过服务账号客户端调用 NI Admin REST，而非使用管理员密码。

身份服务由套件运维方独立部署与维护；登录流程参见
[登录](help:page/sso-login)。
