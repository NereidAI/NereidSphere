// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Application entry point. Configures i18n, the reactive antd locale and
 * theme, and mounts the router inside the antd App context so message and
 * modal helpers share one theme. The antd locale follows the selected i18n
 * language instantly, without a page reload.
 */

import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ConfigProvider, App as AntdApp, theme } from 'antd'
import enUS from 'antd/locale/en_US'
import zhCN from 'antd/locale/zh_CN'
import { useTranslation } from 'react-i18next'
import './i18n'
import App from './App'
import './index.css'

/** ThemedApp binds the antd locale and theme token to the i18n language. */
function ThemedApp() {
  const { i18n } = useTranslation()
  const locale = i18n.language?.startsWith('zh') ? zhCN : enUS
  return (
    <ConfigProvider
      locale={locale}
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: { colorPrimary: '#1677ff', borderRadius: 6 },
      }}
    >
      <AntdApp>
        <App />
      </AntdApp>
    </ConfigProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ThemedApp />
    </BrowserRouter>
  </React.StrictMode>,
)
