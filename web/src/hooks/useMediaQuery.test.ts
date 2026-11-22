import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useMediaQuery } from './useMediaQuery'

afterEach(() => {
  vi.unstubAllGlobals()
})

function fakeMatchMedia(initial: boolean) {
  const listeners = new Set<() => void>()
  const mql = {
    matches: initial,
    addEventListener: (_: string, l: () => void) => listeners.add(l),
    removeEventListener: (_: string, l: () => void) => listeners.delete(l),
  }
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue(mql))
  return {
    set(v: boolean) {
      mql.matches = v
      listeners.forEach((l) => l())
    },
    listeners,
  }
}

describe('useMediaQuery', () => {
  it('is false when matchMedia is unavailable', () => {
    vi.stubGlobal('matchMedia', undefined)
    const { result } = renderHook(() => useMediaQuery('(min-width: 900px)'))
    expect(result.current).toBe(false)
  })

  it('follows the query and unsubscribes on unmount', () => {
    const mq = fakeMatchMedia(false)
    const { result, unmount } = renderHook(() => useMediaQuery('(min-width: 900px)'))
    expect(result.current).toBe(false)
    act(() => mq.set(true))
    expect(result.current).toBe(true)
    unmount()
    expect(mq.listeners.size).toBe(0)
  })
})
