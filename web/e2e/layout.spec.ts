import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { columnCount, createCardViaApi, createDeckViaApi, noHorizontalOverflow, shot, smallTapTargets, uniqueName } from './helpers.ts'

const longLine = `std::vector<std::pair<std::string, int>> result = compute_something_quite_long(${'argument_'.repeat(12)}end);`
const code = ['#include <vector>', 'int main() {', `  ${longLine}`, ...Array.from({ length: 12 }, (_, n) => `  int v${n} = ${n};`), '}'].join('\n')

async function seed(request: APIRequestContext, project: string) {
  const deckId = await createDeckViaApi(request, uniqueName(project))
  const cardIds: number[] = []
  cardIds.push(await createCardViaApi(request, deckId, `Long code: what does \`main\` do?\n\n\`\`\`cpp\n${code}\n\`\`\``, 'It runs `main`.'))
  for (let i = 1; i < 4; i++) cardIds.push(await createCardViaApi(request, deckId, `Card ${i}: what is \`f${i}()\`?`, `It returns \`${i}\`.`))
  return { deckId, cardIds }
}

async function expectClean(page: Page) {
  expect(await noHorizontalOverflow(page)).toBe(true)
  expect(await smallTapTargets(page)).toEqual([])
}

/** Side by side: the preview sits right of the text box on the same row. Otherwise it is hidden. */
async function expectSplit(page: Page, sideBySide: boolean) {
  const front = page.getByLabel('Front', { exact: true })
  const preview = page.getByRole('region', { name: 'Front preview' })
  if (!sideBySide) {
    await expect(preview).toBeHidden()
    return
  }
  await expect(preview).toBeVisible()
  const [f, p] = await Promise.all([front.boundingBox(), preview.boundingBox()])
  expect(f).not.toBeNull()
  expect(p).not.toBeNull()
  expect(p!.x).toBeGreaterThanOrEqual(f!.x + f!.width)
  expect(Math.abs(p!.y - f!.y)).toBeLessThan(80)
}

