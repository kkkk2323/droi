import type { Page } from '@playwright/test'
import { exchange, userEntry } from './fake-kernel/history'
import { expect, openSessionList, pairPhone, pickSession, standIns, test } from './fixtures'
import type { FakeKernel } from './fake-kernel/fake-kernel'

test.use({
  scenario: {
    autonomy: 'ask',
    sessions: [
      { title: 'Deploy', cwd: 'acme-web', entries: [userEntry('ship it')] },
      { title: 'Notes', cwd: 'acme-web', entries: exchange('todo', 'Noted.') },
    ],
  },
})

const played = async (page: Page) => {
  const { sounds, haptics } = await standIns(page)
  return { sounds, haptics }
}

/** Deploy asks to run a command, waits for the computer to allow it, then finishes. */
async function askThenFinish(fakeKernel: FakeKernel, before: () => Promise<void>) {
  fakeKernel.model.push(
    { match: 'clean up', call: { name: 'bash', arguments: { command: 'echo cleaned' } } },
    { match: 'clean up', text: 'Cleaned.' },
  )
  await fakeKernel.submit('Deploy', 'clean up')
  await before()
  await fakeKernel.approve('Deploy')
}

test('finishing and needing input each play their sound and haptic', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Notes/)
  await askThenFinish(fakeKernel, () =>
    expect.poll(() => played(page)).toEqual({ sounds: ['needs-input'], haptics: ['needs-input'] }),
  )
  await expect
    .poll(() => played(page))
    .toEqual({ sounds: ['needs-input', 'finished'], haptics: ['needs-input', 'finished'] })
})

test('the Session on screen stays quiet; another is unread until opened', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Notes/)
  await fakeKernel.turn('Notes', 'and then?', { text: 'Then this.', delayMs: 100 })
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Then this.')
  await fakeKernel.turn('Deploy', 'status?', { text: 'All green.', delayMs: 100 })
  await expect.poll(() => played(page)).toEqual({ sounds: ['finished'], haptics: ['finished'] })

  let list = await openSessionList(page)
  await expect(
    list.getByRole('button', { name: /Deploy/ }).getByRole('img', { name: 'Unread' }),
  ).toBeVisible()
  await expect(
    list.getByRole('button', { name: /Notes/ }).getByRole('img', { name: 'Unread' }),
  ).toHaveCount(0)

  await pickSession(page, /Deploy/)
  list = await openSessionList(page)
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(0)
})

test('per-event switches turn the sound and the haptic off', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  await openSessionList(page)
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('switch', { name: 'Sound when finished' }).click()
  await page.getByRole('switch', { name: 'Haptic when it needs input' }).click()
  await page.goBack()

  await pickSession(page, /Notes/)
  await askThenFinish(fakeKernel, () =>
    expect.poll(() => played(page)).toEqual({ sounds: ['needs-input'], haptics: [] }),
  )
  await expect.poll(() => played(page)).toEqual({ sounds: ['needs-input'], haptics: ['finished'] })
})
