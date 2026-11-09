import type { Page } from '@playwright/test'
import { userEntry } from './fake-kernel/history'
import {
  expect,
  openNewSession,
  openSessionList,
  pairingLink,
  pairPhone,
  pasteLink,
  pickSession,
  relaunch,
  sessionList,
  test,
} from './fixtures'

test.use({
  scenario: { sessions: [{ title: 'Deploy', cwd: 'acme-web', entries: [userEntry('ship it')] }] },
})

/** As if iOS sent the app to the background or brought it back. */
async function setAppVisible(page: Page, visible: boolean) {
  await page.evaluate((value) => {
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => (value ? 'visible' : 'hidden'),
    })
    document.dispatchEvent(new Event('visibilitychange'))
  }, visible)
}

test('a reset Pairing Token says the computer is no longer paired; pairing again recovers it', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  await fakeKernel.resetToken()
  await relaunch(page)
  await expect(page.getByRole('heading', { name: 'Test Mac is no longer paired' })).toBeVisible()

  await pasteLink(page, pairingLink(fakeKernel))
  await expect(sessionList(page)).toBeVisible()
  await openSessionList(page)
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
  await page.getByRole('button', { name: 'Settings' }).click()
  await expect(page.getByRole('button', { name: 'Paired computers: 1' })).toBeVisible()
})

test('an unreachable computer shows its address and what to check, then connects when back', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  await fakeKernel.goDown()
  await relaunch(page)
  await expect(page.getByRole('heading', { name: 'Can’t reach Test Mac' })).toBeVisible()
  await expect(page.getByText(fakeKernel.url, { exact: true })).toBeVisible()
  const checks = page.getByRole('list', { name: 'What to check' }).getByRole('listitem')
  await expect(checks.first()).toContainText('Remote Access')
  await expect(checks).toHaveCount(4)
  await expect(checks.nth(2)).toContainText('Set up a relay')

  await fakeKernel.comeBack()
  await expect(sessionList(page)).toBeVisible({ timeout: 10_000 })
  await expect(page.getByRole('heading', { name: /Can’t reach/ })).toHaveCount(0)
})

test('a computer without a running Kernel says so, and connects once it runs one', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  await fakeKernel.withoutKernel()
  await relaunch(page)
  await expect(
    page.getByRole('heading', { name: 'Test Mac runs Droi without its Kernel' }),
  ).toBeVisible()
  await fakeKernel.withKernel()
  await expect(sessionList(page)).toBeVisible({ timeout: 15_000 })
})

test.describe('on a computer running another Droi', () => {
  test.use({
    scenario: {
      version: '0.9.0',
      sessions: [{ title: 'Deploy', cwd: 'acme-web', entries: [userEntry('ship it')] }],
    },
  })

  test('a notice says so and can be dismissed', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    const notice = page.getByRole('alert').filter({ hasText: 'runs Droi 0.9.0' })
    await expect(notice).toBeVisible()
    await notice.getByRole('button', { name: 'Dismiss' }).click()
    await expect(notice).toHaveCount(0)
    const list = await openSessionList(page)
    await expect(list.getByRole('button', { name: /Deploy/ })).toBeVisible()

    await relaunch(page)
    await expect(sessionList(page)).toBeVisible()
    await expect(page.getByRole('alert').filter({ hasText: 'runs Droi' })).toHaveCount(0)
  })
})

test('coming back to the foreground re-checks the pairing', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Deploy/)
  await setAppVisible(page, false)
  await fakeKernel.resetToken()
  await setAppVisible(page, true)
  await expect(page.getByRole('heading', { name: 'Test Mac is no longer paired' })).toBeVisible()
})

test('the background lets the socket go; the foreground reconnects to the same screen', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Deploy/)
  await expect.poll(() => phoneWire.count('conversation.open')).toBe(1)
  const composer = page.getByRole('textbox', { name: 'Message' })
  await composer.fill('half a thought')
  const transcript = page.getByRole('log', { name: 'Transcript' })

  await setAppVisible(page, false)
  await expect.poll(() => phoneWire.open()).toBe(0)
  await expect(transcript.getByRole('article', { name: 'You' })).toHaveText('ship it')

  await setAppVisible(page, true)
  await expect.poll(() => phoneWire.open()).toBe(1)
  await expect.poll(() => phoneWire.count('conversation.open')).toBe(2)
  await expect(page.getByRole('status', { name: 'Reconnecting' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
  await expect(transcript.getByRole('article', { name: 'You' }).first()).toHaveText('ship it')
  await expect(composer).toHaveValue('half a thought')
  // The Kernel's news flows again on the new socket.
  await fakeKernel.turn('Deploy', 'status?', { chunks: ['All ', 'green.'] })
  await expect(transcript).toContainText('All green.')
  await openSessionList(page)
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
})

test('the New session page comes back with its text', async ({ page, fakeKernel, phoneWire }) => {
  await pairPhone(page, fakeKernel)
  await openNewSession(page)
  const composer = page.getByRole('textbox', { name: 'Message' })
  await composer.fill('build a thing')

  await setAppVisible(page, false)
  await expect.poll(() => phoneWire.open()).toBe(0)
  await setAppVisible(page, true)
  await expect.poll(() => phoneWire.open()).toBe(1)
  await expect(page.getByRole('region', { name: 'New session' })).toBeVisible()
  await expect(composer).toHaveValue('build a thing')
  await expect(page.getByRole('button', { name: 'Start session' })).toBeEnabled()
  expect(phoneWire.count('conversation.create')).toBe(0)
})

test('a dropped socket reconnects with a banner and keeps the open Session', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Deploy/)
  await expect.poll(() => phoneWire.count('conversation.open')).toBe(1)
  await fakeKernel.goDown()
  await expect(page.getByRole('status', { name: 'Reconnecting' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
  await fakeKernel.comeBack()
  await expect(page.getByRole('status', { name: 'Reconnecting' })).toHaveCount(0, {
    timeout: 15_000,
  })
  await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
  // The Session is opened again on the new socket.
  await expect.poll(() => phoneWire.count('conversation.open')).toBe(2)
})
