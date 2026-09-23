import type { Page } from '@playwright/test'
import { session, userMessage } from '../fake-daemon/scenario'
import {
  expect,
  openDrawer,
  pairingLink,
  pairPhone,
  pasteLink,
  pickSession,
  playTurn,
  test,
} from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])

test.use({ scenario: { sessions: [deploy] } })

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
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  fakeDaemon.resetToken()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Test Mac is no longer paired' })).toBeVisible()

  await pasteLink(page, pairingLink(fakeDaemon))
  await expect(page.getByRole('heading', { name: 'New session' })).toBeVisible()
  const list = await openDrawer(page)
  await expect(list.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
  await list.getByRole('button', { name: 'Settings' }).click()
  await expect(page.getByRole('button', { name: 'Paired computers: 1' })).toBeVisible()
})

test('an unreachable computer shows its address and what to check, then connects when back', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  fakeDaemon.goDown()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Can’t reach Test Mac' })).toBeVisible()
  await expect(page.getByText(fakeDaemon.url, { exact: true })).toBeVisible()
  const checks = page.getByRole('list', { name: 'What to check' }).getByRole('listitem')
  await expect(checks.first()).toContainText('Remote Access')
  await expect(checks).toHaveCount(3)

  fakeDaemon.comeBack()
  await expect(page.getByRole('heading', { name: 'New session' })).toBeVisible({
    timeout: 10_000,
  })
  await expect(page.getByRole('heading', { name: /Can’t reach/ })).toHaveCount(0)
})

test('another Droi version on the computer shows a notice that can be dismissed', async ({
  page,
  fakeDaemon,
}) => {
  fakeDaemon.meta = { ...fakeDaemon.meta, version: '0.9.0' }
  await pairPhone(page, fakeDaemon)
  const notice = page.getByRole('alert').filter({ hasText: 'runs Droi 0.9.0' })
  await expect(notice).toBeVisible()
  await notice.getByRole('button', { name: 'Dismiss' }).click()
  await expect(notice).toHaveCount(0)
  const list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Deploy/ })).toBeVisible()

  await page.reload()
  await expect(page.getByRole('heading', { name: 'New session' })).toBeVisible()
  await expect(page.getByRole('alert').filter({ hasText: 'runs Droi' })).toHaveCount(0)
})

test('coming back to the foreground re-checks the pairing', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Deploy/)
  await setAppVisible(page, false)
  fakeDaemon.resetToken()
  await setAppVisible(page, true)
  await expect(page.getByRole('heading', { name: 'Test Mac is no longer paired' })).toBeVisible()
})

test('the background lets the socket go; the foreground reconnects to the same screen', async ({
  page,
  fakeDaemon,
}) => {
  const loads = () => fakeDaemon.requests.filter((r) => r.method === 'daemon.load_session').length
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Deploy/)
  await expect.poll(loads).toBe(1)
  const composer = page.getByRole('textbox', { name: 'Message' })
  await composer.fill('half a thought')
  const transcript = page.getByRole('log', { name: 'Transcript' })

  await setAppVisible(page, false)
  await expect.poll(() => fakeDaemon.connectionCount).toBe(0)
  await expect(transcript.getByRole('article', { name: 'You' })).toHaveText('ship it')

  await setAppVisible(page, true)
  await expect.poll(() => fakeDaemon.connectionCount).toBe(1)
  await expect.poll(loads).toBe(2)
  await expect(page.getByRole('status', { name: 'Reconnecting' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
  await expect(transcript.getByRole('article', { name: 'You' })).toHaveText('ship it')
  await expect(composer).toHaveValue('half a thought')
  // Notifications flow again on the new socket.
  playTurn(fakeDaemon, deploy.sessionId, 'status?', ['All ', 'green.'])
  await expect(transcript).toContainText('All green.')
  const list = await openDrawer(page)
  await expect(list.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
})

test('the New session page comes back with its Draft Session and its text', async ({
  page,
  fakeDaemon,
}) => {
  const drafts = () =>
    fakeDaemon.requests.filter((r) => r.method === 'daemon.initialize_session').length
  await pairPhone(page, fakeDaemon)
  await expect.poll(drafts).toBe(1)
  const composer = page.getByRole('textbox', { name: 'Message' })
  await composer.fill('build a thing')

  await setAppVisible(page, false)
  await expect.poll(() => fakeDaemon.connectionCount).toBe(0)
  await setAppVisible(page, true)
  await expect.poll(drafts).toBe(2)
  await expect(page.getByRole('heading', { name: 'New session' })).toBeVisible()
  await expect(composer).toHaveValue('build a thing')
  await expect(page.getByRole('button', { name: 'Start session' })).toBeEnabled()
})

test('a dropped socket reconnects with a banner and keeps the open Session', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Deploy/)
  await expect
    .poll(() => fakeDaemon.requests.filter((r) => r.method === 'daemon.load_session').length)
    .toBe(1)
  fakeDaemon.goDown()
  await expect(page.getByRole('status', { name: 'Reconnecting' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
  fakeDaemon.comeBack()
  await expect(page.getByRole('status', { name: 'Reconnecting' })).toHaveCount(0, {
    timeout: 15_000,
  })
  await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
  // The Session is loaded again on the new socket.
  await expect
    .poll(() => fakeDaemon.requests.filter((r) => r.method === 'daemon.load_session').length)
    .toBe(2)
})
