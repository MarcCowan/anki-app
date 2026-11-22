import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyDisplay, loadDisplay, saveDisplay } from './display'

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('display settings', () => {
  it('defaults when storage empty or throws', () => {
    expect(loadDisplay()).toEqual({ wrapMode: 'auto', codeCols: 80 })
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(loadDisplay()).toEqual({ wrapMode: 'auto', codeCols: 80 })
  })

  it('clamps codeCols', () => {
    const cases: [unknown, number][] = [[500, 200], [10, 40], ['x', 80], [72, 72], [72.6, 73]]
    for (const [saved, want] of cases) {
      localStorage.setItem('display.v1', JSON.stringify({ wrapMode: 'wrap', codeCols: saved }))
      expect(loadDisplay()).toEqual({ wrapMode: 'wrap', codeCols: want })
    }
    localStorage.setItem('display.v1', JSON.stringify({ wrapMode: 'sideways', codeCols: 90 }))
    expect(loadDisplay()).toEqual({ wrapMode: 'auto', codeCols: 90 })
    localStorage.setItem('display.v1', '{not json')
    expect(loadDisplay()).toEqual({ wrapMode: 'auto', codeCols: 80 })
  })

  it('saves and survives a throwing storage', () => {
    saveDisplay({ wrapMode: 'scroll', codeCols: 100 })
    expect(loadDisplay()).toEqual({ wrapMode: 'scroll', codeCols: 100 })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota')
    })
    expect(() => saveDisplay({ wrapMode: 'wrap', codeCols: 60 })).not.toThrow()
  })

  it('applies css var and wrap class', () => {
    const el = document.createElement('div')
    applyDisplay({ wrapMode: 'wrap', codeCols: 72 }, el)
    expect(el.style.getPropertyValue('--code-cols')).toBe('72')
    expect(el.classList.contains('code-wrap')).toBe(true)
    applyDisplay({ wrapMode: 'scroll', codeCols: 80 }, el)
    expect(el.classList.contains('code-wrap')).toBe(false)
  })

  it('auto wraps only on narrow screens', () => {
    const el = document.createElement('div')
    const mq = (matches: boolean) =>
      vi.fn().mockReturnValue({ matches, addEventListener: vi.fn(), removeEventListener: vi.fn() })
    vi.stubGlobal('matchMedia', mq(true))
    applyDisplay({ wrapMode: 'auto', codeCols: 80 }, el)
    expect(el.classList.contains('code-wrap')).toBe(true)
    vi.stubGlobal('matchMedia', mq(false))
    applyDisplay({ wrapMode: 'auto', codeCols: 80 }, el)
    expect(el.classList.contains('code-wrap')).toBe(false)
    vi.unstubAllGlobals()
  })
})
