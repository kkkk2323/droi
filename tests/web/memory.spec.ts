import type { Locator, Page } from '@playwright/test'
import { expect, openLocalClient, shellRecord, test } from './fixtures'
import { session, userMessage } from '../fake-daemon/scenario'

const first = session('First session', '/Users/dev/app', [userMessage('hello')])

async function openMemory(page: Page, openSidebar: () => Promise<Locator>) {
  await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('button', { name: 'Memory' }).click()
  await expect(page.getByRole('heading', { level: 2, name: 'Memory' })).toBeVisible()
}

test.describe('Settings → Memory', () => {
  test.use({ scenario: { sessions: [first] } })

  test('the Beta switch asks before it restarts the Daemon', async ({
    page,
    fakeDaemon,
    openSidebar,
  }) => {
    await openLocalClient(page, fakeDaemon)
    await openMemory(page, openSidebar)
    await expect(page.getByText('Beta', { exact: true })).toBeVisible()
    const memory = page.getByRole('switch', { name: 'Memory' })
    await expect(memory).not.toBeChecked()

    await memory.click()
    const confirm = page.getByRole('alertdialog', { name: 'Turn Memory on' })
    await expect(confirm).toContainText('Sessions that are working stop')
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    await expect(confirm).toHaveCount(0)
    await expect(memory).not.toBeChecked()
    expect((await shellRecord(page)).settingsUpdates).toEqual([])

    await memory.click()
    await page
      .getByRole('alertdialog', { name: 'Turn Memory on' })
      .getByRole('button', { name: 'Restart Daemon' })
      .click()
    await expect(memory).toBeChecked()
    await expect(page.getByRole('alertdialog')).toHaveCount(0)
    const record = await shellRecord(page)
    expect(record.settingsUpdates).toEqual([{ memoryEnabled: true }])
    expect(record.daemonRestarts).toBe(1)

    await memory.click()
    await page
      .getByRole('alertdialog', { name: 'Turn Memory off' })
      .getByRole('button', { name: 'Restart Daemon' })
      .click()
    await expect(memory).not.toBeChecked()
    expect((await shellRecord(page)).daemonRestarts).toBe(2)
  })

  test('the Memory model comes from the Daemon’s model list', async ({
    page,
    fakeDaemon,
    openSidebar,
  }) => {
    await openLocalClient(page, fakeDaemon, { memoryEnabled: true })
    await openMemory(page, openSidebar)
    const picker = page.getByRole('button', { name: 'Memory model', exact: true })
    await expect(picker).toHaveText('glm-5.3-flash')
    await picker.click()
    const dialog = page.getByRole('dialog', { name: 'Choose a model' })
    await dialog
      .getByRole('listbox', { name: 'Models' })
      .getByRole('option', { name: /GPT-5/ })
      .click()
    await expect(dialog).toBeHidden()
    await expect(picker).toHaveText('GPT-5')
    const record = await shellRecord(page)
    expect(record.settingsUpdates).toEqual([{ memoryModel: 'gpt-5' }])
    expect(record.daemonRestarts).toBe(0)
  })

  test('a Remote Client has no Memory tab', async ({ page, openClient, openSidebar }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
    await expect(page.getByRole('heading', { level: 2, name: 'General' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Memory' })).toHaveCount(0)
  })
})
