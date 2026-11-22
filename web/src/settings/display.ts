// Per-device display settings (spec §8.1), kept in localStorage.

export type WrapMode = 'auto' | 'scroll' | 'wrap'

export interface DisplaySettings {
  wrapMode: WrapMode
  codeCols: number
}

export const DISPLAY_KEY = 'display.v1'
export const MIN_COLS = 40
export const MAX_COLS = 200
export const DEFAULT_DISPLAY: DisplaySettings = { wrapMode: 'auto', codeCols: 80 }
/** In auto mode, code wraps below this viewport width. */
export const NARROW_QUERY = '(max-width: 599px)'

const WRAP_MODES: readonly WrapMode[] = ['auto', 'scroll', 'wrap']

export function clampCols(v: unknown): number {
  if (typeof v !== 'number' || !Number.isFinite(v)) return DEFAULT_DISPLAY.codeCols
  return Math.min(MAX_COLS, Math.max(MIN_COLS, Math.round(v)))
}

function normalise(v: unknown): DisplaySettings {
  if (typeof v !== 'object' || v === null) return { ...DEFAULT_DISPLAY }
  const o = v as Record<string, unknown>
  const wrapMode = WRAP_MODES.find((m) => m === o.wrapMode) ?? DEFAULT_DISPLAY.wrapMode
  return { wrapMode, codeCols: clampCols(o.codeCols) }
}

export function loadDisplay(): DisplaySettings {
  try {
    const raw = localStorage.getItem(DISPLAY_KEY)
    return raw === null ? { ...DEFAULT_DISPLAY } : normalise(JSON.parse(raw))
  } catch {
    return { ...DEFAULT_DISPLAY }
  }
}

export function saveDisplay(s: DisplaySettings): void {
  try {
    localStorage.setItem(DISPLAY_KEY, JSON.stringify(normalise(s)))
  } catch {
    // Storage unavailable (private mode, quota): the setting lasts for this page only.
  }
}

let stopWatching: (() => void) | null = null

function narrowQuery(): MediaQueryList | null {
  return typeof window.matchMedia === 'function' ? window.matchMedia(NARROW_QUERY) : null
}

/**
 * Apply settings to the document: --code-cols as a CSS custom property
 * (allowed by the CSP) and the code-wrap class. In auto mode the class
 * follows the viewport width as it changes.
 */
export function applyDisplay(s: DisplaySettings, root: HTMLElement = document.documentElement): void {
  stopWatching?.()
  stopWatching = null
  root.style.setProperty('--code-cols', String(clampCols(s.codeCols)))
  if (s.wrapMode !== 'auto') {
    root.classList.toggle('code-wrap', s.wrapMode === 'wrap')
    return
  }
  const mq = narrowQuery()
  root.classList.toggle('code-wrap', mq?.matches ?? false)
  if (mq) {
    const onChange = (e: MediaQueryListEvent) => root.classList.toggle('code-wrap', e.matches)
    mq.addEventListener('change', onChange)
    stopWatching = () => mq.removeEventListener('change', onChange)
  }
}
