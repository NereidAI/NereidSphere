// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Help documentation registry. All bilingual Markdown documents are eagerly
 * imported as raw strings, so the Drawer renders synchronously and the help
 * center can run client-side full-text search without additional requests.
 */

/** HelpGroup classifies documents in the help center tree. */
export type HelpGroup = 'start' | 'sso' | 'admin' | 'troubleshooting'

/** HelpEntry describes one help document. */
export interface HelpEntry {
  // id is both the document identifier and the Markdown file basename.
  id: string
  // group places the document in the help center tree.
  group: HelpGroup
  // order sorts documents inside a group.
  order: number
}

// helpEntries is the complete document catalog. Keep in sync with the
// Markdown files under src/docs/{en,zh}/; `npm run check:help` enforces it.
export const helpEntries: HelpEntry[] = [
  { id: 'portal-overview', group: 'start', order: 10 },
  { id: 'sso-login', group: 'sso', order: 10 },
  { id: 'product-sso', group: 'sso', order: 20 },
  { id: 'session-and-logout', group: 'sso', order: 30 },
  { id: 'admin-users', group: 'admin', order: 10 },
  { id: 'connection-status', group: 'admin', order: 20 },
  { id: 'troubleshooting', group: 'troubleshooting', order: 10 },
]

// rawDocs maps "<lang>/<id>" to the raw Markdown source.
const rawDocs = import.meta.glob<string>('../docs/{en,zh}/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
})

// getDoc returns the Markdown document for the id and language. When the
// localized document is missing, English is used as the fallback.
export function getDoc(id: string, language: string): string {
  const lang = language.startsWith('zh') ? 'zh' : 'en'
  const key = `../docs/${lang}/${id}.md`
  if (rawDocs[key]) return rawDocs[key]
  return rawDocs[`../docs/en/${id}.md`] ?? ''
}
