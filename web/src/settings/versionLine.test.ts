import { describe, expect, it } from 'vitest'
import { versionLine } from './versionLine'

describe('versionLine', () => {
  it('formats a winter backup in the given zone', () => {
    expect(versionLine({ version: '3f2a9c1d0b7e', lastBackupAt: Date.UTC(2026, 10, 5, 3, 0) }, 'Europe/London')).toBe(
      'Version 3f2a9c1d0b7e · Last backup 5 Nov, 03:00',
    )
  })
  it('applies summer time', () => {
    expect(versionLine({ version: '3f2a9c1d0b7e', lastBackupAt: Date.UTC(2026, 6, 1, 2, 0) }, 'Europe/London')).toBe(
      'Version 3f2a9c1d0b7e · Last backup 1 Jul, 03:00',
    )
  })
  it('says none yet when there is no backup', () => {
    expect(versionLine({ version: '3f2a9c1d0b7e', lastBackupAt: null })).toBe('Version 3f2a9c1d0b7e · Last backup: none yet')
  })
})
