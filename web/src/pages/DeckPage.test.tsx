import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { card, deck, makeApiMock } from '../test/mockApi'
import { DeckPage, GRID_QUERY } from './DeckPage'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return { ...orig, get api() { return mockApi.current } }
})

beforeEach(() => {
  mockApi.current = makeApiMock()
  mockApi.current.getDeck.mockResolvedValue(deck())
  mockApi.current.listCards.mockResolvedValue([
    card({ id: 1, front: '\n  What does `std::vector<T>::reserve` do?\nmore', back: 'A' }),
    card({ id: 2, front: 'Rule of three', back: 'copy ctor' }),
    card({ id: 3, front: 'RAII', back: 'Resource acquisition' }),
  ])
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function stubWide(wide: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((q: string) => ({ matches: wide && q === GRID_QUERY, addEventListener: vi.fn(), removeEventListener: vi.fn() })),
  )
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/decks/1']}>
      <Routes>
        <Route path="/decks/:id" element={<DeckPage />} />
        <Route path="/" element={<p>home page</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('DeckPage', () => {
  it('narrow screens: lists the first line of each front, with inline code formatted', async () => {
    renderPage()
    const link = await screen.findByRole('link', { name: 'What does `std::vector<T>::reserve` do?' })
    expect(link).toHaveAttribute('href', '/cards/1/edit')
    const row = link.closest('li')!
    expect(row.querySelector('code')).toHaveTextContent('std::vector<T>::reserve')
    expect(row).not.toHaveTextContent('more')
    expect(screen.queryByRole('list', { name: 'Card previews' })).toBeNull()
    expect(screen.getByRole('link', { name: 'Add card' })).toHaveAttribute('href', '/decks/1/cards/new')
    expect(screen.getByRole('link', { name: 'Study' })).toHaveAttribute('href', '/decks/1/study')
  })

  it('wide screens: shows a grid of front-above-back previews', async () => {
    stubWide(true)
    mockApi.current.listCards.mockResolvedValue([
      card({ id: 1, front: 'Deduced type?\n\n```cpp\nauto a = 1;\n```', back: '`int`' }),
      card({ id: 2, front: 'RAII', back: 'Resource acquisition' }),
    ])
    renderPage()
    const grid = await screen.findByRole('list', { name: 'Card previews' })
    const tiles = within(grid).getAllByRole('listitem')
    expect(tiles).toHaveLength(2)
    const first = tiles[0]!
    expect(within(first).getByRole('link', { name: 'Deduced type?' })).toHaveAttribute('href', '/cards/1/edit')
    const front = first.querySelector('.card-tile-front')!
    const back = first.querySelector('.card-tile-back')!
    expect(front.querySelector('.code-block .line')).toHaveTextContent('auto a = 1;')
    expect(back.querySelector('code')).toHaveTextContent('int')
    expect(front.compareDocumentPosition(back) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('filters cards by text', async () => {
    renderPage()
    await screen.findByText('RAII')
    await userEvent.type(screen.getByLabelText('Filter cards'), 'VECTOR')
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    await userEvent.clear(screen.getByLabelText('Filter cards'))
    await userEvent.type(screen.getByLabelText('Filter cards'), 'acquisition')
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('RAII')).toBeInTheDocument()
  })

  it('saves deck settings', async () => {
    mockApi.current.updateDeck.mockResolvedValue(deck({ name: 'C++', newPerDay: 5 }))
    renderPage()
    const name = await screen.findByLabelText('Deck name')
    await userEvent.clear(name)
    await userEvent.type(name, 'C++')
    const perDay = screen.getByLabelText('New cards per day')
    await userEvent.clear(perDay)
    await userEvent.type(perDay, '5')
    await userEvent.click(screen.getByRole('button', { name: 'Save deck settings' }))
    expect(mockApi.current.updateDeck).toHaveBeenCalledWith(1, { name: 'C++', newPerDay: 5 })
    expect(await screen.findByRole('heading', { name: 'C++' })).toBeInTheDocument()
  })

  it("shows new cards per day as a target with today's progress", async () => {
    mockApi.current.getDeck.mockResolvedValue(deck({ newPerDay: 20, counts: { new: 3, learning: 0, review: 0, newToday: 7 } }))
    renderPage()
    expect(await screen.findByText(/A daily target, not a limit\. New today: 7 of 20\./)).toBeInTheDocument()
  })

  it('turning two-sided on confirms, then makes every card two-sided', async () => {
    mockApi.current.updateDeck.mockResolvedValue(deck({ twoSided: true }))
    renderPage()
    await userEvent.click(await screen.findByRole('checkbox', { name: /Two-sided by default/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Save deck settings' }))
    expect(mockApi.current.updateDeck).not.toHaveBeenCalled()
    const dialog = screen.getByRole('dialog', { name: 'Make every card two-sided?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make all two-sided' }))
    expect(mockApi.current.updateDeck).toHaveBeenCalledWith(1, { name: 'Cpp', newPerDay: 20, twoSided: true })
    expect(await screen.findByRole('status')).toHaveTextContent('Saved')
  })

  it('turning two-sided off can keep or remove the reverse sides', async () => {
    for (const [choice, removeReverses] of [['Keep reverse sides', false], ['Remove all reverse sides', true]] as const) {
      mockApi.current = makeApiMock()
      mockApi.current.getDeck.mockResolvedValue(deck({ twoSided: true }))
      mockApi.current.listCards.mockResolvedValue([])
      mockApi.current.updateDeck.mockResolvedValue(deck({ twoSided: false }))
      const { unmount } = renderPage()
      const box = await screen.findByRole('checkbox', { name: /Two-sided by default/ })
      expect(box).toBeChecked()
      await userEvent.click(box)
      await userEvent.click(screen.getByRole('button', { name: 'Save deck settings' }))
      const dialog = screen.getByRole('dialog', { name: 'Turn off two-sided by default?' })
      await userEvent.click(within(dialog).getByRole('button', { name: choice }))
      const want = removeReverses
        ? { name: 'Cpp', newPerDay: 20, twoSided: false, removeReverses: true }
        : { name: 'Cpp', newPerDay: 20, twoSided: false }
      expect(mockApi.current.updateDeck).toHaveBeenCalledWith(1, want)
      unmount()
    }
  })

  it('delete requires typing the deck name', async () => {
    mockApi.current.deleteDeck.mockResolvedValue(undefined)
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Delete deck' }))
    const dialog = screen.getByRole('dialog')
    const confirm = screen.getByRole('button', { name: 'Delete deck and its cards' })
    expect(confirm).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Type Cpp to confirm'), 'Cp')
    expect(confirm).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Type Cpp to confirm'), 'p')
    expect(confirm).toBeEnabled()
    await userEvent.click(confirm)
    expect(mockApi.current.deleteDeck).toHaveBeenCalledWith(1)
    expect(await screen.findByText('home page')).toBeInTheDocument()
    expect(dialog).not.toBeInTheDocument()
  })
})

describe('DeckPage while loading', () => {
  it('does not claim the deck is empty before the cards arrive', async () => {
    mockApi.current.listCards.mockReturnValue(new Promise(() => {}))
    renderPage()
    expect(await screen.findByRole('status', { name: 'Loading' })).toBeInTheDocument()
    expect(screen.queryByText(/No cards/)).toBeNull()
  })
})
