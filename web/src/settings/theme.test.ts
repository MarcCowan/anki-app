import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyTheme, currentTheme, loadTheme, saveTheme, THEME_KEY } from './theme'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
})

function stubSystemDark(dark: boolean) {
  vi.stubGlobal('matchMedia', vi.fn((q: string) => ({ matches: dark && q === '(prefers-color-scheme: dark)' })))
}

describe('theme', () => {
  it('has no stored choice by default, or when storage throws or holds junk', () => {
    expect(loadTheme()).toBeNull()
    localStorage.setItem(THEME_KEY, 'purple')
    expect(loadTheme()).toBeNull()
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(loadTheme()).toBeNull()
  })

  it('saves a choice and survives a throwing storage', () => {
    saveTheme('dark')
    expect(loadTheme()).toBe('dark')
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota')
    })
    expect(() => saveTheme('light')).not.toThrow()
  })

  it('current theme is the stored choice, else the system preference', () => {
    stubSystemDark(true)
    expect(currentTheme()).toBe('dark')
    stubSystemDark(false)
    expect(currentTheme()).toBe('light')
    saveTheme('dark')
    expect(currentTheme()).toBe('dark')
  })

  it('applies a choice as data-theme, and none follows the system', () => {
    const el = document.createElement('html')
    applyTheme('dark', el)
    expect(el.dataset.theme).toBe('dark')
    applyTheme(null, el)
    expect(el.hasAttribute('data-theme')).toBe(false)
  })
})
