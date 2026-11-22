import type { McpBatch, McpGrant } from '../api/types'

/** "8 Nov, 14:05" in the device zone (or `timeZone`), like the version line. */
export function formatWhen(ms: number, timeZone?: string): string {
  const parts = new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
    timeZone,
  }).formatToParts(new Date(ms))
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? ''
  return `${get('day')} ${get('month')}, ${get('hour')}:${get('minute')}`
}

export function cardsText(n: number): string {
  return n === 1 ? '1 card' : `${n} cards`
}

/** "Connected 8 Nov, 14:05 · last used 8 Nov, 15:05" */
export function grantLine(g: McpGrant, timeZone?: string): string {
  return `Connected ${formatWhen(g.createdAt, timeZone)} · last used ${formatWhen(g.lastUsedAt, timeZone)}`
}

/** "8 Nov, 14:05 · Go · 12 cards", or "… · 9 of 12 left" once some are gone. */
export function batchLine(b: McpBatch, timeZone?: string): string {
  const count = b.remaining === b.cardCount ? cardsText(b.cardCount) : `${b.remaining} of ${b.cardCount} left`
  return `${formatWhen(b.createdAt, timeZone)} · ${b.deckName} · ${count}`
}
