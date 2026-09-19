import { expect, test } from './fixtures'
import { session, userMessage } from './fake-daemon/scenario'

const first = session('First session', '/Users/dev/acme-web', [userMessage('hi')], {
  settings: { modelId: 'claude-opus-4-1', reasoningEffort: 'medium', autonomyLevel: 'low' },
})
const second = session('Second session', '/Users/dev/acme-web', [userMessage('yo')])

test.describe('session settings', () => {
  test.use({ scenario: { sessions: [first, second] } })

  test('model, reasoning effort and autonomy go to the Daemon and reflect its confirmation', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/First session/)

    const model = page.getByRole('combobox', { name: 'Model' })
    const effort = page.getByRole('combobox', { name: 'Reasoning effort' })
    const autonomy = page.getByRole('combobox', { name: 'Autonomy' })
    await expect(model).toHaveValue('claude-opus-4-1')
    await expect(effort).toHaveValue('medium')
    await expect(autonomy).toHaveValue('low')

    // Models come from the Daemon's list for this Session.
    const modelOptions = await model.locator('option').allTextContents()
    expect(modelOptions).toEqual(['Auto Model', 'Claude Opus 4.1', 'GPT-5'])

    await model.selectOption('gpt-5')
    await expect
      .poll(
        () =>
          fakeDaemon.requests.filter((r) => r.method === 'daemon.update_session_settings').at(-1)
            ?.params,
      )
      .toMatchObject({
        sessionId: first.sessionId,
        modelId: 'gpt-5',
      })
    await expect(model).toHaveValue('gpt-5')
    // GPT-5 offers different efforts; the list follows the model.
    await expect(effort.locator('option')).toHaveText(['Low', 'Medium', 'High', 'Extra high'])

    await effort.selectOption('xhigh')
    await expect
      .poll(
        () =>
          fakeDaemon.requests.filter((r) => r.method === 'daemon.update_session_settings').at(-1)
            ?.params,
      )
      .toMatchObject({
        reasoningEffort: 'xhigh',
      })
    await expect(effort).toHaveValue('xhigh')

    await autonomy.selectOption('high')
    await expect
      .poll(
        () =>
          fakeDaemon.requests.filter((r) => r.method === 'daemon.update_session_settings').at(-1)
            ?.params,
      )
      .toMatchObject({
        autonomyLevel: 'high',
      })
    await expect(autonomy).toHaveValue('high')
  })

  test('renaming updates the sidebar once the Daemon confirms', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
    openSidebar,
  }) => {
    await openClient()
    await pickSession(/First session/)
    await page.getByRole('button', { name: 'Rename session' }).click()
    const input = page.getByRole('textbox', { name: 'Session title' })
    await input.fill('Renamed session')
    await input.press('Enter')

    await fakeDaemon.waitForRequest('daemon.rename_session')
    await expect(page.getByRole('heading', { level: 2, name: 'Renamed session' })).toBeVisible()
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Renamed session/ })).toBeVisible()
    await expect(sidebar.getByRole('button', { name: /First session/ })).toHaveCount(0)
  })

  test('archiving removes the Session; the archived filter shows it again', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
    openSidebar,
  }) => {
    await openClient()
    await pickSession(/Second session/)
    await page.getByRole('button', { name: 'Archive session' }).click()

    await fakeDaemon.waitForRequest('daemon.archive_session')
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Second session/ })).toHaveCount(0)
    await expect(sidebar.getByRole('button', { name: /First session/ })).toBeVisible()
    await expect(page.getByText(/Select a session|Open the sessions list/)).toBeVisible()

    await sidebar.getByRole('checkbox', { name: 'Show archived' }).check()
    const archived = sidebar.getByRole('button', { name: /Second session/ })
    await expect(archived).toBeVisible()
    await expect(archived.getByLabel('Archived')).toBeVisible()
    const listed = fakeDaemon.requests
      .filter((r) => r.method === 'daemon.list_available_sessions')
      .at(-1)
    expect(listed?.params).toMatchObject({ includeArchived: true })
  })
})
