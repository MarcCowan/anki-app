import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client'
import type { NextCard, NextDone } from '../api/types'
import { card } from '../test/mockApi'
import { faces, formatNextDue, shouldReloadAfterError, studyReducer, type StudyState } from './studyReducer'

const counts = { new: 1, learning: 0, review: 0 }
const p = { due: 0, intervalLabel: '1m' }
const nextCard: NextCard = {
  done: false, card: card({ id: 5, reps: 2, front: 'F', back: 'B' }), stateId: 8, reverse: false, reps: 2, remaining: counts,
  previews: { again: p, hard: p, good: p, easy: p },
}
const nextDone: NextDone = { done: true, nextDueAt: 1234, remaining: counts }

describe('faces', () => {
  it('forward: front is the question, back the answer', () => {
    expect(faces(nextCard)).toEqual({ question: 'F', answer: 'B' })
  })
  it('reverse: back is the question, front the answer', () => {
    expect(faces({ ...nextCard, reverse: true })).toEqual({ question: 'B', answer: 'F' })
  })
})

describe('studyReducer', () => {
  it('loaded not-done -> front with shownAt = now', () => {
    expect(studyReducer({ kind: 'loading' }, { type: 'loaded', res: nextCard, now: 99 })).toEqual({
      kind: 'front', next: nextCard, shownAt: 99, last: undefined,
    })
  })

  it('reveal: front -> back; reveal in back is a no-op', () => {
    const front: StudyState = { kind: 'front', next: nextCard, shownAt: 1 }
    const back = studyReducer(front, { type: 'reveal' })
    expect(back).toEqual({ kind: 'back', next: nextCard, shownAt: 1 })
    expect(studyReducer(back, { type: 'reveal' })).toBe(back)
    const done: StudyState = { kind: 'done', nextDueAt: null }
    expect(studyReducer(done, { type: 'reveal' })).toBe(done)
  })

  it('answered records last and goes to loading', () => {
    const back: StudyState = { kind: 'back', next: nextCard, shownAt: 1 }
    const s = studyReducer(back, { type: 'answered', stateId: 5, repsAfter: 3 })
    expect(s).toEqual({ kind: 'loading', last: { stateId: 5, repsAfter: 3 } })
    expect(studyReducer(s, { type: 'loaded', res: nextCard, now: 7 })).toMatchObject({
      kind: 'front', last: { stateId: 5, repsAfter: 3 },
    })
  })

  it('loaded done -> done with nextDueAt, keeps last', () => {
    const s = studyReducer({ kind: 'loading', last: { stateId: 5, repsAfter: 3 } }, { type: 'loaded', res: nextDone, now: 1 })
    expect(s).toEqual({ kind: 'done', nextDueAt: 1234, last: { stateId: 5, repsAfter: 3 } })
  })

  it('undone clears last', () => {
    const s = studyReducer({ kind: 'front', next: nextCard, shownAt: 1, last: { stateId: 5, repsAfter: 3 } }, { type: 'undone' })
    expect(s).toEqual({ kind: 'loading' })
  })

  it('failed keeps last and shows the message', () => {
    const s = studyReducer({ kind: 'loading', last: { stateId: 5, repsAfter: 3 } }, { type: 'failed', error: new ApiError(500, 'internal', 'internal error') })
    expect(s).toEqual({ kind: 'error', message: 'internal error', last: { stateId: 5, repsAfter: 3 } })
  })

  it('404 on answer loads next', () => {
    expect(shouldReloadAfterError(new ApiError(404, 'not_found', ''))).toBe(true)
    expect(shouldReloadAfterError(new ApiError(409, 'stale', ''))).toBe(true)
    expect(shouldReloadAfterError(new ApiError(500, 'internal', ''))).toBe(false)
    expect(shouldReloadAfterError(new ApiError(0, 'network', ''))).toBe(false)
  })
})

describe('formatNextDue', () => {
  const now = new Date(2026, 10, 2, 10, 0).getTime()
  it('shows only the time today', () => {
    expect(formatNextDue(new Date(2026, 10, 2, 14, 5).getTime(), now)).toBe('Next card due at 14:05')
  })
  it('adds the date on another day', () => {
    expect(formatNextDue(new Date(2026, 10, 5, 9, 30).getTime(), now)).toMatch(/^Next card due on .+ at 09:30$/)
  })
})
