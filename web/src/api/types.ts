// Mirrors the HTTP API in spec §6 and the two-sided spec §4. Times are UTC Unix milliseconds.

/** 0 New, 1 Learning, 2 Review, 3 Relearning */
export type CardState = 0 | 1 | 2 | 3

export interface Counts {
  new: number
  learning: number
  review: number
  /** New items introduced today, shown against the deck's daily target. */
  newToday?: number
}

export interface Deck {
  id: number
  name: string
  /** A daily target, not a cap. */
  newPerDay: number
  /** The default for new cards. */
  twoSided: boolean
  counts: Counts
}

/** state, due and reps are the forward (front → back) direction's. */
export interface Card {
  id: number
  deckId: number
  front: string
  back: string
  state: CardState
  due: number
  reps: number
  twoSided: boolean
  createdAt: number
  updatedAt: number
}

export type RatingKey = 'again' | 'hard' | 'good' | 'easy'
export type Rating = 1 | 2 | 3 | 4

export interface Preview {
  due: number
  intervalLabel: string
}

/** One direction of a card and its schedule. */
export interface ReviewState {
  id: number
  cardId: number
  /** true: back → front */
  reverse: boolean
  state: CardState
  due: number
  reps: number
}

export type NextCard = {
  done: false
  card: Card
  stateId: number
  /** Show the back first, then reveal the front. */
  reverse: boolean
  reps: number
  previews: Record<RatingKey, Preview>
  remaining: Counts
}
export type NextDone = { done: true; nextDueAt: number | null; remaining: Counts }
export type NextResponse = NextCard | NextDone

export interface Settings {
  desiredRetention: number
  dayRolloverHour: number
}

export interface Health {
  version: string
  lastBackupAt: number | null
}

export interface AuthStatus {
  enabled: boolean
  authenticated: boolean
}

export interface McpGrant {
  id: number
  createdAt: number
  lastUsedAt: number
}

export interface McpBatch {
  id: number
  deckId: number
  deckName: string
  createdAt: number
  cardCount: number
  remaining: number
}

export interface McpStatus {
  enabled: boolean
  connectorUrl: string
  grants: McpGrant[]
  batches: McpBatch[]
}
