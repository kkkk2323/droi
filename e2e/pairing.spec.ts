import { expect, test } from './fixtures'

test.describe('pairing', () => {
  test('a pairing link stores the token, strips it from the URL and connects', async ({
    page,
    fakeDaemon,
    openClient,
  }) => {
    await openClient()

    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)
    expect(new URL(page.url()).hash).toBe('')
    expect(await page.evaluate(() => localStorage.getItem('droi.pairingToken'))).toBe(
      fakeDaemon.token,
    )
    expect(fakeDaemon.requests.map((r) => r.method)).toContain('daemon.authenticate')
  })

  test('a stored token is reused on the next visit without a link', async ({
    page,
    openClient,
  }) => {
    await openClient()
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)

    await page.goto('/')
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)
  })

  test('a wrong token shows pairing failed and does not retry', async ({
    page,
    fakeDaemon,
    openClient,
  }) => {
    await openClient({ token: 'not-the-token' })

    await expect(page.getByRole('heading', { name: 'Pairing failed' })).toBeVisible()
    await page.waitForTimeout(1_500)
    expect(fakeDaemon.requests).toHaveLength(0)
    expect(fakeDaemon.connectionCount).toBe(0)
  })

  test('a revoked token shows pairing failed on the next visit', async ({
    page,
    fakeDaemon,
    openClient,
  }) => {
    await openClient()
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)

    fakeDaemon.revokeToken()
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Pairing failed' })).toBeVisible()
  })

  test('without any token the Client asks to be paired', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Pairing failed' })).toBeVisible()
    await expect(page.getByText(/has not been paired/)).toBeVisible()
  })
})

test.describe('remote client surface', () => {
  test('a Remote Client gets only the General settings; the Shell tabs need the bridge', async ({
    page,
    openClient,
  }) => {
    await openClient()
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)
    await page.goto('/#/settings')
    const sections = page.getByRole('navigation', { name: 'Settings sections' })
    await expect(sections.getByRole('button', { name: 'General' })).toBeVisible()
    await expect(
      sections.getByRole('button', { name: /Account|Daemon|Remote Access/ }),
    ).toHaveCount(0)
    await expect(page.getByRole('combobox', { name: 'Theme' })).toBeVisible()
    await page.getByRole('combobox', { name: 'Text size' }).click()
    await page.getByRole('listbox').getByRole('option', { name: 'Large', exact: true }).click()
    await expect(page.locator('html')).toHaveCSS('font-size', '17.5px')
    await page.reload()
    await expect(page.locator('html')).toHaveCSS('font-size', '17.5px')
    await expect(page.getByRole('switch', { name: 'Show archived sessions' })).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'Factory API key' })).toHaveCount(0)
  })
})
