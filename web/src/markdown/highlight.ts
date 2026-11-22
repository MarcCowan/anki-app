import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import c from 'highlight.js/lib/languages/c'
import cpp from 'highlight.js/lib/languages/cpp'
import json from 'highlight.js/lib/languages/json'
import python from 'highlight.js/lib/languages/python'

const languages = { cpp, c, python, bash, json }
for (const [name, lang] of Object.entries(languages)) {
  hljs.registerLanguage(name, lang)
}

/** Resolve a fence tag (any case, any alias) to a registered language name. */
function canonical(lang: string | undefined): string | null {
  const wanted = hljs.getLanguage(lang?.trim().toLowerCase() ?? '')
  if (!wanted) return null
  return Object.keys(languages).find((name) => hljs.getLanguage(name) === wanted) ?? null
}

export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#x27;')
}

/**
 * Highlight code for a fence tag. Unknown or missing languages give plain
 * escaped text and language null; this never throws.
 */
export function highlightCode(code: string, lang: string | undefined): { html: string; language: string | null } {
  const language = canonical(lang)
  if (!language) return { html: escapeHtml(code), language: null }
  try {
    return { html: hljs.highlight(code, { language, ignoreIllegals: true }).value, language }
  } catch {
    return { html: escapeHtml(code), language: null }
  }
}
