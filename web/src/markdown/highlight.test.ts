import { describe, expect, it } from 'vitest'
import { highlightCode } from './highlight'

describe('highlightCode', () => {
  it('highlights cpp', () => {
    const r = highlightCode('int x;', 'cpp')
    expect(r.language).toBe('cpp')
    expect(r.html).toMatch(/hljs-(type|keyword)/)
  })

  it('aliases and unknown languages', () => {
    expect(highlightCode('int x;', 'c++').language).toBe('cpp')
    expect(highlightCode('int x;', 'C++').language).toBe('cpp')
    expect(highlightCode('x = 1', 'py').language).toBe('python')
    expect(highlightCode('ls', 'sh').language).toBe('bash')
    for (const lang of ['madeup', undefined, 'text', '']) {
      const r = highlightCode('a<b & "c"', lang)
      expect(r.language).toBeNull()
      expect(r.html).toBe('a&lt;b &amp; &quot;c&quot;')
    }
  })

  it('escapes html inside highlighted code', () => {
    expect(highlightCode('std::vector<int> v;', 'cpp').html).not.toContain('<int>')
  })
})
