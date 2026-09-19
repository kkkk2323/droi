import { expect, test } from './fixtures'

test('the Client renders the Droi title with the Geist font', async ({ page, openClient }) => {
  await openClient()
  const heading = page.getByRole('heading', { name: 'Droi' })
  await expect(heading).toBeVisible()
  const font = await heading.evaluate((el) => getComputedStyle(el).fontFamily)
  expect(font).toMatch(/Geist/)
})
