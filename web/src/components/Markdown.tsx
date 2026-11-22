import { useMemo } from 'react'
import { renderInlineMarkdown, renderMarkdown } from '../markdown/render'
import '../styles/card.css'

/**
 * Renders card Markdown. This is the only component allowed to use
 * dangerouslySetInnerHTML: the HTML comes from markdown-it (html: false)
 * and is sanitised by DOMPurify.
 *
 * `inline` renders a single line's inline Markdown into a <span>, for one-line
 * previews.
 */
export function Markdown({ source, className, inline = false }: { source: string; className?: string; inline?: boolean }) {
  const html = useMemo(() => (inline ? renderInlineMarkdown(source) : renderMarkdown(source)), [source, inline])
  const cls = ['md', inline && 'md-inline', className].filter(Boolean).join(' ')
  return inline ? (
    <span className={cls} dangerouslySetInnerHTML={{ __html: html }} />
  ) : (
    <div className={cls} dangerouslySetInnerHTML={{ __html: html }} />
  )
}
