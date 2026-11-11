<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# 连接状态

**连接状态**页面（管理导航 → 连接）展示 NereidSphere 与身份平面的连接方式，
对套件 `operator` 与 `admin` 角色可见。

## 字段说明

- **认证模式**
  - `NereidIdentity（OIDC + PKCE）`——浏览器通过授权码 + PKCE 跳转
    NereidIdentity 登录；
  - `本地应急账号`——使用部署密钥中的 bcrypt 应急账号，不联系任何身份服务。
- **身份发行方**——OIDC issuer URL（发现文档位于
  `<issuer>/.well-known/openid-configuration`）。
- **发行方可达**——发现端点当前是否可访问。发行方不可达不会阻止 NS 启动；
  就绪探针（`/readyz`）与本页面会实时反映状态。
- **Admin REST API**——是否已配置 `nereidsphere-admin` 服务账号以支持用户
  管理。

## 产品集成

每个产品使用以下两种模式之一：

| 模式 | 含义 |
| --- | --- |
| NS 票据枢纽（`ns`） | 打开产品时走 `/auth/enter/{id}`，使用 30 秒有效、一次性、绑定受众的密封票据。 |
| 独立 OIDC（`oidc`） | 产品自身是 NereidIdentity 的 OIDC 客户端；NS 直接跳转，既有 SSO 会话使其免登进入。 |

身份服务正常但页面显示不可达时，请检查网络策略/出网访问与配置的 issuer
URL。参见[常见问题](help:page/troubleshooting)。
