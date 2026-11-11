<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 通过单点登录打开产品

登录 NereidSphere 后，可以**无需再次登录**打开 NereidForge、NereidHorizon
或 NereidLink。门户顺序遵循闭环链路：Forge（模型工厂）→ Horizon（分发到
边缘站点）→ Link（设备平台）。NereidEdge 在每个边缘站点独立部署，不进入
中心门户入口。

## 从门户进入

1. 登录 NereidSphere（oidc 模式下为 NereidIdentity 登录页，本地模式下为
   应急表单）。
2. 点击某个产品卡片。它会在新窗口打开 `/auth/enter/{product}`，原窗口继续保留门户。
3. **票据模式（`ns`）**的产品会收到一张 30 秒有效的一次性密封
   [NS 票据](help:glossary/ns-ticket)，由其后端在服务端换取已验证身份并建立
   自己的会话；票据只能使用一次。
4. **OIDC 模式（`oidc`）**的产品是独立的 NereidIdentity 客户端；
   NereidSphere 直接跳转，既有 SSO 会话使其免登进入。

## 从产品跳回

如果某个产品尚未识别你的身份，可能会把你引导到：

```
/auth/login?redirect=<产品地址>
```

若 NereidSphere 会话有效，你会被立刻带回该产品；否则需要先在身份服务完成
登录。

## 允许的跳转目标

登录后跳转只接受已配置的精确产品来源（例如
`https://nereidlink.example.com`），产品内深链只能是相对路径。未知或被篡改
的地址会被拒绝，以防止开放重定向漏洞。

另见[单点登录（SSO）](help:glossary/sso)、
[NS 票据](help:glossary/ns-ticket)与[网关](help:glossary/gateway)。
