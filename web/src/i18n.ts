// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * i18next bootstrap.
 *
 * Two supported languages (English, Chinese). Detection order is
 * localStorage → browser → <html lang>, the choice is cached in localStorage,
 * and English is the fallback. Adding a language requires a locale pack, an
 * entry in `resources` and an antd locale mapping in main.tsx.
 */

import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

import en from './locales/en'
import zh from './locales/zh'

const resources = {
  en: { translation: en },
  zh: { translation: zh },
} as const

const supportedLngs = Object.keys(resources)

// normalizeLng migrates cached codes such as zh-CN/en-US to the two-letter
// identifiers used by the locale packs.
function normalizeLng(lng: string): string {
  const normalized = lng.toLowerCase()
  if (normalized === 'zh-cn' || normalized === 'zh') return 'zh'
  if (normalized === 'en-us' || normalized === 'en') return 'en'
  return normalized
}

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'en',
    supportedLngs,
    nonExplicitSupportedLngs: true,
    detection: {
      order: ['localStorage', 'navigator', 'htmlTag'],
      caches: ['localStorage'],
      convertDetectedLanguage: normalizeLng,
    },
    interpolation: { escapeValue: false },
    react: { useSuspense: false },
  })

export default i18n
