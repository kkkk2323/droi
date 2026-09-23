import { expect, openLocalClient, pickSession, shellRecord, test } from './fixtures'
import { session, userMessage } from '../fake-daemon/scenario'

const chat = session('Chat', '/Users/dev/acme-web', [userMessage('hi')])

test.describe('open the Workspace in another app', () => {
  test.use({ scenario: { sessions: [chat] } })

  test('opens in Finder until another app is picked, then remembers that one', async ({
    page,
    fakeDaemon,
  }) => {
    await openLocalClient(page, fakeDaemon)
    await pickSession(page, /Chat/)

    await page.getByRole('button', { name: 'Open in Finder' }).click()
    expect((await shellRecord(page)).opened).toEqual([['/Users/dev/acme-web', 'finder']])

    await page.getByRole('button', { name: 'Open in another app' }).click()
    const menu = page.getByRole('menu', { name: 'Open in' })
    await expect(menu.getByRole('menuitemradio')).toHaveText(['VS Code', 'Finder'])
    await expect(menu.getByRole('menuitemradio', { name: 'Finder' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    await menu.getByRole('menuitemradio', { name: 'VS Code' }).click()
    expect((await shellRecord(page)).opened).toEqual([
      ['/Users/dev/acme-web', 'finder'],
      ['/Users/dev/acme-web', 'vscode'],
    ])

    await page.reload()
    await expect(page.getByRole('button', { name: 'Open in VS Code' })).toBeVisible()
  })

  test('a Remote Client has no way to open the Workspace', async ({
    page,
    openClient,
    pickSession: pick,
  }) => {
    await openClient()
    await pick(/Chat/)
    await expect(page.getByRole('log', { name: 'Transcript' })).toBeVisible()
    await expect(page.getByRole('button', { name: /^Open in/ })).toHaveCount(0)
  })
})
