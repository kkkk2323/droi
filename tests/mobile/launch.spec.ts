import { expect, presetStandIns, test } from './fixtures'

test('first launch shows the pairing screen', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Pair with a computer' })).toBeVisible()
})

test('settings show the version and the days left on the signature', async ({ page }) => {
  const inThreeDays = new Date(Date.now() + 2.5 * 86_400_000).toISOString()
  await presetStandIns(page, { signatureExpiry: inThreeDays })
  await page.goto('/')
  await page.getByRole('button', { name: 'Settings' }).click()
  await expect(page.getByLabel(/^Version: \d+\.\d+\.\d+$/)).toBeVisible()
  await expect(page.getByLabel('Signature: 3 days left')).toBeVisible()
})
