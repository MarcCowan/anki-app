import type { ApiError } from '../api/client'
import type { NextCard, NextResponse } from '../api/types'

/** The most recent answer, which Undo reverts: one direction (review state) of a card. */
export interface LastAnswer {
  stateId: number
  repsAfter: number
}

/** Which side is asked and which is revealed: a reverse item asks the back. */
export function faces(next: NextCard): { question: string; answer: string } {
  return next.reverse
    ? { question: next.card.back, answer: next.card.front }
    : { question: next.card.front, answer: next.card.back }
}

export type StudyState =
  | { kind: 'loading'; last?: LastAnswer }
  | { kind: 'front'; next: NextCard; shownAt: number; last?: LastAnswer }
  | { kind: 'back'; next: NextCard; shownAt: number; last?: LastAnswer }
  | { kind: 'done'; nextDueAt: number | null; last?: LastAnswer }
  | { kind: 'error'; message: string; last?: LastAnswer }

export type StudyEvent =
  | { type: 'loaded'; res: NextResponse; now: number }
  | { type: 'reveal' }
  | { type: 'answered'; stateId: number; repsAfter: number }
  | { type: 'undone' }
  | { type: 'failed'; error: ApiError }

export function studyReducer(s: StudyState, e: StudyEvent): StudyState {
  switch (e.type) {
    case 'loaded':
      return e.res.done
        ? { kind: 'done', nextDueAt: e.res.nextDueAt, last: s.last }
        : { kind: 'front', next: e.res, shownAt: e.now, last: s.last }
    case 'reveal':
      return s.kind === 'front' ? { ...s, kind: 'back' } : s
    case 'answered':
      return { kind: 'loading', last: { stateId: e.stateId, repsAfter: e.repsAfter } }
    case 'undone':
      return { kind: 'loading' }
    case 'failed':
      return { kind: 'error', message: e.error.message || 'Something went wrong', last: s.last }
  }
}

/**
 * A 409 (answered elsewhere, or a double tap) or a 404 (card deleted on
 * another device) means this screen is out of date: fetch the next card.
 */
export function shouldReloadAfterError(e: ApiError): boolean {
  return e.status === 409 || e.status === 404
}

const pad = (n: number) => String(n).padStart(2, '0')

/** "Next card due at 14:05" today, or "... on Thu 5 Nov at 09:30" on another day. */
export function formatNextDue(ts: number, now: number = Date.now()): string {
  const due = new Date(ts)
  const today = new Date(now)
  const time = `${pad(due.getHours())}:${pad(due.getMinutes())}`
  if (due.toDateString() === today.toDateString()) return `Next card due at ${time}`
  const day = due.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
  return `Next card due on ${day} at ${time}`
}
