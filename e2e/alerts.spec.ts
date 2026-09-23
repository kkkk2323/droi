import type { Page } from '@playwright/test'
import {
  clickNotification,
  drawerGone,
  expect,
  openLocalClient,
  openSidebar,
  pickSession,
  setWindowFocused,
  shellRecord,
  test,
} from './fixtures'
import { session, userMessage } from './fake-daemon/scenario'
import { permissionTurn, streamedReply } from './fake-daemon/turns'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('hi')])
const notes = session('Notes', '/Users/dev/acme-web', [userMessage('yo')])

const DONE = 'data:audio/wav,fx-ok01'
const ASKING = 'data:audio/wav,fx-ack01'

async function send(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text)
  await page.getByRole('button', { name: 'Send' }).click()
}

/** The Session's row in the sidebar; the phone's drawer is closed again afterwards. */
async function expectRow(
  page: Page,
  title: RegExp,
  check: (row: ReturnType<Page['getByRole']>) => Promise<void>,
) {
  const sidebar = await openSidebar(page)
  await check(sidebar.getByRole('button', { name: title }))
  if (await page.getByRole('dialog', { name: 'Sessions' }).isVisible()) {
    await page.keyboard.press('Escape')
    await drawerGone(page)
  }
}

const sounds = async (page: Page) => (await shellRecord(page)).sounds
const notifications = async (page: Page) => (await shellRecord(page)).notifications

test.describe('a Session waiting for an answer', () => {
  test.use({
    scenario: {
      sessions: [deploy, notes],
      handlers: {
        'daemon.add_user_message': permissionTurn({ command: 'npm test', reply: 'All green.' }),
      },
    },
  })

  test('shows Needs input and chimes; answering it plays the completion sound', async ({
    page,
    fakeDaemon,
  }) => {
    await openLocalClient(page, fakeDaemon)
    await pickSession(page, /Deploy/)
    await send(page, 'Run the tests')

    await expect(page.getByRole('group', { name: /Permission request/ })).toBeVisible()
    await expectRow(page, /Deploy/, (row) =>
      expect(row.getByRole('status', { name: 'Needs input' })).toBeVisible(),
    )
    await expect.poll(() => sounds(page)).toEqual([ASKING])

    await page.getByRole('button', { name: 'Yes, allow' }).click()
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('All green.')
    await expect.poll(() => sounds(page)).toEqual([ASKING, DONE])
    await expectRow(page, /Deploy/, async (row) => {
      await expect(row.getByRole('status')).toHaveCount(0)
      await expect(row.getByRole('img', { name: 'Unread' })).toHaveCount(0)
    })
    // Watching the Session in a focused window: nothing to notify about.
    expect(await notifications(page)).toEqual([])
  })

  test('with Droi in the background, a desktop notification says it needs input', async ({
    page,
    fakeDaemon,
  }) => {
    await openLocalClient(page, fakeDaemon)
    await pickSession(page, /Deploy/)
    await setWindowFocused(page, false)
    await send(page, 'Run the tests')

    await expect
      .poll(() => notifications(page))
      .toEqual([
        {
          title: 'Droid needs input',
          body: 'Deploy — waiting for your answer',
          sessionId: deploy.sessionId,
        },
      ])
    expect(await sounds(page)).toEqual([ASKING])
  })

  test('a Remote Client shows the state but plays nothing', async ({ page, openClient }) => {
    await openClient()
    await pickSession(page, /Deploy/)
    await send(page, 'Run the tests')
    await expectRow(page, /Deploy/, (row) =>
      expect(row.getByRole('status', { name: 'Needs input' })).toBeVisible(),
    )
  })
})

test.describe('a Session finishing out of sight', () => {
  test.use({
    scenario: {
      sessions: [deploy, notes],
      handlers: {
        'daemon.add_user_message': streamedReply({
          deltas: ['Working ', 'on ', 'it.'],
          delayMs: 400,
        }),
      },
    },
  })

  test('notifies, marks it unread, and the notification opens it', async ({ page, fakeDaemon }) => {
    await openLocalClient(page, fakeDaemon)
    await pickSession(page, /Deploy/)
    await send(page, 'Go')
    await pickSession(page, /Notes/)

    await expect
      .poll(() => notifications(page))
      .toEqual([{ title: 'Droid finished', body: 'Deploy', sessionId: deploy.sessionId }])
    expect(await sounds(page)).toEqual([DONE])
    await expectRow(page, /Deploy/, (row) =>
      expect(row.getByRole('img', { name: 'Unread' })).toBeVisible(),
    )

    await clickNotification(page, deploy.sessionId)
    await expect(page).toHaveURL(new RegExp(`#/s/${deploy.sessionId}`))
    await expectRow(page, /Deploy/, (row) =>
      expect(row.getByRole('img', { name: 'Unread' })).toHaveCount(0),
    )
  })
})

test.describe('notification settings', () => {
  test.use({ scenario: { sessions: [deploy] } })

  test('each event has its own sound, with a test button and a custom file', async ({
    page,
    fakeDaemon,
  }) => {
    await openLocalClient(page, fakeDaemon)
    await (await openSidebar(page)).getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('button', { name: 'Notifications' }).click()

    await page.getByRole('button', { name: 'Test completion sound' }).click()
    await page.getByRole('button', { name: 'Test needs-input sound' }).click()
    await expect.poll(() => sounds(page)).toEqual([DONE, ASKING])

    await page.getByRole('combobox', { name: 'Completion sound' }).click()
    await page.getByRole('option', { name: 'Acknowledge tone' }).click()
    await page.getByRole('button', { name: 'Test completion sound' }).click()
    await expect.poll(() => sounds(page)).toEqual([DONE, ASKING, ASKING])

    await page.getByRole('combobox', { name: 'Completion sound' }).click()
    await page.getByRole('option', { name: 'Custom…' }).click()
    await expect(page.getByText('Custom file: ding.wav')).toBeVisible()
    await page.getByRole('button', { name: 'Test completion sound' }).click()
    await expect
      .poll(() => sounds(page))
      .toEqual([DONE, ASKING, ASKING, 'data:audio/wav,/Users/dev/sounds/ding.wav'])

    const finishes = page.getByRole('switch', { name: 'Notify when Droid finishes' })
    await expect(finishes).toBeChecked()
    await finishes.click()
    await page.reload()
    await page.getByRole('button', { name: 'Notifications' }).click()
    await expect(page.getByRole('switch', { name: 'Notify when Droid finishes' })).not.toBeChecked()
    await expect(page.getByRole('combobox', { name: 'Completion sound' })).toHaveText('Custom…')
  })
})
