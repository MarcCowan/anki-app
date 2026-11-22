import { describe, expect, it } from 'vitest'
import { splitHighlightedLines } from './splitLines'

const balanced = (l: string) => (l.match(/<span/g) ?? []).length === (l.match(/<\/span>/g) ?? []).length

describe('splitHighlightedLines', () => {
  it('splits plain lines', () => {
    expect(splitHighlightedLines('a\nb')).toEqual(['a', 'b'])
  })

  it('closes and reopens spans across newlines', () => {
    expect(splitHighlightedLines('<span class="c">x\ny</span>z')).toEqual([
      '<span class="c">x</span>',
      '<span class="c">y</span>z',
    ])
  })

  it('handles nested spans and empty lines', () => {
    const lines = splitHighlightedLines('<span class="a"><span class="b">p\n\nq</span></span>')
    expect(lines).toHaveLength(3)
    expect(lines.every(balanced)).toBe(true)
    expect(lines[1]).toBe('<span class="a"><span class="b"></span></span>')
  })

  it('returns one empty line for empty input', () => {
    expect(splitHighlightedLines('')).toEqual([''])
  })
})
