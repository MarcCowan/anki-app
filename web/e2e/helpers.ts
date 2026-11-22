// Shared Playwright helpers. Specs seed data through the API (fast, no UI
// coupling) and assert on the UI.
import { expect, type APIRequestContext, type Page } from '@playwright/test'

let seq = 0

/** A deck name unique across workers and runs. */
export function uniqueName(project: string): string {
  return `E2E ${project} ${Date.now()}-${seq++}`
}

export async function createDeckViaApi(request: APIRequestContext, name: string, opts: { twoSided?: boolean } = {}): Promise<number> {
  const res = await request.post('/api/decks', { data: { name, ...opts } })
  expect(res.status()).toBe(201)
  return ((await res.json()) as { id: number }).id
}

export async function createCardViaApi(
  request: APIRequestContext,
  deckId: number,
  front: string,
  back: string,
  opts: { twoSided?: boolean } = {},
): Promise<number> {
  const res = await request.post(`/api/decks/${deckId}/cards`, { data: { front, back, ...opts } })
  expect(res.status()).toBe(201)
  return ((await res.json()) as { id: number }).id
}

/** True when the page itself does not scroll sideways. */
export async function noHorizontalOverflow(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)
}

/**
 * Visible buttons, links styled as buttons, tabs, radios' labels and text inputs
 * that are shorter than `min` px (spec: tap targets of at least 44px).
 * Returns a description of each offender so a failure says what to fix.
 */
export async function smallTapTargets(page: Page, min = 44): Promise<string[]> {
  return page.evaluate((min) => {
    const sel = 'button, a.button, [role="tab"], input:not([type="hidden"]), select, textarea'
    const out: string[] = []
    for (const el of Array.from(document.querySelectorAll<HTMLElement>(sel))) {
      const r = el.getBoundingClientRect()
      if (r.width === 0 || r.height === 0) continue
      if (el.closest('[hidden], [aria-hidden="true"]')) continue
      // Radios and checkboxes are tapped through their label.
      const target = el instanceof HTMLInputElement && (el.type === 'radio' || el.type === 'checkbox') ? el.closest('label') ?? el : el
      const h = target.getBoundingClientRect().height
      if (h < min) out.push(`${target.tagName.toLowerCase()} "${(target.textContent ?? '').trim().slice(0, 40) || el.getAttribute('aria-label') || el.id}" is ${Math.round(h)}px`)
    }
    return out
  }, min)
}

/** Number of distinct column positions among the elements (e.g. grid tiles). */
export async function columnCount(page: Page, selector: string): Promise<number> {
  const lefts = await page.locator(selector).evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().left)))
  return new Set(lefts).size
}

/** Saves a screenshot when E2E_SHOTS_DIR is set (manual review aid); otherwise does nothing. */
export async function shot(page: Page, name: string): Promise<void> {
  const dir = process.env.E2E_SHOTS_DIR
  if (dir) await page.screenshot({ path: `${dir}/${name}.png` })
}
