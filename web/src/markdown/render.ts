import DOMPurify from 'dompurify'
import MarkdownIt from 'markdown-it'
import { highlightCode } from './highlight'
import { splitHighlightedLines } from './splitLines'

const SAFE_LINK = /^(https?:|mailto:)/i

function renderFence(code: string, info: string): string {
  const lang = info.trim().split(/\s+/)[0]
  // markdown-it passes the fence body with its final newline.
  const body = code.endsWith('\n') ? code.slice(0, -1) : code
  const { html, language } = highlightCode(body, lang)
  const lines = splitHighlightedLines(html)
    .map((l) => '<span class="line">' + l + '</span>')
    .join('')
  const cls = language ? `hljs language-${language}` : 'hljs'
  return `<pre class="code-block"><code class="${cls}">${lines}</code></pre>`
}

const md = new MarkdownIt({
  html: false,
  linkify: false,
  typographer: false,
  // Returning a string that starts with <pre makes markdown-it use it as-is.
  highlight: renderFence,
})
md.validateLink = (url) => SAFE_LINK.test(url.trim())
// Media is out of scope, and remote images would break the CSP.
md.disable('image')

function sanitize(html: string): string {
  return DOMPurify.sanitize(html, {
    ALLOWED_ATTR: ['class', 'href', 'title'],
    FORBID_TAGS: ['style'],
  })
}

/** Render card Markdown to sanitised HTML. The only HTML renderer in the app. */
export function renderMarkdown(source: string): string {
  return sanitize(md.render(source))
}

/**
 * Render one line of card Markdown as inline HTML only (code spans, emphasis,
 * links): block syntax such as headings, lists and fences stays literal text.
 * Used for one-line card previews.
 */
export function renderInlineMarkdown(source: string): string {
  return sanitize(md.renderInline(source))
}
