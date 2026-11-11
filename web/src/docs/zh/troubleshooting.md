<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 常见问题

NereidSphere 登录与访问中的常见问题。

## 提示“invalid username or password”

- 该提示仅出现在**本地应急模式**（`auth.mode=local`）。请确认用户名和
  密码；错误有意不说明究竟是哪一项填错。
- 开发环境使用预置账号（`admin/admin`、`user/user`）。
- 确认大写锁定（Caps Lock）已关闭。
- oidc 模式下凭据在 NereidIdentity 页面输入，账号问题由身份服务/管理员
  处理。

## SSO 登录失败或被拒绝

- **登录被取消或拒绝**——身份服务拒绝了登录；请重新发起并使用套件领域中
  已授权的账号。
- **单点登录失败**——短时握手可能已过期或 state 不匹配；请从门户重新
  登录。
- **身份服务不可达**——NereidSphere 本身仍在运行，但身份平面可能正在
  重启。请稍后重试，或联系管理员获取本地应急访问。

## 页面反复回到登录页

- 必须允许 NereidSphere 来源使用 Cookie；会话保存在 HttpOnly 的 `ns_session` Cookie 中。
- 若隐私/无痕模式阻止了 Cookie，请改用普通窗口。
- 会话过期后重新登录即可。

## 产品打不开（bad gateway）

- 产品服务可能已停止，或网关无法访问到它。请检查产品本身以及 NereidSphere 服务端日志。
- 访问产品的请求都会经过统一[网关](help:glossary/gateway)。

## 跳转被拒绝

登录后跳转地址必须与已配置的产品来源精确匹配。被修改或外部的地址会被拒绝，以防止开放重定向。请改为从门户产品卡片进入。

## 语言选择没有保持

语言选择保存在浏览器 localStorage 的 `i18next` 键下。清除站点数据或禁用存储后，会恢复为按浏览器语言检测。

## 仍然无法解决

请联系套件管理员。另见[登录](help:page/sso-login)。