test.describe('layout', () => {
  test('decks list', async ({ page, request }, testInfo) => {
    const { deckId } = await seed(request, testInfo.project.name)
    await page.goto('/')
    await expect(page.locator(`a[href="/decks/${deckId}/study"]`)).toBeVisible()
    await expectClean(page)
  })

  test('deck page', async ({ page, request }, testInfo) => {
    const { deckId } = await seed(request, testInfo.project.name)
    await page.goto(`/decks/${deckId}`)
    if (testInfo.project.name === 'desktop') {
      await expect(page.getByRole('list', { name: 'Card previews' }).getByRole('listitem')).toHaveCount(4)
      expect(await columnCount(page, '.card-tile')).toBe(3)
    } else {
      await expect(page.locator('.card-row')).toHaveCount(4)
      await expect(page.getByRole('list', { name: 'Card previews' })).toHaveCount(0)
    }
    await expectClean(page)
  })

  test('new card page', async ({ page, request }, testInfo) => {
    const { deckId } = await seed(request, testInfo.project.name)
    await page.goto(`/decks/${deckId}/cards/new`)
    const front = page.getByLabel('Front', { exact: true })
    await expect(front).toBeVisible()
    await front.fill(`Q\n\n\`\`\`cpp\n${code}\n\`\`\``)
    await expectSplit(page, testInfo.project.name === 'desktop')
    await expectClean(page)
  })

  test('existing card page: preview and edit tabs', async ({ page, request }, testInfo) => {
    const { cardIds } = await seed(request, testInfo.project.name)
    await page.goto(`/cards/${cardIds[0]}/edit`)
    await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('region', { name: 'Front preview' }).locator('.code-block')).toBeVisible()
    await expectClean(page)

    await page.getByRole('tab', { name: 'Edit' }).click()
    await expect(page.getByLabel('Front', { exact: true })).toBeVisible()
    await expectSplit(page, testInfo.project.name === 'desktop')
    await expectClean(page)
  })

  test('study page before and after Show answer', async ({ page, request }, testInfo) => {
    const { deckId } = await seed(request, testInfo.project.name)
    await page.goto(`/decks/${deckId}/study`)
    await expect(page.locator('.code-block').first()).toBeVisible()
    await expectClean(page)
    await page.getByRole('button', { name: 'Show answer' }).click()
    await expect(page.getByRole('button', { name: /^Good/ })).toBeVisible()
    await expectClean(page)
  })

  test('settings page', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByRole('radio', { name: 'Wrap' })).toBeVisible()
    await expectClean(page)
  })

  test('settings page Claude connector section (connector off)', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByRole('heading', { level: 2, name: 'Claude connector' })).toBeVisible()
    await expect(page.getByText('The Claude connector is off. See the VM runbook to turn it on.')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Recent batches' })).toHaveCount(0)
    await expectClean(page)
  })

  test('settings page version line', async ({ page }) => {
    await page.goto('/settings')
    const line = page.locator('p.version-line')
    await expect(line).toBeVisible()
    await expect(line).toHaveText(/^Version \S+ · Last backup/)
    await expectClean(page)
    const [box, vp] = [await line.boundingBox(), page.viewportSize()]
    expect(box).not.toBeNull()
    expect(box!.x + box!.width).toBeLessThanOrEqual(vp!.width)
  })

  test('help page', async ({ page }) => {
    await page.goto('/help')
    await expect(page.getByRole('heading', { name: 'Help', level: 1 })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Studying' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Editing cards' })).toBeVisible()
    await expect(page.locator('kbd').first()).toBeVisible()
    await expectClean(page)
  })

  test('header nav and sticky header', async ({ page, request }, testInfo) => {
    const { deckId } = await seed(request, testInfo.project.name)
    for (let i = 0; i < 25; i++) await createCardViaApi(request, deckId, `Filler ${i}`, 'x')
    await page.goto(`/decks/${deckId}`)
    const header = page.locator('header.app-header')
    await expect(header.getByRole('link', { name: 'Home' })).toBeVisible()
    const nav = header.getByRole('navigation', { name: 'Main' })
    for (const n of ['Decks', 'Settings', 'Help']) await expect(nav.getByRole('link', { name: n })).toBeVisible()
    await expect(nav.getByRole('button', { name: /^Switch to (dark|light) theme$/ })).toBeVisible()
    await expect(page.locator('.card-row, .card-tile').nth(20)).toBeAttached()
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(500)
    await expect.poll(async () => Math.round((await header.boundingBox())!.y)).toBe(0)
    await expect(header).toBeInViewport({ ratio: 1 })
  })

  test('header stays at the top on a scrolled settings page', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByRole('radio', { name: 'Wrap' })).toBeVisible()
    await page.setViewportSize({ width: page.viewportSize()!.width, height: 300 })
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(0)
    await expect(page.locator('header.app-header')).toBeInViewport({ ratio: 1 })
  })

  test('study card: front and back faces share one card, no keyboard hint, no rule', async ({ page, request }, testInfo) => {
    const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
    await createCardViaApi(request, deckId, 'Short', 'Brief')
    await page.goto(`/decks/${deckId}/study`)
    const card = page.locator('.study-card')
    const front = card.locator('.card-face').first()
    await expect(front).toBeVisible()
    expect((await front.boundingBox())!.height).toBeGreaterThanOrEqual(192)
    await expect(page.getByText(/Space shows the answer/)).toHaveCount(0)
    expect(await card.evaluate((el) => getComputedStyle(el, '::before').content)).toMatch(/^(none|normal)$/)
    await expect(card.locator('.card-face-back')).toHaveCount(0)
    await page.getByRole('button', { name: 'Show answer' }).click()
    const back = card.locator('.card-face.card-face-back')
    await expect(back).toBeVisible()
    expect((await back.boundingBox())!.height).toBeGreaterThanOrEqual(192)
    expect(await back.evaluate((el) => getComputedStyle(el).borderTopStyle)).toBe('dashed')
    await expectClean(page)
  })

  test('editor: preview faces and textarea share the same minimum height', async ({ page, request }, testInfo) => {
    const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
    const cardId = await createCardViaApi(request, deckId, 'Short', 'Brief')
    await page.goto(`/cards/${cardId}/edit`)
    const front = page.getByRole('region', { name: 'Front preview' })
    await expect(front).toBeVisible()
    expect((await front.boundingBox())!.height).toBeGreaterThanOrEqual(192)
    await expect(page.locator('.study-card.editor-preview .card-face')).toHaveCount(2)
    await page.getByRole('tab', { name: 'Edit' }).click()
    if (testInfo.project.name === 'desktop') {
      const [t, p] = await Promise.all([page.getByLabel('Front', { exact: true }).boundingBox(), front.boundingBox()])
      expect(t!.height).toBeGreaterThanOrEqual(192)
      expect(Math.abs(t!.height - p!.height)).toBeLessThanOrEqual(4)
    }
  })

  test('settings page does not shift while loading', async ({ page }, testInfo) => {
    type Rec = { top: number; left: number; retentionPresent: boolean }
    await page.addInitScript(() => {
      const w = window as unknown as { __display?: Rec }
      const check = () => {
        const h = document.getElementById('display-heading')
        if (!h || w.__display) return
        const r = h.getBoundingClientRect()
        w.__display = { top: r.top + window.scrollY, left: r.left, retentionPresent: document.getElementById('retention') !== null }
      }
      new MutationObserver(check).observe(document, { childList: true, subtree: true })
      const tick = () => {
        check()
        requestAnimationFrame(tick)
      }
      requestAnimationFrame(tick)
    })
    await page.route('**/api/settings', async (route) => {
      await new Promise((r) => setTimeout(r, 500))
      await route.continue()
    })
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible()
    await expect(page.getByRole('status')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Display' })).toHaveCount(0)
    await expect(page.getByLabel('Desired retention')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Display' })).toBeVisible()
    await expect(page.locator('p.version-line')).toBeVisible()
    const first = await page.evaluate(() => (window as unknown as { __display?: Rec }).__display)
    expect(first).toBeDefined()
    expect(first!.retentionPresent).toBe(true)
    const box = (await page.getByRole('heading', { name: 'Display' }).boundingBox())!
    const finalTop = box.y + (await page.evaluate(() => window.scrollY))
    expect(Math.abs(first!.top - finalTop)).toBeLessThanOrEqual(1)
    expect(Math.abs(first!.left - box.x)).toBeLessThanOrEqual(1)
    if (testInfo.project.name === 'iphone') {
      await page.emulateMedia({ colorScheme: 'dark' })
      await shot(page, 'settings-iphone-dark')
    }
  })

  test('deck row: click anywhere opens the deck, Study opens study', async ({ page, request }, testInfo) => {
    const names = [uniqueName(testInfo.project.name), uniqueName(testInfo.project.name), uniqueName(testInfo.project.name)]
    const ids: number[] = []
    for (const n of names) {
      const id = await createDeckViaApi(request, n)
      await createCardViaApi(request, id, 'q', 'a')
      ids.push(id)
    }
    const id = ids[1]!
    const name = names[1]!
    const row = page.locator('li.deck-row').filter({ hasText: name })
    const study = row.getByRole('link', { name: `Study ${name}` })
    const open = async () => {
      await page.goto('/')
      await expect(row).toBeVisible()
      await row.scrollIntoViewIfNeeded()
    }
    await open()
    if (testInfo.project.name === 'desktop') {
      await row.hover()
      await shot(page, 'decks-desktop-light-hover')
    }
    const [r, s, n] = [(await row.boundingBox())!, (await study.boundingBox())!, (await row.getByRole('link', { name, exact: true }).boundingBox())!]
    expect(s.x).toBeGreaterThan(n.x + n.width)
    // Empty area just left of the Study button, vertically centred.
    await page.mouse.click(s.x - 12, r.y + r.height / 2)
    await expect(page).toHaveURL(new RegExp(`/decks/${id}$`))
    // Bottom-left corner of the row, below the name text.
    await open()
    const r2 = (await row.boundingBox())!
    await page.mouse.click(r2.x + 4, r2.y + r2.height - 4)
    await expect(page).toHaveURL(new RegExp(`/decks/${id}$`))
    await open()
    await study.click()
    await expect(page).toHaveURL(new RegExp(`/decks/${id}/study$`))
  })

  test('two-sided: new deck form, editor checkbox, deck settings and its dialogs', async ({ page, request }, testInfo) => {
    const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name), { twoSided: true })
    const cardId = await createCardViaApi(request, deckId, 'Q', 'A')
    const desktop = testInfo.project.name === 'desktop'

    await page.goto('/')
    const nameInput = page.getByLabel('New deck name')
    const check = page.getByRole('checkbox', { name: 'Two-sided' })
    await expect(check).toBeVisible()
    await expectClean(page)
    if (desktop) {
      const [n, c] = [(await nameInput.boundingBox())!, (await check.locator('xpath=ancestor::label').boundingBox())!]
      expect(c.x, 'checkbox sits beside the name input').toBeGreaterThan(n.x + n.width - 1)
    }

    await page.goto(`/decks/${deckId}/cards/new`)
    const box = page.getByRole('checkbox', { name: /^Two-sided/ })
    await expect(box).toBeChecked()
    const add = (await page.getByRole('button', { name: 'Add card' }).boundingBox())!
    const label = (await box.locator('xpath=ancestor::label').boundingBox())!
    expect(label.y + label.height, 'checkbox is above the Add row').toBeLessThanOrEqual(add.y + 1)
    await expectClean(page)

    await page.goto(`/cards/${cardId}/edit`)
    await page.getByRole('tab', { name: 'Edit' }).click()
    await expectSplit(page, desktop)
    await page.getByRole('checkbox', { name: /^Two-sided/ }).uncheck()
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(page.getByRole('dialog', { name: 'Remove the reverse side?' })).toBeVisible()
    await expectClean(page)
    await page.getByRole('button', { name: 'Cancel' }).click()

    await page.goto(`/decks/${deckId}`)
    await expect(page.getByRole('checkbox', { name: 'Two-sided by default' })).toBeChecked()
    await expectClean(page)
    await page.getByRole('checkbox', { name: 'Two-sided by default' }).uncheck()
    await page.getByRole('button', { name: 'Save deck settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Turn off two-sided by default?' })
    await expect(dialog.getByRole('button')).toHaveCount(3)
    await expectClean(page)
    const vp = page.viewportSize()!
    for (const b of await dialog.getByRole('button').all()) {
      const r = (await b.boundingBox())!
      expect(r.x).toBeGreaterThanOrEqual(0)
      expect(r.x + r.width).toBeLessThanOrEqual(vp.width)
    }
  })

  test('editor edit tab: preview card shares a grid with the text boxes', async ({ page, request }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop', 'side-by-side editor is desktop only')
    const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
    const shortId = await createCardViaApi(request, deckId, 'Short', 'Brief')
    const longId = await createCardViaApi(request, deckId, 'Short', Array.from({ length: 25 }, (_, i) => `line ${i}`).join('\n\n'))
    const box = async (sel: string) => (await page.locator(sel).boundingBox())!
    const bottom = (b: { y: number; height: number }) => b.y + b.height
    for (const c of [
      { id: shortId, scheme: 'light', file: 'editor-1440-short-light' },
      { id: longId, scheme: 'dark', file: 'editor-1440-longback-dark' },
    ] as const) {
      await page.emulateMedia({ colorScheme: c.scheme })
      await page.goto(`/cards/${c.id}/edit`)
      await page.getByRole('tab', { name: 'Edit' }).click()
      await expect(page.getByLabel('Front', { exact: true })).toBeVisible()
      await expect(page.getByRole('region', { name: 'Back preview' })).toBeVisible()
      const [card, front, back, backFace, frontFace] = [
        await box('.editor-preview'),
        await box('#card-front'),
        await box('#card-back'),
        await box('.editor-preview .card-face-back'),
        await box('.editor-preview .card-face:not(.card-face-back)'),
      ]
      expect(Math.abs(card.y - front.y), 'card top == Front textarea top').toBeLessThanOrEqual(1)
      expect(Math.abs(backFace.y - bottom(front)), 'back face top == Front textarea bottom').toBeLessThanOrEqual(1)
      expect(Math.abs(bottom(backFace) - bottom(back)), 'back face bottom == Back textarea bottom').toBeLessThanOrEqual(1)
      expect(Math.abs(bottom(card) - bottom(back)), 'card bottom == Back textarea bottom').toBeLessThanOrEqual(1)
      expect(Math.abs(bottom(frontFace) - backFace.y), 'front face meets back face').toBeLessThanOrEqual(1)
      // The Back label stays just above its box; extra preview height goes to the box, not the label row.
      const backLabel = await box('label[for="card-back"]')
      expect(back.y - bottom(backLabel), 'Back label sits directly above the Back textarea').toBeLessThanOrEqual(12)
      if (c.id === longId) expect(backFace.height, 'long back is taller than the front textarea').toBeGreaterThan(front.height)
      await shot(page, c.file)
    }
  })
})
