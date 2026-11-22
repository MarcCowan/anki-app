import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import type { NextCard, NextDone, ReviewState } from '../api/types'
import { card, makeApiMock } from '../test/mockApi'
import { StudyPage } from './StudyPage'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return { ...orig, get api() { return mockApi.current } }
})

const remaining = { new: 1, learning: 2, review: 3 }
/** A state's id is its card's id + 100, so tests can tell the two apart. */
function nextCard(id: number, front: string, reps = 0, reverse = false): NextCard {
  return {
    done: false,
    card: card({ id, front, back: `back of ${front}`, reps }),
    stateId: id + 100,
    reverse,
    reps,
    remaining,
    previews: {
      again: { due: 0, intervalLabel: '1m' },
      hard: { due: 0, intervalLabel: '6m' },
      good: { due: 0, intervalLabel: '10m' },
      easy: { due: 0, intervalLabel: '8d' },
    },
  }
}
function reviewState(id: number, reps: number): ReviewState {
  return { id, cardId: id - 100, reverse: false, state: 1, due: 0, reps }
}
const done: NextDone = { done: true, nextDueAt: null, remaining: { new: 0, learning: 0, review: 0 } }

beforeEach(() => {
  mockApi.current = makeApiMock()
})

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/decks/1/study']}>
      <Routes>
        <Route path="/decks/:id/study" element={<StudyPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('StudyPage', () => {
  it('a reverse item asks the back, reveals the front, and answers its own state', async () => {
    mockApi.current.next.mockResolvedValueOnce(nextCard(7, 'Q seven', 0, true)).mockResolvedValue(done)
    mockApi.current.review.mockResolvedValue({ state: reviewState(107, 1) })
    renderPage()
    expect(await screen.findByText('back of Q seven')).toBeInTheDocument()
    expect(screen.queryByText('Q seven')).toBeNull()
    expect(screen.getByText('Reverse: back → front')).toBeInTheDocument()
    await userEvent.keyboard(' ')
    expect(screen.getByText('Q seven')).toBeInTheDocument()
    await userEvent.keyboard('3')
    expect(mockApi.current.review).toHaveBeenCalledWith(107, expect.objectContaining({ rating: 3 }))
  })

  it('space reveals then rates good', async () => {
    mockApi.current.next.mockResolvedValueOnce(nextCard(7, 'Q seven', 4)).mockResolvedValue(done)
    mockApi.current.review.mockResolvedValue({ state: reviewState(107, 5) })
    renderPage()
    expect(await screen.findByText('Q seven')).toBeInTheDocument()
    expect(screen.queryByText('back of Q seven')).toBeNull()
    expect(screen.getByText('2 learning')).toBeInTheDocument()
    await userEvent.keyboard(' ')
    expect(screen.getByText('back of Q seven')).toBeInTheDocument()
    expect(screen.getByText('Q seven')).toBeInTheDocument()
    await userEvent.keyboard(' ')
    expect(mockApi.current.review).toHaveBeenCalledWith(107, expect.objectContaining({ rating: 3, expectedReps: 4 }))
    const durationMs = mockApi.current.review.mock.calls[0]![1].durationMs
    expect(durationMs).toBeGreaterThanOrEqual(0)
    expect(await screen.findByText('Done for now')).toBeInTheDocument()
  })

  it('number keys rate', async () => {
    mockApi.current.next.mockResolvedValueOnce(nextCard(7, 'Q')).mockResolvedValue(done)
    mockApi.current.review.mockResolvedValue({ state: reviewState(107, 1) })
    renderPage()
    await screen.findByText('Q')
    await userEvent.keyboard('1')
    expect(mockApi.current.review).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Show answer' }))
    await userEvent.keyboard('1')
    expect(mockApi.current.review).toHaveBeenCalledWith(107, expect.objectContaining({ rating: 1, expectedReps: 0 }))
  })

  it('buttons show interval labels', async () => {
    mockApi.current.next.mockResolvedValue(nextCard(7, 'Q'))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Show answer' }))
    for (const name of ['Again · 1m', 'Hard · 6m', 'Good · 10m', 'Easy · 8d']) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument()
    }
  })

  it('ignores double press while in flight', async () => {
    mockApi.current.next.mockResolvedValueOnce(nextCard(7, 'Q')).mockResolvedValue(done)
    let resolve: (v: { state: ReviewState }) => void = () => {}
    mockApi.current.review.mockReturnValue(new Promise((r) => (resolve = r)))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Show answer' }))
    await userEvent.keyboard('33')
    await userEvent.click(screen.getByRole('button', { name: 'Good · 10m' }))
    expect(mockApi.current.review).toHaveBeenCalledTimes(1)
    await act(async () => resolve({ state: reviewState(107, 1) }))
    expect(await screen.findByText('Done for now')).toBeInTheDocument()
  })

  it('undo calls api.undo with repsAfter', async () => {
    mockApi.current.next
      .mockResolvedValueOnce(nextCard(7, 'Q'))
      .mockResolvedValueOnce(nextCard(8, 'Q eight'))
      .mockResolvedValue(nextCard(7, 'Q'))
    mockApi.current.review.mockResolvedValue({ state: reviewState(107, 1) })
    mockApi.current.undo.mockResolvedValue({ state: reviewState(107, 0) })
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Show answer' }))
    await userEvent.click(screen.getByRole('button', { name: 'Easy · 8d' }))
    expect(await screen.findByText('Q eight')).toBeInTheDocument()
    await userEvent.keyboard('z')
    expect(mockApi.current.undo).toHaveBeenCalledWith(107, 1)
    expect(await screen.findByText('Q')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull()
  })

  it('reloads quietly on 409 and 404 from review', async () => {
    mockApi.current.next.mockResolvedValueOnce(nextCard(7, 'Q')).mockResolvedValue(nextCard(9, 'Q nine'))
    mockApi.current.review.mockRejectedValue(new ApiError(404, 'not_found', 'not found'))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Show answer' }))
    await userEvent.keyboard('3')
    expect(await screen.findByText('Q nine')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows an error with retry on other failures', async () => {
    mockApi.current.next.mockResolvedValueOnce(nextCard(7, 'Q')).mockResolvedValue(done)
    mockApi.current.review.mockRejectedValue(new ApiError(500, 'internal', 'internal error'))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Show answer' }))
    await userEvent.keyboard('3')
    expect(await screen.findByRole('alert')).toHaveTextContent('internal error')
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Done for now')).toBeInTheDocument()
  })

  it('shows done state with next due time', async () => {
    mockApi.current.next.mockResolvedValue({ ...done, nextDueAt: Date.now() + 5 * 60_000 })
    renderPage()
    expect(await screen.findByText('Done for now')).toBeInTheDocument()
    expect(screen.getByText(/^Next card due (at|on)/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to deck' })).toHaveAttribute('href', '/decks/1')
  })
})

describe('StudyPage chrome', () => {
  it('does not print keyboard hints (they live on the Help page)', async () => {
    mockApi.current.next.mockResolvedValue(nextCard(7, 'Q'))
    renderPage()
    await screen.findByText('Q')
    expect(screen.queryByText(/1–4 rate/)).toBeNull()
  })
})
