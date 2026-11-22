import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { columnCount, createCardViaApi, createDeckViaApi, noHorizontalOverflow, uniqueName } from './helpers.ts'

test.skip(({ isMobile }) => isMobile, 'breakpoints run on the desktop project only')

const HEIGHT = 900

async function seed(request: APIRequestContext) {
  const deckId = await createDeckViaApi(request, uniqueName('bp'))
  const cardIds: number[] = []
  for (let i = 0; i < 4; i++) {
    cardIds.push(await createCardViaApi(request, deckId, `Card ${i}: what is \`f${i}()\`?\n\n\`\`\`cpp\nint v = ${i};\n\`\`\``, `It returns \`${i}\`.`))
  }
  return { deckId, cardId: cardIds[0]! }
}

async function expectDeckLayout(page: Page, mode: 'rows' | 'cols2' | 'cols3') {
  if (mode === 'rows') {
    await expect(page.locator('.card-row')).toHaveCount(4)
    await expect(page.getByRole('list', { name: 'Card previews' })).toHaveCount(0)
  } else {
    await expect(page.getByRole('list', { name: 'Card previews' }).getByRole('listitem')).toHaveCount(4)
    await expect(page.locator('.card-row')).toHaveCount(0)
    await expect.poll(() => columnCount(page, '.card-tile')).toBe(mode === 'cols2' ? 2 : 3)
  }
  expect(await noHorizontalOverflow(page)).toBe(true)
}

async function expectEditor(page: Page, sideBySide: boolean) {
  const preview = page.getByRole('region', { name: 'Front preview' })
  if (sideBySide) {
    await expect(preview).toBeVisible()
    const [f, p] = await Promise.all([page.getByLabel('Front', { exact: true }).boundingBox(), preview.boundingBox()])
    expect(p!.x).toBeGreaterThanOrEqual(f!.x + f!.width)
  } else {
    await expect(preview).toBeHidden()
  }
  expect(await noHorizontalOverflow(page)).toBe(true)
}

async function openEdit(page: Page, cardId: number) {
  await page.goto(`/cards/${cardId}/edit`)
  await page.getByRole('tab', { name: 'Edit' }).click()
  await expect(page.getByLabel('Front', { exact: true })).toBeVisible()
}

const wrapClass = (page: Page, on: boolean) =>
  on ? expect(page.locator('html')).toHaveClass(/code-wrap/) : expect(page.locator('html')).not.toHaveClass(/code-wrap/)

const deckCases = [
  { width: 599, mode: 'rows', wrap: true },
  { width: 600, mode: 'cols2', wrap: false },
  { width: 852, mode: 'cols2', wrap: false },
  { width: 899, mode: 'cols2', wrap: false },
  { width: 900, mode: 'cols3', wrap: false },
] as const

for (const c of deckCases) {
  test(`deck page at ${c.width}px: ${c.mode}, code-wrap ${c.wrap ? 'on' : 'off'}`, async ({ page, request }) => {
    const { deckId } = await seed(request)
    await page.setViewportSize({ width: c.width, height: HEIGHT })
    await page.goto(`/decks/${deckId}`)
    await expectDeckLayout(page, c.mode)
    await wrapClass(page, c.wrap)
  })
}

for (const c of [
  { width: 599, side: false },
  { width: 852, side: false },
  { width: 899, side: false },
  { width: 900, side: true },
]) {
  test(`card editor at ${c.width}px: ${c.side ? 'side by side' : 'single column'}`, async ({ page, request }) => {
    const { cardId } = await seed(request)
    await page.setViewportSize({ width: c.width, height: HEIGHT })
    await openEdit(page, cardId)
    await expectEditor(page, c.side)
  })
}

test('live resize across 600px updates the deck grid and code wrap', async ({ page, request }) => {
  const { deckId } = await seed(request)
  await page.setViewportSize({ width: 700, height: HEIGHT })
  await page.goto(`/decks/${deckId}`)
  await expectDeckLayout(page, 'cols2')
  await wrapClass(page, false)

  await page.setViewportSize({ width: 599, height: HEIGHT })
  await expectDeckLayout(page, 'rows')
  await wrapClass(page, true)

  await page.setViewportSize({ width: 600, height: HEIGHT })
  await expectDeckLayout(page, 'cols2')
  await wrapClass(page, false)
})

test('live resize across 900px updates the deck grid and the editor', async ({ page, request }) => {
  const { deckId, cardId } = await seed(request)
  await page.setViewportSize({ width: 899, height: HEIGHT })
  await page.goto(`/decks/${deckId}`)
  await expectDeckLayout(page, 'cols2')
  await page.setViewportSize({ width: 900, height: HEIGHT })
  await expectDeckLayout(page, 'cols3')
  await page.setViewportSize({ width: 899, height: HEIGHT })
  await expectDeckLayout(page, 'cols2')

  await openEdit(page, cardId)
  await expectEditor(page, false)
  await page.setViewportSize({ width: 900, height: HEIGHT })
  await expectEditor(page, true)
  await page.setViewportSize({ width: 899, height: HEIGHT })
  await expectEditor(page, false)
})
