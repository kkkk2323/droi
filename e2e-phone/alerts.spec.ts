import type { Page } from '@playwright/test'
import type { FakeDaemon } from '../e2e/fake-daemon/fake-daemon'
import { session, userMessage } from '../e2e/fake-daemon/scenario'
import { expect, openDrawer, pairPhone, pickSession, standIns, test } from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])
const notes = session('Notes', '/Users/dev/acme-web', [userMessage('todo')])

test.use({ scenario: { sessions: [deploy, notes] } })

function workingState(daemon: FakeDaemon, sessionId: string, newState: string) {
  daemon.notify(sessionId, { type: 'droid_working_state_changed', newState })
}

/** Subscribes the phone to both Sessions and leaves Notes on screen. */
async function watchBoth(page: Page) {
  await pickSession(page, /Deploy/)
  await pickSession(page, /Notes/)
}

const played = async (page: Page) => {
  const { sounds, haptics } = await standIns(page)
  return { sounds, haptics }
}

test('finishing and needing input each play their sound and haptic', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await watchBoth(page)
  workingState(fakeDaemon, deploy.sessionId, 'streaming_assistant_message')
  workingState(fakeDaemon, deploy.sessionId, 'waiting_for_tool_confirmation')
  await expect
    .poll(() => played(page))
    .toEqual({ sounds: ['needs-input'], haptics: ['needs-input'] })
  workingState(fakeDaemon, deploy.sessionId, 'idle')
  await expect
    .poll(() => played(page))
    .toEqual({
      sounds: ['needs-input', 'finished'],
      haptics: ['needs-input', 'finished'],
    })
})

test('the Session on screen stays quiet; another is unread until opened', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await watchBoth(page)
  workingState(fakeDaemon, notes.sessionId, 'streaming_assistant_message')
  workingState(fakeDaemon, notes.sessionId, 'idle')
  workingState(fakeDaemon, deploy.sessionId, 'streaming_assistant_message')
  workingState(fakeDaemon, deploy.sessionId, 'idle')
  await expect.poll(() => played(page)).toEqual({ sounds: ['finished'], haptics: ['finished'] })

  let list = await openDrawer(page)
  await expect(
    list.getByRole('button', { name: /Deploy/ }).getByRole('img', { name: 'Unread' }),
  ).toBeVisible()
  await expect(
    list.getByRole('button', { name: /Notes/ }).getByRole('img', { name: 'Unread' }),
  ).toHaveCount(0)

  await pickSession(page, /Deploy/)
  list = await openDrawer(page)
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(0)
})

test('per-event switches turn the sound and the haptic off', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const list = await openDrawer(page)
  await list.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('switch', { name: 'Sound when finished' }).click()
  await page.getByRole('switch', { name: 'Haptic when it needs input' }).click()
  await page.goBack()

  await watchBoth(page)
  workingState(fakeDaemon, deploy.sessionId, 'waiting_for_tool_confirmation')
  workingState(fakeDaemon, deploy.sessionId, 'idle')
  await expect.poll(() => played(page)).toEqual({ sounds: ['needs-input'], haptics: ['finished'] })
})
