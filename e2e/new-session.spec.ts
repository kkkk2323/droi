import { expect, test } from './fixtures'
import { session, userMessage } from './fake-daemon/scenario'

const older = session('Old work', '/Users/dev/billing-service', [userMessage('a')])
const newer = session('Recent work', '/Users/dev/acme-web', [userMessage('b')])
const newest = session('Newest work', '/Users/dev/acme-web', [userMessage('c')])

test.describe('new session', () => {
  test.use({
    scenario: {
      sessions: [older, newer, newest],
      validDirectories: ['/Users/dev/fresh-project'],
    },
  })

  test('recent Workspaces come from existing Sessions, newest first, deduplicated', async ({
    page,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session' }).click()
    const form = page.getByRole('region', { name: 'New session' })
    const recent = form.getByRole('list').getByRole('button')
    await expect(recent).toHaveCount(2)
    await expect(recent.nth(0)).toContainText('acme-web')
    await expect(recent.nth(0)).toContainText('/Users/dev/acme-web')
    await expect(recent.nth(1)).toContainText('billing-service')
  })

  test('choosing a recent Workspace creates and opens a Session there', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session' }).click()
    await page
      .getByRole('region', { name: 'New session' })
      .getByRole('button', { name: /billing-service/ })
      .click()

    const created = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(created.params).toMatchObject({ cwd: '/Users/dev/billing-service' })
    await expect(
      page.getByRole('region', { name: 'New session' }).getByRole('heading', { level: 2 }),
    ).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeEnabled()
    expect(new URL(page.url()).hash).toMatch(/^#\/s\//)

    // The Session appears in the sidebar under its Workspace.
    const billing = (await openSidebar()).getByRole('region', { name: 'billing-service' })
    await expect(billing.getByRole('button', { name: /New session/ })).toBeVisible()
    await expect(billing.getByRole('button', { name: /New session/ })).toHaveAttribute(
      'aria-current',
      'page',
    )
  })

  test('a typed path is validated by the Daemon', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session' }).click()
    const path = page.getByRole('textbox', { name: 'Workspace path' })
    const start = page.getByRole('button', { name: 'Start' })
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
