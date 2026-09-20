import { expect, test } from './fixtures'

test('the Client renders the sidebar with the Geist font', async ({ openClient, openSidebar }) => {
  await openClient()
  const row = (await openSidebar()).getByRole('button', { name: 'New session' })
  await expect(row).toBeVisible()
  const font = await row.evaluate((el) => getComputedStyle(el).fontFamily)
  expect(font).toMatch(/Geist/)
})

test('the sidebar collapses from either edge and the choice survives a reload', async ({
  page,
  openClient,
}, testInfo) => {
  test.skip(testInfo.project.name === 'phone', 'a phone has a drawer, not a collapsible sidebar')
  await openClient()
  const sidebar = page.getByRole('navigation', { name: 'Sessions' })
  await expect(sidebar).toBeVisible()

  await page.getByRole('button', { name: 'Hide sidebar' }).click()
  await expect(sidebar).toBeHidden()
  const show = page.getByRole('button', { name: 'Show sidebar' })
  await expect(show).toHaveAttribute('aria-expanded', 'false')

  await page.reload()
  await expect(page.getByRole('navigation', { name: 'Sessions' })).toBeHidden()

  await page.getByRole('button', { name: 'Show sidebar' }).click()
  await expect(page.getByRole('navigation', { name: 'Sessions' })).toBeVisible()

  // ⌘B / Ctrl+B toggles it too.
  await page.keyboard.press('ControlOrMeta+b')
  await expect(page.getByRole('navigation', { name: 'Sessions' })).toBeHidden()
  await page.keyboard.press('ControlOrMeta+b')
  await expect(page.getByRole('navigation', { name: 'Sessions' })).toBeVisible()
})
