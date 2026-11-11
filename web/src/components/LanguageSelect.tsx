// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Compact language selector shared by the login card and the portal header.
 * The chosen language is cached by i18next's localStorage detector and the
 * reactive antd ConfigProvider (see main.tsx) swaps antd's own texts at once.
 */

import { Select } from 'antd'
import { useTranslation } from 'react-i18next'

const languageOptions = [
  { value: 'en', label: 'English' },
  { value: 'zh', label: '中文' },
]

interface LanguageSelectProps {
  /** width in pixels or any CSS width; defaults to the header size. */
  width?: number | string
  /** small renders the compact header variant. */
  small?: boolean
}

/** LanguageSelect switches between the supported locale packs. */
export default function LanguageSelect({ width = 100, small = true }: LanguageSelectProps) {
  const { i18n } = useTranslation()
  const current = i18n.language?.startsWith('zh') ? 'zh' : 'en'

  return (
    <Select
      size={small ? 'small' : 'middle'}
      value={current}
      onChange={(value) => void i18n.changeLanguage(value)}
      options={languageOptions}
      style={{ width }}
    />
  )
}
