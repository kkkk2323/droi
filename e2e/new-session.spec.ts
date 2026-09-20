import { expect, test } from './fixtures'
import { session, userMessage } from './fake-daemon/scenario'
import { streamedReply } from './fake-daemon/turns'

const older = session('Old work', '/Users/dev/billing-service', [userMessage('a')])
const newer = session('Recent work', '/Users/dev/acme-web', [userMessage('b')])
const newest = session('Newest work', '/Users/dev/acme-web', [userMessage('c')])

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
    await form.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitemradio', { name: 'billing-service' }).click()
    await expect(form.getByRole('heading', { level: 2 })).toContainText('billing-service')

    await form.getByRole('textbox', { name: 'Message' }).fill('Refactor the invoices')
    await form.getByRole('button', { name: 'Start session' }).click()

    const created = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(created.params).toMatchObject({ cwd: '/Users/dev/billing-service' })
    await expect(
      page.getByRole('region', { name: 'New session' }).getByRole('heading', { level: 2 }),
    ).toBeVisible()
    expect(new URL(page.url()).hash).toMatch(/^#\/s\//)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('article', { name: 'You' })).toContainText(
      'Refactor the invoices',
    )
    await fakeDaemon.waitForRequest('daemon.add_user_message')

    // The Session appears in the sidebar under its Workspace.
    const billing = (await openSidebar()).getByRole('region', { name: 'billing-service' })
    await expect(billing.getByRole('button', { name: /New session/ })).toHaveAttribute(
      'aria-current',
      'page',
    )
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
    const created = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(created.params).toMatchObject({
      cwd: '/Users/dev/acme-web',
      modelId: 'gpt-5',
      reasoningEffort: 'xhigh',
      autonomyLevel: 'high',
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
    const checked = await fakeDaemon.waitForRequest('daemon.validate_working_directory')
    expect(checked.params).toMatchObject({ workingDirectory: '/nope/missing' })
    expect(
      fakeDaemon.requests.filter((r) => r.method === 'daemon.initialize_session'),
    ).toHaveLength(0)

    await path.fill('/Users/dev/fresh-project')
    await start.click()
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeEnabled()
    const created = fakeDaemon.requests.find((r) => r.method === 'daemon.initialize_session')
    expect(created?.params).toMatchObject({ cwd: '/Users/dev/fresh-project' })
    await expect((await openSidebar()).getByRole('region', { name: 'fresh-project' })).toBeVisible()
  })
})
