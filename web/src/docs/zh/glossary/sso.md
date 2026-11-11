<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 单点登录（SSO）

单点登录指用户在 NereidSphere **登录一次**，之后打开所有已集成产品都无需再次登录。

在 NereidSphere 中，浏览器从 `/auth/login` 跳转到
[NereidIdentity](help:glossary/identity-provider) 登录页，再通过
`/auth/callback` 以 OAuth2 授权码 + PKCE 完成登录。NereidSphere 校验返回的
令牌，并把身份保存在密封的[会话](help:glossary/session) Cookie 中；浏览器
永远接触不到上游密码与令牌。本地应急（local）模式则使用基于内置 bcrypt
账号的 break-glass 用户名/密码表单。

进入产品时，NereidSphere 要么签发一张一次性密封
[NS 票据](help:glossary/ns-ticket)（`ns` 模式），要么直接跳转至产品自身的
OIDC 登录（`oidc` 模式）。
