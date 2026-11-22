// Per-device light/dark choice, kept in localStorage. No stored choice means
// "follow the system" (prefers-color-scheme), which the CSS handles itself.

export type Theme = 'light' | 'dark'

export const THEME_KEY = 'theme.v1'
const DARK_QUERY = '(prefers-color-scheme: dark)'

export function loadTheme(): Theme | null {
  try {
    const v = localStorage.getItem(THEME_KEY)
    return v === 'light' || v === 'dark' ? v : null
  } catch {
    return null
  }
}

export function saveTheme(t: Theme): void {
  try {
    localStorage.setItem(THEME_KEY, t)
  } catch {
    // Storage unavailable: the choice lasts for this page only.
  }
}

function systemTheme(): Theme {
  return typeof window.matchMedia === 'function' && window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light'
}

/** The theme on screen: the stored choice, else the system's. */
export function currentTheme(): Theme {
  return loadTheme() ?? systemTheme()
}

/** Set data-theme on <html> (read by color-scheme in base.css); null follows the system. */
export function applyTheme(t: Theme | null, root: HTMLElement = document.documentElement): void {
  if (t === null) root.removeAttribute('data-theme')
  else root.dataset.theme = t
}
