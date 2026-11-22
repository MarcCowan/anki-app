import { describe, expect, it } from 'vitest'
import { batchLine, formatWhen, grantLine } from './connectorText'

const ms = Date.UTC(2026, 10, 8, 14, 5)

describe('connector text', () => {
  it('formats a time like the version line', () => {
    expect(formatWhen(ms, 'UTC')).toBe('8 Nov, 14:05')
  })

  it('describes a grant', () => {
    expect(grantLine({ id: 1, createdAt: ms, lastUsedAt: ms + 3_600_000 }, 'UTC')).toBe(
      'Connected 8 Nov, 14:05 · last used 8 Nov, 15:05',
    )
  })

  it('describes a whole batch', () => {
    const b = { id: 1, deckId: 2, deckName: 'Go', createdAt: ms, cardCount: 12, remaining: 12 }
    expect(batchLine(b, 'UTC')).toBe('8 Nov, 14:05 · Go · 12 cards')
  })

  it('says how many cards of a batch are left', () => {
    const b = { id: 1, deckId: 2, deckName: 'Go', createdAt: ms, cardCount: 12, remaining: 9 }
    expect(batchLine(b, 'UTC')).toBe('8 Nov, 14:05 · Go · 9 of 12 left')
  })

  it('uses the singular for one card', () => {
    const b = { id: 1, deckId: 2, deckName: 'Go', createdAt: ms, cardCount: 1, remaining: 1 }
    expect(batchLine(b, 'UTC')).toBe('8 Nov, 14:05 · Go · 1 card')
  })
})
