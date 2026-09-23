import { expect, test } from './fixtures'
import type { RecordedRequest } from '../fake-daemon/fake-daemon'
import { session, userMessage } from '../fake-daemon/scenario'
import { streamedReply } from '../fake-daemon/turns'

function sessionIdOf(request: RecordedRequest): string {
  return String((request.params as Record<string, unknown>)['sessionId'])
}

const older = session('Old work', '/Users/dev/billing-service', [userMessage('a')])
const newer = session('Recent work', '/Users/dev/acme-web', [userMessage('b')])
const newest = session('Newest work', '/Users/dev/acme-web', [userMessage('c')])

test.describe('draft sessions', () => {
  test.use({
    scenario: {
      sessions: [
        session('Real work', '/Users/dev/acme-web', [userMessage('a')]),
        // Left over from an earlier launch that never sent anything.
        session('New session', '/Users/dev/leftover', [], { tags: [{ name: 'droi.draft' }] }),
      ],
    },
  })

  test('a Draft Session is never listed, not even one from an earlier launch', async ({
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await fakeDaemon.waitForRequest('daemon.initialize_session')
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('region', { name: 'acme-web' })).toBeVisible()
    await expect(sidebar.getByRole('region', { name: 'leftover' })).toHaveCount(0)
  })
})

