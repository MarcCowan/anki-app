import { vi } from 'vitest'
import type { api as realApi } from '../api/client'
import type { Card, Deck } from '../api/types'

type Api = typeof realApi

/** A full api object whose every method is a vi.fn(). */
export function makeApiMock(): { [K in keyof Api]: ReturnType<typeof vi.fn<Api[K]>> } {
  const keys: (keyof Api)[] = [
    'listDecks', 'getDeck', 'createDeck', 'updateDeck', 'deleteDeck',
    'listCards', 'getCard', 'createCard', 'updateCard', 'deleteCard',
    'next', 'review', 'undo', 'getSettings', 'updateSettings',
    'authStatus', 'login', 'logout', 'health',
    'getMcp', 'deleteMcpBatch', 'disconnectMcp',
  ]
  return Object.fromEntries(keys.map((k) => [k, vi.fn()])) as never
}

export function deck(over: Partial<Deck> = {}): Deck {
  return { id: 1, name: 'Cpp', newPerDay: 20, twoSided: false, counts: { new: 3, learning: 1, review: 2 }, ...over }
}

export function card(over: Partial<Card> = {}): Card {
  return {
    id: 1, deckId: 1, front: 'Q', back: 'A', state: 0, due: 0, reps: 0, twoSided: false, createdAt: 0, updatedAt: 0, ...over,
  }
}
