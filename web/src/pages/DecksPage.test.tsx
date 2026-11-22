import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { deck, makeApiMock } from '../test/mockApi'
import { DecksPage } from './DecksPage'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return { ...orig, get api() { return mockApi.current } }
})

beforeEach(() => {
  mockApi.current = makeApiMock()
})

function renderPage() {
  return render(
    <MemoryRouter>
      <DecksPage />
    </MemoryRouter>,
  )
}

describe('DecksPage', () => {
  it('lists decks with counts and study link', async () => {
    mockApi.current.listDecks.mockResolvedValue([
      deck(),
      deck({ id: 2, name: 'Go', counts: { new: 0, learning: 0, review: 0 } }),
    ])
    renderPage()
    const cpp = (await screen.findByRole('link', { name: 'Cpp' })).closest('li')!
    expect(within(cpp).getByRole('link', { name: 'Cpp' })).toHaveAttribute('href', '/decks/1')
    expect(cpp).toHaveTextContent('1 learning')
    expect(cpp).toHaveTextContent('2 review')
    expect(cpp).toHaveTextContent('3 new')
    expect(within(cpp).getByRole('link', { name: 'Study Cpp' })).toHaveAttribute('href', '/decks/1/study')

    const go = screen.getByRole('link', { name: 'Go' }).closest('li')!
    expect(within(go).queryByRole('link', { name: 'Study Go' })).toBeNull()
    expect(within(go).getByText('Study')).toHaveAttribute('aria-disabled', 'true')
  })

  it('creates a deck and reloads the list', async () => {
    mockApi.current.listDecks.mockResolvedValueOnce([]).mockResolvedValue([deck({ name: 'Rust' })])
    mockApi.current.createDeck.mockResolvedValue(deck({ name: 'Rust' }))
    renderPage()
    await userEvent.type(await screen.findByLabelText('New deck name'), 'Rust')
    await userEvent.click(screen.getByRole('button', { name: 'Create deck' }))
    expect(mockApi.current.createDeck).toHaveBeenCalledWith('Rust', false)
    expect(await screen.findByRole('link', { name: 'Rust' })).toBeInTheDocument()
    expect(screen.getByLabelText('New deck name')).toHaveValue('')
  })

  it('creates a two-sided deck', async () => {
    mockApi.current.listDecks.mockResolvedValue([])
    mockApi.current.createDeck.mockResolvedValue(deck({ name: 'Rust', twoSided: true }))
    renderPage()
    await userEvent.type(await screen.findByLabelText('New deck name'), 'Rust')
    await userEvent.click(screen.getByRole('checkbox', { name: 'Two-sided' }))
    await userEvent.click(screen.getByRole('button', { name: 'Create deck' }))
    expect(mockApi.current.createDeck).toHaveBeenCalledWith('Rust', true)
    expect(screen.getByRole('checkbox', { name: 'Two-sided' })).not.toBeChecked()
  })

  it('shows duplicate-name error inline', async () => {
    mockApi.current.listDecks.mockResolvedValue([deck()])
    mockApi.current.createDeck.mockRejectedValue(new ApiError(409, 'conflict', 'x'))
    renderPage()
    await userEvent.type(await screen.findByLabelText('New deck name'), 'Cpp')
    await userEvent.click(screen.getByRole('button', { name: 'Create deck' }))
    expect(await screen.findByText('A deck with that name already exists')).toBeInTheDocument()
  })

  it('shows a load error with retry', async () => {
    mockApi.current.listDecks.mockRejectedValueOnce(new ApiError(0, 'network', 'Could not reach the server')).mockResolvedValue([deck()])
    renderPage()
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server')
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('link', { name: 'Cpp' })).toBeInTheDocument()
  })
})

describe('DecksPage while loading', () => {
  it('shows neither the empty message nor the form until the list arrives', async () => {
    let resolve!: (v: ReturnType<typeof deck>[]) => void
    mockApi.current.listDecks.mockReturnValue(new Promise((r) => (resolve = r)))
    renderPage()
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument()
    expect(screen.queryByText(/No decks yet/)).toBeNull()
    expect(screen.queryByLabelText('New deck name')).toBeNull()
    resolve([])
    expect(await screen.findByText(/No decks yet/)).toBeInTheDocument()
    expect(screen.queryByRole('status', { name: 'Loading' })).toBeNull()
  })
})

describe('DecksPage rows', () => {
  it('makes the whole row a link to the deck (stretched over the row), with Study above it', async () => {
    mockApi.current.listDecks.mockResolvedValue([deck()])
    renderPage()
    const link = await screen.findByRole('link', { name: 'Cpp' })
    expect(link).toHaveClass('stretched-link-after')
    expect(link.closest('li')).toHaveClass('deck-row')
  })
})
