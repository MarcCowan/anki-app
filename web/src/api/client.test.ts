import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, onUnauthorized } from './client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const fetchMock = vi.fn<typeof fetch>()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
  onUnauthorized(() => {})
})

describe('api client', () => {
  it('sends JSON with content type', async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, { id: 1, name: 'Cpp', newPerDay: 20, counts: { new: 0, learning: 0, review: 0 } }))
    const deck = await api.createDeck('Cpp')
    expect(deck.name).toBe('Cpp')
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/decks')
    expect(init?.method).toBe('POST')
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
    expect(init?.body).toBe('{"name":"Cpp","twoSided":false}')
    expect(init?.credentials).toBe('same-origin')
  })

  it('sends the JSON content type on DELETE too', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await api.deleteCard(3)
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/cards/3')
    expect(init?.method).toBe('DELETE')
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
  })

  it('throws ApiError with server code', async () => {
    fetchMock.mockResolvedValue(jsonResponse(409, { error: { code: 'stale', message: 'm' } }))
    const err = await api.review(1, { rating: 3, expectedReps: 0, durationMs: 5 }).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 409, code: 'stale', message: 'm' })
  })

  it('maps network failure', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'))
    const err = await api.listDecks().catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 0, code: 'network' })
  })

  it('keeps the status when an error body is not JSON', async () => {
    fetchMock.mockResolvedValue(new Response('bad gateway', { status: 502 }))
    const err = await api.listDecks().catch((e: unknown) => e)
    expect(err).toMatchObject({ status: 502, code: 'unknown' })
  })

  it('calls onUnauthorized on 401', async () => {
    const handler = vi.fn()
    onUnauthorized(handler)
    fetchMock.mockResolvedValue(jsonResponse(401, { error: { code: 'unauthorized', message: 'login' } }))
    await expect(api.listDecks()).rejects.toMatchObject({ status: 401 })
    expect(handler).toHaveBeenCalledOnce()
  })

  it('returns undefined for 204', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await expect(api.deleteDeck(1)).resolves.toBeUndefined()
  })

  it('builds the review and undo requests', async () => {
    fetchMock.mockImplementation(async () => jsonResponse(200, { state: {} }))
    await api.review(7, { rating: 4, expectedReps: 2, durationMs: 900 })
    await api.undo(7, 3)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/states/7/review')
    expect(fetchMock.mock.calls[0]![1]?.body).toBe('{"rating":4,"expectedReps":2,"durationMs":900}')
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/states/7/undo')
    expect(fetchMock.mock.calls[1]![1]?.body).toBe('{"expectedReps":3}')
  })
})

describe('Claude connector calls', () => {
  it('gets the connector status', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { enabled: false, connectorUrl: '', grants: [], batches: [] }))
    const s = await api.getMcp()
    expect(s.enabled).toBe(false)
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/mcp')
    expect(init?.method).toBe('GET')
  })

  it('deletes a batch with the JSON content type', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await api.deleteMcpBatch(7)
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/mcp/batches/7')
    expect(init?.method).toBe('DELETE')
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
  })

  it('disconnects Claude', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await api.disconnectMcp()
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/mcp/grants')
    expect(init?.method).toBe('DELETE')
  })
})
