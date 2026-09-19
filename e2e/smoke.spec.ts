import { expect, test } from '@playwright/test'

test('the Client renders the Droi title', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Droi' })).toBeVisible()
})
