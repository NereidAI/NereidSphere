<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 登录

NereidSphere 是套件的统一登录入口。登录方式取决于部署配置的认证模式。

## OIDC 模式（默认）

1. 打开 NereidSphere 门户；未登录访问会进入登录页，页面显示
   **使用 NereidIdentity 登录**。
2. 点击按钮，浏览器通过标准 OAuth2 **授权码 + PKCE** 流程跳转到
   NereidIdentity 登录页。
3. 在该页面输入组织账号密码。NereidSphere 不会接触或保存该密码，浏览器也
   永远拿不到上游令牌。
4. 浏览器回到 NereidSphere 的 `/auth/callback`；后端建立加密的套件会话
   Cookie，然后进入门户。

外部身份提供商（Entra ID、Okta、Auth0、Authing 等）统一通过
NereidIdentity 代理（broker）；新增外部 IdP 时 NereidSphere 无需任何改动。
参见[身份提供方](help:glossary/identity-provider)。

## 本地应急模式

当部署使用 `auth.mode=local` 时，登录页显示标注为**应急登录**的用户名/
密码表单。这些账号是部署密钥中声明的 bcrypt 哈希条目，仅用于 break-glass
应急访问；常规用户始终通过 NereidIdentity 登录。

## 错误提示

- **登录被取消或拒绝**——身份服务拒绝了登录；请重试或使用套件领域中已授权
  的账号。
- **身份服务不可达**——NereidIdentity 可能正在启动或已停机；紧急情况请联系
  管理员获取本地应急账号。
- **单点登录失败**——握手过期或 state 不匹配；请重新发起登录。
- 本地模式的**用户名或密码错误**——请重新核对；接口不会指出究竟是哪一项
  错误。

若反复登录失败，参见[常见问题](help:page/troubleshooting)。

另见[产品单点登录](help:page/product-sso)与
[会话与登出](help:page/session-and-logout)。
