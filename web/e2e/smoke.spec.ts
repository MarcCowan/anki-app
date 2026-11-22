import { expect, test } from '@playwright/test'
import { createCardViaApi, createDeckViaApi, noHorizontalOverflow, uniqueName } from './helpers.ts'

test('create deck, add code card, study, rate good', async ({ page }, testInfo) => {
  const name = uniqueName(testInfo.project.name)
  await page.goto('/')
  await page.getByLabel('New deck name').fill(name)
  await page.getByRole('button', { name: 'Create deck' }).click()
  await page.getByRole('link', { name, exact: true }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  await page.getByRole('link', { name: 'Add card' }).click()
  await page.getByLabel('Front', { exact: true }).fill('What is `x`?\n\n```cpp\nint x = 0;\n```')
  await page.getByLabel('Back', { exact: true }).fill('zero')
  await page.getByRole('button', { name: 'Add card' }).click()
  await expect(page.getByText('Card added')).toBeVisible()

  await page.getByRole('link', { name: 'Back to deck' }).click()
  await page.getByRole('link', { name: 'Study', exact: true }).click()
  await expect(page.locator('.code-block .line').first()).toBeVisible()
  await expect(page.locator('.code-block .hljs-type, .code-block .hljs-keyword').first()).toBeVisible()
  await page.getByRole('button', { name: 'Show answer' }).click()
  await expect(page.getByText('zero')).toBeVisible()
  await page.getByRole('button', { name: /^Good/ }).click()

  // Good on a new card schedules it minutes ahead, inside the 20-minute
  // learn-ahead window (spec §5.1), so it comes straight back.
  await expect(page.getByRole('button', { name: 'Show answer' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Undo' })).toBeVisible()
  await page.getByRole('button', { name: 'Show answer' }).click()
  await page.getByRole('button', { name: /^Easy/ }).click()
  await expect(page.getByText('Done for now')).toBeVisible()
  await expect(page.getByText(/^Next card due/)).toBeVisible()
})

test('no horizontal page overflow in wrap mode', async ({ page, request }, testInfo) => {
  await page.goto('/settings')
  await page.getByRole('radio', { name: 'Wrap' }).check()

  const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
  const longToken = 'std::' + 'a'.repeat(195)
  const longUrl = 'https://example.com/' + 'b'.repeat(180)
  await createCardViaApi(
    request,
    deckId,
    `A long token in prose: ${'c'.repeat(200)} and ${longUrl}\n\n\`\`\`cpp\nauto v = ${longToken};\n    int indented = 1; // ${'d'.repeat(150)}\n\`\`\``,
    `\`${longToken}\``,
  )

  await page.goto(`/decks/${deckId}/study`)
  await expect(page.locator('.code-block')).toBeVisible()
  await page.getByRole('button', { name: 'Show answer' }).click()
  await expect(page.locator('html')).toHaveClass(/code-wrap/)
  expect(await noHorizontalOverflow(page)).toBe(true)

  // The code block itself wraps rather than scrolling.
  const block = page.locator('.code-block').first()
  expect(await block.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
})

test('no CSP violations', async ({ page, request }, testInfo) => {
  const violations: string[] = []
  page.on('console', (m) => {
    const text = m.text()
    if (m.type() === 'error' && /Content Security Policy|Refused to/i.test(text)) violations.push(text)
  })
  page.on('pageerror', (e) => violations.push(e.message))

  const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
  await createCardViaApi(request, deckId, 'Q with `code`\n\n```cpp\nint main() { return 0; }\n```', 'A')
  const cards = (await (await request.get(`/api/decks/${deckId}/cards`)).json()) as { id: number }[]

  for (const path of ['/', '/settings', `/decks/${deckId}`, `/decks/${deckId}/cards/new`, `/cards/${cards[0]!.id}/edit`]) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
  }
  await page.goto(`/decks/${deckId}/study`)
  await page.getByRole('button', { name: 'Show answer' }).click()
  await page.waitForLoadState('networkidle')
  expect(violations).toEqual([])
})

test('deck view previews cards; a card opens in preview', async ({ page, request }, testInfo) => {
  const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
  for (let i = 0; i < 4; i++) {
    const code = Array.from({ length: 30 }, (_, n) => `int v${n} = ${n};`).join('\n')
    await createCardViaApi(request, deckId, `Card ${i}: what does \`f${i}()\` do?\n\n\`\`\`cpp\n${code}\n\`\`\``, `It returns \`${i}\`.`)
  }

  await page.goto(`/decks/${deckId}`)
  const wide = testInfo.project.name === 'desktop'
  if (wide) {
    const tiles = page.getByRole('list', { name: 'Card previews' }).getByRole('listitem')
    await expect(tiles).toHaveCount(4)
    const boxes = await tiles.evaluateAll((els) => els.map((e) => e.getBoundingClientRect()))
    // Three equal columns of equal-height tiles, the fourth wrapping to a new row.
    expect(new Set(boxes.map((b) => Math.round(b.width))).size).toBe(1)
    expect(new Set(boxes.map((b) => Math.round(b.height))).size).toBe(1)
    expect(boxes[0]!.top).toBe(boxes[2]!.top)
    expect(boxes[3]!.top).toBeGreaterThan(boxes[0]!.bottom)
    await expect(tiles.first().locator('.card-tile-front .code-block')).toBeVisible()
    await expect(tiles.first().locator('.card-tile-back code')).toHaveText('0')
  } else {
    await expect(page.getByRole('list', { name: 'Card previews' })).toHaveCount(0)
    const row = page.locator('.card-row').first()
    await expect(row.locator('code')).toHaveText('f0()')
    await expect(row.locator('.code-block')).toHaveCount(0)
  }
  expect(await noHorizontalOverflow(page)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('deck.png'), fullPage: true })

  await page.getByRole('link', { name: 'Card 0: what does `f0()` do?' }).click()
  await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('region', { name: 'Front preview' }).locator('.code-block')).toBeVisible()
  await expect(page.getByLabel('Front', { exact: true })).toBeHidden()
  await page.getByRole('tab', { name: 'Edit' }).click()
  await expect(page.getByLabel('Front', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Front preview' })).toBeVisible({ visible: wide })
  await page.screenshot({ path: testInfo.outputPath('card-edit.png'), fullPage: true })
})

test('theme toggle persists across reload and overrides the system scheme', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto('/')
  const html = page.locator('html')
  const bg = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  const lightBg = await bg()

  await page.getByRole('button', { name: 'Switch to dark theme' }).click()
  await expect(html).toHaveAttribute('data-theme', 'dark')
  expect(await page.evaluate(() => localStorage.getItem('theme.v1'))).toBe('dark')
  const darkBg = await bg()
  expect(darkBg).not.toBe(lightBg)

  await page.reload()
  await expect(html).toHaveAttribute('data-theme', 'dark')
  expect(await bg()).toBe(darkBg)
  await page.getByRole('button', { name: 'Switch to light theme' }).click()
  await expect(html).toHaveAttribute('data-theme', 'light')
  expect(await bg()).toBe(lightBg)
})

test('with no stored theme the page follows prefers-color-scheme', async ({ page }) => {
  const bg = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Switch to dark theme' })).toBeVisible()
  const lightBg = await bg()
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.reload()
  await expect(page.getByRole('button', { name: 'Switch to light theme' })).toBeVisible()
  expect(await bg()).not.toBe(lightBg)
  expect(await page.evaluate(() => localStorage.getItem('theme.v1'))).toBeNull()
})

test('saving a card edit stays on the page and shows a Saved note that goes away', async ({ page, request }, testInfo) => {
  const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
  const cardId = await createCardViaApi(request, deckId, 'Old front', 'Old back')
  await page.goto(`/cards/${cardId}/edit`)
  await page.getByRole('tab', { name: 'Edit' }).click()
  await page.getByLabel('Front', { exact: true }).fill('New front')
  await page.getByRole('button', { name: 'Save changes' }).click()
  const saved = page.getByRole('status').filter({ hasText: 'Saved' })
  await expect(saved).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`/cards/${cardId}/edit$`))
  await expect(page.getByLabel('Front', { exact: true })).toHaveValue('New front')
  await expect(saved).toBeHidden() // gone by itself after ~2.5s
  await expect(page.getByText('Ctrl+Enter saves')).toHaveCount(0)
})

test('decks page shows Loading, never "No decks yet", while the API is slow', async ({ page, request }, testInfo) => {
  await createDeckViaApi(request, uniqueName(testInfo.project.name))
  await page.addInitScript(() => {
    const w = window as unknown as { __sawEmpty: boolean }
    w.__sawEmpty = false
    new MutationObserver(() => {
      if (document.body?.textContent?.includes('No decks yet')) w.__sawEmpty = true
    }).observe(document, { childList: true, subtree: true, characterData: true })
  })
  let release: () => void = () => {}
  const gate = new Promise<void>((resolve) => (release = resolve))
  await page.route('**/api/decks', async (route) => {
    await gate
    await route.continue()
  })
  await page.goto('/')
  await expect(page.getByRole('status', { name: 'Loading' })).toBeAttached()
  release()
  await expect(page.getByRole('status', { name: 'Loading' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: /^E2E / }).first()).toBeVisible()
  expect(await page.evaluate(() => (window as unknown as { __sawEmpty: boolean }).__sawEmpty)).toBe(false)
})

test('two-sided deck: card editor starts checked; reverse is buried after the forward review', async ({ page }, testInfo) => {
  const name = uniqueName(testInfo.project.name)
  await page.goto('/')
  await page.getByLabel('New deck name').fill(name)
  await page.getByRole('checkbox', { name: 'Two-sided' }).check()
  await page.getByRole('button', { name: 'Create deck' }).click()
  await page.getByRole('link', { name, exact: true }).click()

  await page.getByRole('link', { name: 'Add card' }).click()
  await expect(page.getByRole('checkbox', { name: /^Two-sided/ })).toBeChecked()
  await page.getByLabel('Front', { exact: true }).fill('Forward question')
  await page.getByLabel('Back', { exact: true }).fill('Forward answer')
  await page.getByRole('button', { name: 'Add card' }).click()
  await expect(page.getByText('Card added')).toBeVisible()

  await page.getByRole('link', { name: 'Back to deck' }).click()
  await expect(page.getByRole('checkbox', { name: 'Two-sided by default' })).toBeChecked()
  await page.getByRole('link', { name: 'Study', exact: true }).click()
  await expect(page.getByText('Forward question')).toBeVisible()
  await expect(page.getByText('Reverse: back')).toHaveCount(0)
  await page.getByRole('button', { name: 'Show answer' }).click()
  await page.getByRole('button', { name: /^Easy/ }).click()
  // Easy schedules days ahead and the reverse is buried for today (sibling burying).
  await expect(page.getByText('Done for now')).toBeVisible()
  await expect(page.getByText('Reverse: back')).toHaveCount(0)
})

test('turning a saved card one-sided asks first; Cancel keeps the reverse', async ({ page, request }, testInfo) => {
  const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
  const cardId = await createCardViaApi(request, deckId, 'Q', 'A', { twoSided: true })
  const twoSidedNow = async () =>
    ((await (await request.get(`/api/decks/${deckId}/cards`)).json()) as { id: number; twoSided: boolean }[]).find((c) => c.id === cardId)!.twoSided

  await page.goto(`/cards/${cardId}/edit`)
  await page.getByRole('tab', { name: 'Edit' }).click()
  const box = page.getByRole('checkbox', { name: /^Two-sided/ })
  await expect(box).toBeChecked()
  await box.uncheck()
  await page.getByRole('button', { name: 'Save changes' }).click()
  const dialog = page.getByRole('dialog', { name: 'Remove the reverse side?' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  expect(await twoSidedNow()).toBe(true)

  await page.getByRole('button', { name: 'Save changes' }).click()
  await page.getByRole('dialog', { name: 'Remove the reverse side?' }).getByRole('button', { name: 'Remove reverse side' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible()
  expect(await twoSidedNow()).toBe(false)
})

test('deck settings: two-sided by default asks before changing every card', async ({ page, request }, testInfo) => {
  const deckId = await createDeckViaApi(request, uniqueName(testInfo.project.name))
  await createCardViaApi(request, deckId, 'Q', 'A')
  const cardsNow = async () => (await (await request.get(`/api/decks/${deckId}/cards`)).json()) as { twoSided: boolean }[]
  await page.goto(`/decks/${deckId}`)
  await expect(page.getByText(/A daily target, not a limit\. New today: \d+ of \d+\./)).toBeVisible()
  await page.getByRole('checkbox', { name: 'Two-sided by default' }).check()
  await page.getByRole('button', { name: 'Save deck settings' }).click()
  await page.getByRole('dialog', { name: 'Make every card two-sided?' }).getByRole('button', { name: 'Make all two-sided' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible()
  expect((await cardsNow()).map((c) => c.twoSided)).toEqual([true])

  await page.getByRole('checkbox', { name: 'Two-sided by default' }).uncheck()
  await page.getByRole('button', { name: 'Save deck settings' }).click()
  const off = page.getByRole('dialog', { name: 'Turn off two-sided by default?' })
  await off.getByRole('button', { name: 'Keep reverse sides' }).click()
  await expect(off).toBeHidden()
  expect((await cardsNow()).map((c) => c.twoSided)).toEqual([true])
})
