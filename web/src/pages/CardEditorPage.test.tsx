import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { card, deck, makeApiMock } from '../test/mockApi'
import { CardEditorPage, SAVED_FLASH_MS } from './CardEditorPage'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return { ...orig, get api() { return mockApi.current } }
})

beforeEach(() => {
  mockApi.current = makeApiMock()
  mockApi.current.getDeck.mockResolvedValue(deck({ twoSided: false }))
})

/** A new card waits for its deck (for the two-sided default) before showing the form. */
async function renderNew(path: string) {
  renderAt(path)
  await screen.findByLabelText('Front')
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/decks/:id/cards/new" element={<CardEditorPage mode="new" />} />
        <Route path="/cards/:id/edit" element={<CardEditorPage mode="edit" />} />
        <Route path="/decks/:id" element={<p>deck page</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

const front = () => screen.getByLabelText('Front')
const back = () => screen.getByLabelText('Back')
const save = () => screen.getByRole('button', { name: /^(Add card|Save changes)$/ })

describe('CardEditorPage', () => {
  it('save disabled when empty or over limit', async () => {
    await renderNew('/decks/1/cards/new')
    expect(save()).toBeDisabled()
    await userEvent.type(front(), 'Q')
    expect(save()).toBeDisabled()
    await userEvent.type(back(), '   ')
    expect(save()).toBeDisabled()
    await userEvent.type(back(), 'A')
    expect(save()).toBeEnabled()
    fireEvent.change(back(), { target: { value: '→'.repeat(6001) } })
    expect(save()).toBeDisabled()
    expect(screen.getByText('6001 / 6000')).toHaveClass('over')
    fireEvent.change(back(), { target: { value: '→'.repeat(6000) } })
    expect(save()).toBeEnabled()
  })

  it('create posts and clears form', async () => {
    mockApi.current.createCard.mockResolvedValue(card())
    await renderNew('/decks/4/cards/new')
    await userEvent.type(front(), 'What is RAII?')
    await userEvent.type(back(), 'Scope-bound resources')
    await userEvent.click(save())
    expect(mockApi.current.createCard).toHaveBeenCalledWith(4, { front: 'What is RAII?', back: 'Scope-bound resources', twoSided: false })
    expect(await screen.findByText('Card added')).toBeInTheDocument()
    expect(front()).toHaveValue('')
    expect(back()).toHaveValue('')
    expect(front()).toHaveFocus()
  })

  it('saves with Ctrl+Enter', async () => {
    mockApi.current.createCard.mockResolvedValue(card())
    await renderNew('/decks/4/cards/new')
    await userEvent.type(front(), 'Q')
    await userEvent.type(back(), 'A{Control>}{Enter}{/Control}')
    expect(mockApi.current.createCard).toHaveBeenCalledWith(4, { front: 'Q', back: 'A', twoSided: false })
  })

  it('edit loads card and saves changes', async () => {
    mockApi.current.getCard.mockResolvedValue(card({ id: 9, deckId: 3, front: 'Old Q', back: 'Old A' }))
    mockApi.current.updateCard.mockResolvedValue(card({ id: 9, deckId: 3 }))
    renderAt('/cards/9/edit')
    expect(await screen.findByDisplayValue('Old Q')).toBe(front())
    expect(back()).toHaveValue('Old A')
    await userEvent.clear(front())
    await userEvent.type(front(), 'New Q')
    await userEvent.click(save())
    expect(mockApi.current.updateCard).toHaveBeenCalledWith(9, { front: 'New Q', back: 'Old A', twoSided: false })
    expect(await screen.findByRole('status')).toHaveTextContent('Saved')
    expect(screen.queryByText('deck page')).toBeNull()
    expect(front()).toHaveValue('New Q')
  })

  it('the saved indicator goes away by itself', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      mockApi.current.getCard.mockResolvedValue(card({ id: 9, deckId: 3 }))
      mockApi.current.updateCard.mockResolvedValue(card({ id: 9, deckId: 3 }))
      renderAt('/cards/9/edit')
      await screen.findByRole('button', { name: 'Save changes' })
      fireEvent.click(save())
      expect(await screen.findByRole('status')).toHaveTextContent('Saved')
      await act(async () => {
        await vi.advanceTimersByTimeAsync(SAVED_FLASH_MS + 50)
      })
      expect(screen.queryByRole('status')).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows a loading state, not empty boxes, while the card loads', () => {
    mockApi.current.getCard.mockReturnValue(new Promise(() => {}))
    renderAt('/cards/9/edit')
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Front')).toBeNull()
  })

  it('does not print keyboard hints (they live on the Help page)', async () => {
    await renderNew('/decks/1/cards/new')
    expect(screen.queryByText(/Ctrl\+Enter/)).toBeNull()
  })

  it('shows the server validation message', async () => {
    mockApi.current.createCard.mockRejectedValue(new ApiError(400, 'invalid_input', 'front is 6001 characters; the limit is 6000'))
    await renderNew('/decks/1/cards/new')
    await userEvent.type(front(), 'Q')
    await userEvent.type(back(), 'A')
    await userEvent.click(save())
    expect(await screen.findByRole('alert')).toHaveTextContent('front is 6001 characters; the limit is 6000')
    expect(front()).toHaveValue('Q')
  })

  it('deletes a card after confirming', async () => {
    mockApi.current.getCard.mockResolvedValue(card({ id: 9, deckId: 3 }))
    mockApi.current.deleteCard.mockResolvedValue(undefined)
    renderAt('/cards/9/edit')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete card' }))
    await userEvent.click(screen.getByRole('button', { name: 'Delete this card' }))
    expect(mockApi.current.deleteCard).toHaveBeenCalledWith(9)
    expect(await screen.findByText('deck page')).toBeInTheDocument()
  })

  it('preview renders markdown', async () => {
    await renderNew('/decks/1/cards/new')
    await userEvent.type(front(), 'call `x` now')
    const preview = screen.getByRole('region', { name: 'Front preview' })
    expect(preview.querySelector('code')).toHaveTextContent('x')
  })

  it('opens an existing card in preview mode', async () => {
    mockApi.current.getCard.mockResolvedValue(card({ id: 9, deckId: 3, front: 'Old `Q`', back: 'Old A' }))
    renderAt('/cards/9/edit')
    const preview = await screen.findByRole('tab', { name: 'Preview' })
    expect(preview).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'Edit' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('heading', { name: 'Card' })).toBeInTheDocument()
    const front = await screen.findByRole('region', { name: 'Front preview' })
    expect(await within(front).findByText('Q')).toHaveProperty('tagName', 'CODE')
    expect(document.querySelector('form.editor')).toHaveAttribute('data-tab', 'preview')
  })

  it('opens a new card in edit mode', async () => {
    await renderNew('/decks/1/cards/new')
    expect(screen.getByRole('tab', { name: 'Edit' })).toHaveAttribute('aria-selected', 'true')
    expect(document.querySelector('form.editor')).toHaveAttribute('data-tab', 'edit')
  })

  it('a new card starts with the deck default and can override it', async () => {
    mockApi.current.getDeck.mockResolvedValue(deck({ id: 4, twoSided: true }))
    mockApi.current.createCard.mockResolvedValue(card())
    await renderNew('/decks/4/cards/new')
    const box = screen.getByRole('checkbox', { name: /Two-sided/ })
    expect(box).toBeChecked()
    await userEvent.type(front(), 'Q')
    await userEvent.type(back(), 'A')
    await userEvent.click(box)
    await userEvent.click(save())
    expect(mockApi.current.createCard).toHaveBeenCalledWith(4, { front: 'Q', back: 'A', twoSided: false })
    // The choice sticks for the next card.
    expect(await screen.findByText('Card added')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: /Two-sided/ })).not.toBeChecked()
  })

  it('turning two-sided off on a saved card asks first', async () => {
    mockApi.current.getCard.mockResolvedValue(card({ id: 9, deckId: 3, twoSided: true }))
    mockApi.current.updateCard.mockResolvedValue(card({ id: 9, deckId: 3, twoSided: false }))
    renderAt('/cards/9/edit')
    const box = await screen.findByRole('checkbox', { name: /Two-sided/ })
    expect(box).toBeChecked()
    await userEvent.click(box)
    await userEvent.click(save())
    expect(mockApi.current.updateCard).not.toHaveBeenCalled()
    const dialog = screen.getByRole('dialog', { name: 'Remove the reverse side?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(mockApi.current.updateCard).not.toHaveBeenCalled()
    await userEvent.click(save())
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove reverse side' }))
    expect(mockApi.current.updateCard).toHaveBeenCalledWith(9, { front: 'Q', back: 'A', twoSided: false })
    expect(await screen.findByRole('status')).toHaveTextContent('Saved')
    // Saved: saving again does not ask.
    await userEvent.click(save())
    expect(mockApi.current.updateCard).toHaveBeenCalledTimes(2)
  })
})
