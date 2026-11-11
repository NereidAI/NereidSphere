// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// check-help-docs verifies the help system is internally consistent:
//
//   - every openHelp("<id>") call site references a catalog document;
//   - every catalog entry has both an English and a Chinese Markdown file;
//   - every Markdown page on disk is registered in the catalog;
//   - every glossary term exists in both languages;
//   - every explicit help: Markdown link resolves to a catalog page or
//     glossary term.
//
// It guards against silent failures: getDoc falls back to an empty string for
// an unknown id, so a dangling reference would render a blank drawer with no
// error anywhere. Run it with `npm run check:help`.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const problems = []

// catalogIds reads the ids registered in the help registry.
function catalogIds() {
  const src = readFileSync(join(root, 'src/help/docs.ts'), 'utf8')
  return [...src.matchAll(/\{ id: ['"]([^'"]+)['"]/g)].map((match) => match[1])
}

// termIds lists the glossary Markdown basenames in one language.
function termIds(language) {
  const dir = join(root, 'src/docs', language, 'glossary')
  return new Set(
    readdirSync(dir).filter((n) => n.endsWith('.md')).map((n) => n.replace(/\.md$/, '')),
  )
}

// helpLinks extracts help-link targets declared in a Markdown file.
function helpLinks(file) {
  const src = readFileSync(file, 'utf8')
  return [...src.matchAll(/\(help:(?:(page|glossary)\/)?([a-z0-9-]+)\)/g)].map(
    (m) => ({ kind: m[1] ?? 'auto', id: m[2] }),
  )
}

// listMarkdown yields every page .md path in one language directory.
function listMarkdown(language) {
  return readdirSync(join(root, 'src/docs', language))
    .filter((n) => n.endsWith('.md'))
    .map((n) => join(root, 'src/docs', language, n))
}

// docIds lists the Markdown basenames in one language directory.
function docIds(language) {
  return new Set(
    readdirSync(join(root, 'src/docs', language))
      .filter((name) => name.endsWith('.md'))
      .map((name) => name.replace(/\.md$/, '')),
  )
}

// walkTsx yields .ts/.tsx files under dir recursively.
function walkTsx(dir) {
  const out = []
  for (const entry of readdirSync(dir)) {
    if (entry === 'node_modules' || entry === 'dist') continue
    const full = join(dir, entry)
    const st = statSync(full)
    if (st.isDirectory()) out.push(...walkTsx(full))
    else if (/\.(ts|tsx)$/.test(entry)) out.push(full)
  }
  return out
}

const catalog = catalogIds()
const en = docIds('en')
const zh = docIds('zh')
const enTerms = termIds('en')
const zhTerms = termIds('zh')

// Glossary terms must exist in both languages.
for (const id of enTerms) {
  if (!zhTerms.has(id)) {
    problems.push(`glossary term "${id}" missing Chinese page (src/docs/zh/glossary/${id}.md)`)
  }
}
for (const id of zhTerms) {
  if (!enTerms.has(id)) {
    problems.push(`glossary term "${id}" missing English page (src/docs/en/glossary/${id}.md)`)
  }
}

// Every help: link must resolve to a catalog page or glossary term.
const pageIDs = new Set(catalog)
const allMarkdown = [
  ...listMarkdown('en'),
  ...listMarkdown('zh'),
  ...[...enTerms].map((id) => join(root, 'src/docs/en/glossary', `${id}.md`)),
  ...[...zhTerms].map((id) => join(root, 'src/docs/zh/glossary', `${id}.md`)),
]
for (const file of allMarkdown) {
  for (const { kind, id } of helpLinks(file)) {
    const rel = file.replace(root + '/', '')
    if (kind === 'page' && !pageIDs.has(id)) {
      problems.push(`${rel}: help:page/${id} links to an unknown help page`)
    } else if (kind === 'glossary' && !enTerms.has(id)) {
      problems.push(`${rel}: help:glossary/${id} links to an unknown term`)
    } else if (kind === 'auto' && !pageIDs.has(id) && !enTerms.has(id)) {
      problems.push(`${rel}: help:${id} links to an unknown document`)
    }
  }
}

// Every openHelp("...") call site must resolve, otherwise the drawer is empty.
for (const file of walkTsx(join(root, 'src'))) {
  const source = readFileSync(file, 'utf8')
  for (const match of source.matchAll(/openHelp\(\s*['"]([^'"]+)['"]/g)) {
    if (!catalog.includes(match[1])) {
      problems.push(`${file.replace(root + '/', '')}: openHelp("${match[1]}") is not in the help catalog`)
    }
  }
}

// Every catalog entry needs both translations; nothing may sit unregistered.
for (const id of catalog) {
  if (!en.has(id)) problems.push(`catalog "${id}" has no src/docs/en/${id}.md`)
  if (!zh.has(id)) problems.push(`catalog "${id}" has no src/docs/zh/${id}.md`)
}
for (const id of en) {
  if (!catalog.includes(id)) problems.push(`src/docs/en/${id}.md is not registered`)
}
for (const id of zh) {
  if (!catalog.includes(id)) problems.push(`src/docs/zh/${id}.md is not registered`)
}

if (problems.length > 0) {
  for (const problem of problems) console.error(`help docs: ${problem}`)
  process.exit(1)
}
console.log(
  `help docs: ${catalog.length} documents, ${enTerms.size} glossary terms, all consistent`,
)
