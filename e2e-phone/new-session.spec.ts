import type { RecordedRequest } from '../e2e/fake-daemon/fake-daemon'
import { session, userMessage } from '../e2e/fake-daemon/scenario'
import { streamedReply } from '../e2e/fake-daemon/turns'
import { expect, openDrawer, pairPhone, pickSession, test } from './fixtures'

function sessionIdOf(request: RecordedRequest): string {
  return String((request.params as Record<string, unknown>)['sessionId'])
}

const older = session('Old work', '/Users/dev/billing-service', [userMessage('a')])
const newer = session('Recent work', '/Users/dev/acme-web', [userMessage('b')])

test.use({
  scenario: {
    sessions: [older, newer],
    validDirectories: ['/Users/dev/fresh-project'],
    skills: [{ name: 'handoff', description: 'Compact the conversation' }],
    handlers: { 'daemon.add_user_message': streamedReply({ deltas: ['On it.'] }) },
  },
})

test('starts a Session in a recent Workspace with the chosen model', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const form = page.getByRole('region', { name: 'New session' })
  await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/acme-web\?/)
  const firstDraft = await fakeDaemon.waitForRequest('daemon.initialize_session')
  expect(firstDraft.params).toMatchObject({
    cwd: '/Users/dev/acme-web',
    tags: [{ name: 'droi.draft' }],
  })

  await form.getByRole('button', { name: 'Workspace' }).click()
  await page
    .getByRole('dialog', { name: 'Workspace' })
    .getByRole('radio', {
      name: 'billing-service',
    })
    .click()
  await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/billing-service\?/)
  const draft = await fakeDaemon.waitForRequest('daemon.initialize_session', 2)
  expect(draft.params).toMatchObject({ cwd: '/Users/dev/billing-service' })
  const closed = await fakeDaemon.waitForRequest('daemon.close_session')
  expect(closed.params).toMatchObject({ sessionId: sessionIdOf(firstDraft) })

  await form.getByRole('button', { name: 'Model', exact: true }).click()
  await page
    .getByRole('dialog', { name: 'Choose a model' })
    .getByRole('radio', { name: 'GPT-5' })
    .click()
  await expect(form.getByRole('button', { name: 'Model', exact: true })).toHaveText(/GPT-5/)

  await form.getByRole('textbox', { name: 'Message' }).fill('Refactor the invoices')
  await form.getByRole('button', { name: 'Start session' }).click()
  const takeover = await fakeDaemon.waitForRequest('daemon.update_session_settings')
  expect(takeover.params).toMatchObject({
    sessionId: sessionIdOf(draft),
    modelId: 'gpt-5',
    tags: [],
  })
  const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
  expect(sent.params).toMatchObject({
    sessionId: sessionIdOf(draft),
    text: 'Refactor the invoices',
  })
  const transcript = page.getByRole('log', { name: 'Transcript' })
  await expect(transcript.getByRole('article', { name: 'You' })).toContainText(
    'Refactor the invoices',
  )
  expect(fakeDaemon.requests.filter((r) => r.method === 'daemon.initialize_session')).toHaveLength(
    2,
  )
})

test('a typed path is checked by the Daemon and starts a Session there', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const form = page.getByRole('region', { name: 'New session' })
  await form.getByRole('button', { name: 'Workspace' }).click()
  await page.getByRole('button', { name: 'Other folder…' }).click()
  await expect(form.getByRole('heading', { name: 'Where should Droid work?' })).toBeVisible()
  const path = form.getByRole('textbox', { name: 'Workspace path' })
  await path.fill('/nope/missing')
  await form.getByRole('button', { name: 'Start', exact: true }).click()
  await expect(form.getByRole('alert')).toContainText('Directory does not exist: /nope/missing')

  await path.fill('/Users/dev/fresh-project')
  await form.getByRole('button', { name: 'Start', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Message' })).toHaveAttribute(
    'placeholder',
    'Ask anything',
  )
  const list = await openDrawer(page)
  await expect(list.getByRole('group', { name: 'fresh-project' })).toBeVisible()
})

test('"/" lists the Workspace’s skills before the first send', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const form = page.getByRole('region', { name: 'New session' })
  await fakeDaemon.waitForRequest('daemon.initialize_session')
  await form.getByRole('textbox', { name: 'Message' }).fill('/')
  const menu = page.getByRole('menu', { name: 'Commands and skills' })
  await expect(menu.getByRole('menuitem')).toHaveText([/\/handoff.*Skill/])
})

test('leaving the page closes the draft, which never shows in the list', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const draft = await fakeDaemon.waitForRequest('daemon.initialize_session')
  const list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Recent work/ })).toBeVisible()
  await expect(list.getByRole('button', { name: /Old work/ })).toBeVisible()
  await expect(list.getByRole('group')).toHaveCount(2)
  await pickSession(page, /Recent work/)
  const closed = await fakeDaemon.waitForRequest('daemon.close_session')
  expect(closed.params).toMatchObject({ sessionId: sessionIdOf(draft) })
  const again = await openDrawer(page)
  await expect(again.getByRole('group')).toHaveCount(2)
})
