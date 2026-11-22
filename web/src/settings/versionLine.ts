import type { Health } from '../api/types'

function formatBackup(ms: number, timeZone?: string): string {
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

/** "Version <id> · Last backup 5 Nov, 03:00" in the device zone (or `timeZone`). */
export function versionLine(h: Health, timeZone?: string): string {
  const backup = h.lastBackupAt === null ? ': none yet' : ` ${formatBackup(h.lastBackupAt, timeZone)}`
  return `Version ${h.version} · Last backup${backup}`
}
