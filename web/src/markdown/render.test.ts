import { describe, expect, it } from 'vitest'
import { renderInlineMarkdown, renderMarkdown } from './render'

describe('renderMarkdown', () => {
  it('strips raw html', () => {
    const html = renderMarkdown('<script>alert(1)</script>')
    expect(html).not.toContain('<script')
  })

  it('neutralises event handlers', () => {
    expect(renderMarkdown('<img src=x onerror=alert(1)>')).not.toMatch(/<img/)
    expect(renderMarkdown('[x](http://a.example "t" onmouseover=alert(1))')).not.toMatch(/<[^>]*onmouseover/)
  })

  it('blocks javascript: links', () => {
    for (const src of ['[x](javascript:alert(1))', '[x](JAVASCRIPT:alert(1))', '[x](data:text/html,hi)', '<javascript:alert(1)>']) {
      expect(renderMarkdown(src)).not.toMatch(/href="(javascript|data)/i)
    }
    expect(renderMarkdown('[ok](https://example.com)')).toContain('href="https://example.com"')
    expect(renderMarkdown('[mail](mailto:a@b.c)')).toContain('href="mailto:a@b.c"')
  })

  it('renders fenced cpp as lines', () => {
    const html = renderMarkdown('```cpp\nint a;\nint b;\n```')
    expect(html).toContain('class="code-block"')
    expect(html.match(/class="line"/g)).toHaveLength(2)
    expect(html).toContain('language-cpp')
  })

  it('renders untagged and unknown fences as plain escaped lines', () => {
    for (const src of ['```\na<b\n```', '```madeup\na<b\n```', '```text\na<b\n```']) {
      const html = renderMarkdown(src)
      expect(html).toContain('class="code-block"')
      expect(html).toContain('<span class="line">a&lt;b</span>')
    }
  })

  it('renders inline code', () => {
    expect(renderMarkdown('The `std::vector<T>` type')).toContain('<code>std::vector&lt;T&gt;</code>')
  })

  it('preserves indentation in fences', () => {
    const html = renderMarkdown('```cpp\n    int& r = x;\n```')
    expect(html).toMatch(/<span class="line"> {4}/)
  })

  it('keeps blank lines inside fences', () => {
    const html = renderMarkdown('```\na\n\nb\n```')
    expect(html.match(/class="line"/g)).toHaveLength(3)
  })

  it('does not load images', () => {
    expect(renderMarkdown('![x](https://evil.example/p.png)')).not.toContain('<img')
  })
})

describe('renderInlineMarkdown', () => {
  it('renders inline code and emphasis without block wrappers', () => {
    const html = renderInlineMarkdown('What does `std::vector<T>` **do**?')
    expect(html).toBe('What does <code>std::vector&lt;T&gt;</code> <strong>do</strong>?')
  })

  it('never produces block elements', () => {
    for (const src of ['# Heading', '- item', '```cpp', '> quote']) {
      expect(renderInlineMarkdown(src)).not.toMatch(/<(h1|ul|li|pre|p|blockquote)\b/)
    }
  })

  it('is sanitised like the block renderer', () => {
    expect(renderInlineMarkdown('<script>alert(1)</script>')).not.toContain('<script')
    expect(renderInlineMarkdown('[x](javascript:alert(1))')).not.toMatch(/href="javascript/i)
    expect(renderInlineMarkdown('![x](https://evil.example/p.png)')).not.toContain('<img')
  })
})