test.describe('new session', () => {
  test.use({
    scenario: {
      sessions: [older, newer, newest],
      validDirectories: ['/Users/dev/fresh-project'],
      handlers: { 'daemon.add_user_message': streamedReply({ deltas: ['On it.'] }) },
    },
  })

  test('the most recent Workspace is preselected; the menu lists the rest, deduplicated', async ({
    page,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session', exact: true }).click()
    const form = page.getByRole('region', { name: 'New session' })
    await expect(form.getByRole('heading', { level: 2 })).toHaveText(
      /What do you want to build in\s*acme-web\s*\?/,
    )
    await form.getByRole('button', { name: 'Workspace' }).click()
    const items = page.getByRole('menu').getByRole('menuitemradio')
    await expect(items).toHaveText(['acme-web', 'billing-service'])
    await expect(items.nth(0)).toHaveAttribute('aria-checked', 'true')
    await expect(
      page.getByRole('menu').getByRole('menuitem', { name: 'Other folder…' }),
    ).toBeVisible()
  })

  test('choosing a recent Workspace and sending creates the Session and posts the first message', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session', exact: true }).click()
    const form = page.getByRole('region', { name: 'New session' })
    // The page opens a Draft Session for the preselected Workspace at once.
    const firstDraft = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(firstDraft.params).toMatchObject({
      cwd: '/Users/dev/acme-web',
      tags: [{ name: 'droi.draft' }],
    })
    await form.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitemradio', { name: 'billing-service' }).click()
    await expect(form.getByRole('heading', { level: 2 })).toContainText('billing-service')

    // Switching Workspace closes that draft and opens one in the new Workspace.
    const draft = await fakeDaemon.waitForRequest('daemon.initialize_session', 2)
    expect(draft.params).toMatchObject({ cwd: '/Users/dev/billing-service' })
    const closed = await fakeDaemon.waitForRequest('daemon.close_session')
    expect(closed.params).toMatchObject({ sessionId: sessionIdOf(firstDraft) })

    await form.getByRole('textbox', { name: 'Message' }).fill('Refactor the invoices')
    await form.getByRole('button', { name: 'Start session' }).click()

    // Sending takes the draft over instead of creating another Session.
    const takeover = await fakeDaemon.waitForRequest('daemon.update_session_settings')
    expect(takeover.params).toMatchObject({ sessionId: sessionIdOf(draft), tags: [] })
    await expect(page).toHaveURL(new RegExp(`#/s/${sessionIdOf(draft)}`))
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('article', { name: 'You' })).toContainText(
      'Refactor the invoices',
    )
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ sessionId: sessionIdOf(draft) })
    expect(
      fakeDaemon.requests.filter((r) => r.method === 'daemon.initialize_session'),
    ).toHaveLength(2)

    // The Session appears in the sidebar under its Workspace.
    const billing = (await openSidebar()).getByRole('region', { name: 'billing-service' })
    await expect(billing.getByRole('listitem').first().getByRole('button')).toHaveAttribute(
      'aria-current',
      'page',
    )
  })

  test('a Workspace group in the sidebar starts a new Session working there', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const billing = (await openSidebar()).getByRole('region', { name: 'billing-service' })
    await billing.getByRole('heading').hover()
    await billing.getByRole('button', { name: 'New session in billing-service' }).click()
    const form = page.getByRole('region', { name: 'New session' })
    await expect(form.getByRole('heading', { level: 2 })).toContainText('billing-service')
    expect(new URL(page.url()).hash).toContain('#/new?ws=')

    await form.getByRole('button', { name: 'Start session' }).click()
    // Home opened a draft in the most recent Workspace first; this page's is the second.
    const draft = await fakeDaemon.waitForRequest('daemon.initialize_session', 2)
    expect(draft.params).toMatchObject({ cwd: '/Users/dev/billing-service' })
    await expect(page).toHaveURL(new RegExp(`#/s/${sessionIdOf(draft)}`))
  })

  test('sending nothing still opens an empty Session in the preselected Workspace', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session', exact: true }).click()
    await page
      .getByRole('region', { name: 'New session' })
      .getByRole('button', { name: 'Start session' })
      .click()
    const created = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(created.params).toMatchObject({ cwd: '/Users/dev/acme-web' })
    await expect(page).toHaveURL(new RegExp(`#/s/${sessionIdOf(created)}`))
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeEnabled()
    expect(fakeDaemon.requests.filter((r) => r.method === 'daemon.add_user_message')).toHaveLength(
      0,
    )
  })

  test('the model, effort and autonomy picked on the start page go into the new Session', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session', exact: true }).click()
    const form = page.getByRole('region', { name: 'New session' })
    // Defaults come from the Daemon.
    await expect(form.getByRole('button', { name: 'Model' })).toHaveText('Auto Model')
    await expect(form.getByRole('combobox', { name: 'Autonomy' })).toHaveText('Low autonomy')

    await form.getByRole('button', { name: 'Model' }).click()
    await page
      .getByRole('listbox', { name: 'Models' })
      .getByRole('option', { name: /GPT-5/ })
      .click()
    await expect(form.getByRole('button', { name: 'Model' })).toHaveText('GPT-5')
    // The effort list follows the model; "none" is not on GPT-5's list.
    await expect(form.getByRole('combobox', { name: 'Reasoning effort' })).toHaveText('Low')
    await form.getByRole('combobox', { name: 'Reasoning effort' }).click()
    await page.getByRole('listbox').getByRole('option', { name: 'Extra high' }).click()
    await form.getByRole('combobox', { name: 'Autonomy' }).click()
    await page.getByRole('listbox').getByRole('option', { name: 'High autonomy' }).click()

    await form.getByRole('button', { name: 'Start session' }).click()
    // The Draft Session already exists; the choices are applied as it is taken over.
    const draft = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(draft.params).toMatchObject({ cwd: '/Users/dev/acme-web' })
    const takeover = await fakeDaemon.waitForRequest('daemon.update_session_settings')
    expect(takeover.params).toMatchObject({
      sessionId: sessionIdOf(draft),
      modelId: 'gpt-5',
      reasoningEffort: 'xhigh',
      autonomyLevel: 'high',
      tags: [],
    })
  })

  test('a typed path is validated by the Daemon', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session', exact: true }).click()
    const form = page.getByRole('region', { name: 'New session' })
    await form.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitem', { name: 'Other folder…' }).click()
    await expect(form.getByRole('heading', { level: 2 })).toContainText('Where should Droid work?')
    const path = page.getByRole('textbox', { name: 'Workspace path' })
    const start = page.getByRole('button', { name: 'Start', exact: true })
    await expect(start).toBeDisabled()

    await path.fill('/nope/missing')
    await start.click()
    await expect(page.getByRole('alert')).toContainText('Directory does not exist: /nope/missing')
    expect(
      fakeDaemon.requests
        .filter((r) => r.method === 'daemon.validate_working_directory')
        .map((r) => (r.params as Record<string, unknown>)['workingDirectory']),
    ).toContain('/nope/missing')
    const initialized = () =>
      fakeDaemon.requests
        .filter((r) => r.method === 'daemon.initialize_session')
        .map((r) => (r.params as Record<string, unknown>)['cwd'])
    expect(initialized()).not.toContain('/nope/missing')

    await path.fill('/Users/dev/fresh-project')
    await start.click()
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeEnabled()
    expect(initialized()).toContain('/Users/dev/fresh-project')
    await expect((await openSidebar()).getByRole('region', { name: 'fresh-project' })).toBeVisible()
  })
})
