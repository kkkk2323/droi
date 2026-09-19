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
  test('a Remote Client has no Settings; the settings route falls back to home', async ({
    page,
    openClient,
  }) => {
    await openClient()
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)
    await expect(page.getByRole('button', { name: 'Settings' })).toHaveCount(0)
    await page.goto('/#/settings')
    await expect(page.getByRole('region', { name: 'Settings' })).toHaveCount(0)
    await expect(page.getByText(/Select a session|Open the sessions list/)).toBeVisible()
  })
})
