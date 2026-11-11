<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 会话与登出

套件会话保存在单个加密的 HttpOnly Cookie（`ns_session`）中，内容包含你的身份以及上游令牌数据；浏览器 JavaScript 永远接触不到任何令牌。

## 有效期

会话遵循配置的生命周期（开发环境为 8 小时）。过期后需要重新登录。

## 登出

1. 打开门户头部右上角头像中的用户菜单。
2. 选择**退出登录**。
3. NereidSphere 会尽力撤销上游刷新令牌、清除会话与 CSRF Cookie；oidc 模式下
   随后跳转身份服务登出页，完成后回到门户。

退出 NereidSphere 会结束套件会话；已经建立本地会话的产品可能仍需在产品侧另行退出。

## Cookie 要求

必须允许 NereidSphere 来源使用 Cookie。如果每次请求都显示未登录，参见[常见问题](help:page/troubleshooting)。

另见[会话](help:glossary/session)。
