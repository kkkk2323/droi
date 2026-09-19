import { expect, test } from './fixtures'

test.describe('connection state', () => {
  test('losing the Daemon shows a reconnecting banner that clears when it returns', async ({
    page,
    fakeDaemon,
    openClient,
  }) => {
    await openClient()
    const status = page.getByRole('status', { name: 'Connection' })
    await expect(status).toHaveText(/Connected/)

    fakeDaemon.goDown()
    const banner = page.getByRole('alert')
    await expect(banner).toContainText(/Reconnecting|Cannot reach/)
    await expect(status).not.toHaveText(/Connected/)

    fakeDaemon.comeBack()
    await expect(status).toHaveText(/Connected/, { timeout: 15_000 })
    await expect(banner).toHaveCount(0)
    expect(fakeDaemon.requests.filter((r) => r.method === 'daemon.authenticate').length).toBe(2)
  })

  test('the Daemon being away longer than the SDK retries still recovers', async ({
    page,
    fakeDaemon,
    openClient,
  }) => {
    await openClient()
    const status = page.getByRole('status', { name: 'Connection' })
    await expect(status).toHaveText(/Connected/)

    fakeDaemon.goDown()
    await expect(page.getByRole('alert')).toBeVisible()
    // The SDK's own budget is 3 attempts at 1s, 1.5s, 2.25s (plus jitter).
    await page.waitForTimeout(8_000)
    await expect(status).not.toHaveText(/Connected/)

    fakeDaemon.comeBack()
    await expect(status).toHaveText(/Connected/, { timeout: 15_000 })
    await expect(page.getByRole('alert')).toHaveCount(0)
  })
})
