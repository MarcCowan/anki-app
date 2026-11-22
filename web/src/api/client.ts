// The only module that calls fetch.
import type { AuthStatus, Card, Deck, Health, McpStatus, NextResponse, Rating, ReviewState, Settings } from './types'

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

let unauthorizedHandler: () => void = () => {}

/** Register the handler called on any 401 response. */
export function onUnauthorized(handler: () => void): void {
  unauthorizedHandler = handler
}

function isErrorBody(v: unknown): v is { error: { code: string; message?: string } } {
  if (typeof v !== 'object' || v === null || !('error' in v)) return false
  const e = (v as { error: unknown }).error
  return typeof e === 'object' && e !== null && typeof (e as { code?: unknown }).code === 'string'
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  // The server requires the JSON content type on every mutation, DELETE included.
  if (method !== 'GET') headers['Content-Type'] = 'application/json'
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', 'Could not reach the server')
  }
  if (res.status === 401) unauthorizedHandler()
  if (res.status === 204) return undefined as T
  let data: unknown
  try {
    data = await res.json()
  } catch {
    if (!res.ok) throw new ApiError(res.status, 'unknown', `Request failed (${res.status})`)
    throw new ApiError(res.status, 'bad_response', 'The server sent an unreadable response')
  }
  if (!res.ok) {
    if (isErrorBody(data)) throw new ApiError(res.status, data.error.code, data.error.message ?? '')
    throw new ApiError(res.status, 'unknown', `Request failed (${res.status})`)
  }
  return data as T
}

export const api = {
  listDecks: () => request<Deck[]>('GET', '/api/decks'),
  getDeck: (id: number) => request<Deck>('GET', `/api/decks/${id}`),
  createDeck: (name: string, twoSided = false) => request<Deck>('POST', '/api/decks', { name, twoSided }),
  /** twoSided true makes every card two-sided; removeReverses needs twoSided false. */
  updateDeck: (id: number, p: { name?: string; newPerDay?: number; twoSided?: boolean; removeReverses?: boolean }) =>
    request<Deck>('PATCH', `/api/decks/${id}`, p),
  deleteDeck: (id: number) => request<void>('DELETE', `/api/decks/${id}`),

  listCards: (deckId: number) => request<Card[]>('GET', `/api/decks/${deckId}/cards`),
  getCard: (id: number) => request<Card>('GET', `/api/cards/${id}`),
  /** twoSided omitted: the deck's default. */
  createCard: (deckId: number, p: { front: string; back: string; twoSided?: boolean }) =>
    request<Card>('POST', `/api/decks/${deckId}/cards`, p),
  updateCard: (id: number, p: { front?: string; back?: string; twoSided?: boolean }) =>
    request<Card>('PATCH', `/api/cards/${id}`, p),
  deleteCard: (id: number) => request<void>('DELETE', `/api/cards/${id}`),

  next: (deckId: number) => request<NextResponse>('GET', `/api/decks/${deckId}/next`),
  review: (stateId: number, p: { rating: Rating; expectedReps: number; durationMs: number }) =>
    request<{ state: ReviewState }>('POST', `/api/states/${stateId}/review`, p),
  undo: (stateId: number, expectedReps: number) =>
    request<{ state: ReviewState }>('POST', `/api/states/${stateId}/undo`, { expectedReps }),

  getSettings: () => request<Settings>('GET', '/api/settings'),
  updateSettings: (p: Partial<Settings>) => request<Settings>('PATCH', '/api/settings', p),

  health: () => request<Health>('GET', '/api/health'),

  authStatus: () => request<AuthStatus>('GET', '/api/auth'),
  login: (passphrase: string) => request<void>('POST', '/api/login', { passphrase }),
  logout: () => request<void>('POST', '/api/logout', {}),

  getMcp: () => request<McpStatus>('GET', '/api/mcp'),
  deleteMcpBatch: (id: number) => request<void>('DELETE', `/api/mcp/batches/${id}`),
  disconnectMcp: () => request<void>('DELETE', '/api/mcp/grants'),
}
