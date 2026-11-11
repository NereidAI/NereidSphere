// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Loads the standalone glossary documents under
 * src/docs/{en,zh}/glossary/. Glossary pages are not registered in
 * helpEntries: they form their own "Glossary" group and are reached by
 * explicit cross-reference links.
 *
 * Authoring rules:
 *   - There is NO automatic term linking; place links by hand only where a
 *     document actually introduces a concept.
 *   - Write `[label](help:glossary/<id>)` for a term and
 *     `[label](help:page/<id>)` for a catalog help page. The prefix is
 *     required so a term and a guide sharing a basename never collide.
 *   - Both languages must ship the same term file basenames.
 *
 * `npm run check:help` validates bilingual parity and every link target.
 */

/** GlossaryTerm is one loaded glossary document. */
export interface GlossaryTerm {
  // id is the glossary file basename and the help: link target.
  id: string
  // order sorts terms in the Glossary group.
  order: number
  // title is the localized H1 heading.
  title: string
}

// rawGlossary maps "../docs/<lang>/glossary/<id>.md" to raw Markdown.
const rawGlossary = import.meta.glob<string>('../docs/{en,zh}/glossary/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
})

// pickLang normalizes the active language to en/zh.
function pickLang(language: string): string {
  return language.startsWith('zh') ? 'zh' : 'en'
}

// getGlossaryDoc returns one glossary document, falling back to English.
export function getGlossaryDoc(id: string, language: string): string {
  const lang = pickLang(language)
  return (
    rawGlossary[`../docs/${lang}/glossary/${id}.md`] ??
    rawGlossary[`../docs/en/glossary/${id}.md`] ??
    ''
  )
}

// extractTitle returns the first H1 line of a Markdown source.
function extractTitle(source: string, fallback: string): string {
  return source.match(/^#\s+(.+)$/m)?.[1]?.trim() ?? fallback
}

// glossaryTerms lists glossary docs for the language sorted by id.
export function glossaryTerms(language: string): GlossaryTerm[] {
  const lang = pickLang(language)
  const ids = new Set(
    Object.keys(rawGlossary)
      .filter((key) => key.includes(`/${lang}/glossary/`))
      .map((key) => key.split('/').pop()!.replace(/\.md$/, '')),
  )
  return [...ids]
    .sort()
    .map((id, index) => ({
      id,
      order: (index + 1) * 10,
      title: extractTitle(getGlossaryDoc(id, lang), id),
    }))
}

// HelpTarget is a resolved `help:` link destination.
export type HelpTarget =
  | { kind: 'page'; id: string }
  | { kind: 'glossary'; id: string }

// parseHelpTarget resolves a link path inside the help: scheme.
// Explicit prefixes "page/<id>" and "glossary/<id>" win; a bare id is
// auto-detected (glossary first, then catalog page).
export function parseHelpTarget(target: string, language: string): HelpTarget {
  if (target.startsWith('glossary/')) {
    return { kind: 'glossary', id: target.slice('glossary/'.length) }
  }
  if (target.startsWith('page/')) {
    return { kind: 'page', id: target.slice('page/'.length) }
  }
  const isTerm = glossaryTerms(language).some((term) => term.id === target)
  return isTerm
    ? { kind: 'glossary', id: target }
    : { kind: 'page', id: target }
}
