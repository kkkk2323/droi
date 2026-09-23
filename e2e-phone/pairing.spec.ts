import { session, userMessage } from '../e2e/fake-daemon/scenario'
import { expect, openDrawer, pairPhone, pasteLink, pickSession, test } from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])
const notes = session('Notes', '/Users/dev/acme-web', [userMessage('todo')])
const docs = session('Docs', '/Users/dev/handbook', [userMessage('write')])

test.use({ scenario: { sessions: [deploy, notes, docs] } })

test('pasting a pairing link connects and lists the Sessions grouped by Workspace', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const list = await openDrawer(page)
  await expect(list.getByText('Test Mac')).toBeVisible()
  await expect(list.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
  const web = list.getByRole('group', { name: 'acme-web' })
  await expect(web.getByRole('button', { name: /Deploy/ })).toBeVisible()
  await expect(web.getByRole('button', { name: /Notes/ })).toBeVisible()
  await expect(list.getByRole('group', { name: 'handbook' }).getByRole('button')).toHaveText(/Docs/)
})

test('the Pairing Token goes to the keychain, not to app storage', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const stored = await page.evaluate(() => ({
    app: JSON.stringify({ ...localStorage }),
    keychain: JSON.stringify({ ...sessionStorage }),
  }))
  expect(stored.keychain).toContain(fakeDaemon.token)
  expect(stored.app).not.toContain(fakeDaemon.token)
  expect(stored.app).toContain(fakeDaemon.meta.computerId)
})

test('a working Session and a Session waiting on a Prompt show different states', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  // Opening a Session subscribes the phone to its notifications.
  await pickSession(page, /Deploy/)
  await pickSession(page, /Notes/)
  fakeDaemon.notify(deploy.sessionId, {
    type: 'droid_working_state_changed',
    newState: 'streaming_assistant_message',
  })
  fakeDaemon.notify(notes.sessionId, {
    type: 'droid_working_state_changed',
    newState: 'waiting_for_tool_confirmation',
  })
  const list = await openDrawer(page)
  const deployRow = list.getByRole('button', { name: /Deploy/ })
  const notesRow = list.getByRole('button', { name: /Notes/ })
  await expect(deployRow.getByRole('status', { name: 'Working' })).toBeVisible()
  await expect(notesRow.getByRole('status', { name: 'Needs input' })).toBeVisible()
  await expect(deployRow.getByRole('status', { name: 'Needs input' })).toHaveCount(0)
})

test('a link without the address or an unreachable computer says so', async ({ page }) => {
  await page.goto('/')
  await pasteLink(page, 'abc-123')
  await expect(page.getByRole('alert')).toHaveText(/whole pairing link/)
  await pasteLink(page, 'http://127.0.0.1:9/#pair=abc')
  await expect(page.getByRole('alert')).toHaveText(/Cannot reach http:\/\/127\.0\.0\.1:9/)
  await expect(page.getByRole('heading', { name: 'Pair with a computer' })).toBeVisible()
})
