import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { Markdown } from './Markdown'

it('renders sanitised html into the DOM', () => {
  const { container } = render(<Markdown source={'**b** `c` <b onclick="x()">raw</b>'} className="front" />)
  expect(screen.getByText('b').tagName).toBe('STRONG')
  expect(container.querySelector('code')).toHaveTextContent('c')
  expect(container.querySelector('[onclick]')).toBeNull()
  expect(container.firstElementChild).toHaveClass('md', 'front')
})

// The payload server/internal/mcpserver stores verbatim from add_cards
// (TestAddCardsMarkdownStoredVerbatim): cards Claude adds go through the same
// renderer, so none of it may become live markup.
it('renders card text added by Claude harmlessly', () => {
  const payload = '<script>alert(1)</script> [x](javascript:alert(1)) <img src=x onerror=alert(1)>'
  const { container } = render(<Markdown source={payload} className="front" />)
  expect(container.querySelector('script')).toBeNull()
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('[onerror]')).toBeNull()
  for (const a of container.querySelectorAll('a')) {
    expect(a.getAttribute('href') ?? '').not.toMatch(/^\s*javascript:/i)
  }
  expect(container).toHaveTextContent('<script>alert(1)</script>')
})

it('renders inline markdown into a span', () => {
  const { container } = render(<Markdown source={'call `f()` now'} inline className="row" />)
  const el = container.firstElementChild
  expect(el?.tagName).toBe('SPAN')
  expect(el).toHaveClass('md', 'md-inline', 'row')
  expect(el?.querySelector('code')).toHaveTextContent('f()')
  expect(el?.querySelector('p')).toBeNull()
})
