<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 会话

会话是服务端签发的、代表“你已通过认证”的记录。在 NereidSphere 中，它就是一个名为 `ns_session` 的 Cookie：

- 使用服务端 32 字节密钥以 **AES-256-GCM 加密**。
- **HttpOnly**——浏览器 JavaScript 无法读取，令牌不会暴露给页面。
- **无状态**——没有会话数据库；任何 NereidSphere 实例只要持有相同密钥都能解密该 Cookie。

Cookie 中携带你的身份（sub、邮箱、姓名、角色）。清除 Cookie 或选择**退出登录**即结束会话。
