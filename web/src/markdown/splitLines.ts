/**
 * Split highlight.js HTML into one HTML string per source line. Spans open at
 * a newline are closed at the end of that line and reopened on the next, so
 * every line is balanced and can be wrapped in its own element.
 * highlight.js escapes text, so the only "<" in the input start tags.
 */
export function splitHighlightedLines(html: string): string[] {
  const lines: string[] = []
  const open: string[] = []
  let current = ''
  const token = /(<span[^>]*>)|(<\/span>)|(\n)|([^<\n]+)/g
  for (const m of html.matchAll(token)) {
    if (m[1] !== undefined) {
      current += m[1]
      open.push(m[1])
    } else if (m[2] !== undefined) {
      current += m[2]
      open.pop()
    } else if (m[3] !== undefined) {
      lines.push(current + '</span>'.repeat(open.length))
      current = open.join('')
    } else if (m[4] !== undefined) {
      current += m[4]
    }
  }
  lines.push(current)
  return lines
}
