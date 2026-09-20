import { drawerGone, expect, test } from './fixtures'
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

    const model = page.getByRole('button', { name: 'Model' })
    const effort = page.getByRole('combobox', { name: 'Reasoning effort' })
    const autonomy = page.getByRole('combobox', { name: 'Autonomy' })
    await expect(model).toHaveText('Claude Opus 4.1')
    await expect(effort).toHaveText('Medium')
    await expect(autonomy).toHaveText('Low autonomy')

    // Models come from the Daemon's list for this Session; the picker opens on all of them.
    await model.click()
    const picker = page.getByRole('dialog', { name: 'Choose a model' })
    const models = picker.getByRole('listbox', { name: 'Models' })
    await expect(models.getByRole('option')).toHaveCount(3)
    await expect(models.getByRole('option').nth(0)).toContainText('Auto Model')
    await expect(picker.getByRole('searchbox', { name: 'Search models' })).toBeFocused()

    // The rail filters by brand; clicking the active brand again shows everything.
    const rail = picker.getByRole('toolbar', { name: 'Filter models' })
    await expect(rail.getByRole('button')).toHaveCount(4) // Favorites, Anthropic, OpenAI, Other
    await rail.getByRole('button', { name: 'OpenAI' }).click()
    await expect(models.getByRole('option')).toHaveCount(1)
    await expect(models.getByRole('option').first()).toContainText('GPT-5')
    await rail.getByRole('button', { name: 'OpenAI' }).click()
    await expect(models.getByRole('option')).toHaveCount(3)

    // Searching ignores the filter and matches label, id or brand.
    await rail.getByRole('button', { name: 'Anthropic' }).click()
    await picker.getByRole('searchbox', { name: 'Search models' }).fill('gpt')
    await expect(models.getByRole('option')).toHaveCount(1)

    // Favorites: star a model, and the Favorites tab lists it.
    await models.getByRole('button', { name: 'Star GPT-5' }).click()
    await picker.getByRole('searchbox', { name: 'Search models' }).fill('')
    await rail.getByRole('button', { name: 'Favorites' }).click()
    await expect(models.getByRole('option')).toHaveCount(1)
    await expect(models.getByRole('option').first()).toContainText('GPT-5')

    await models.getByRole('option', { name: /GPT-5/ }).click()
    await expect(picker).toBeHidden()
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
    await expect(model).toHaveText('GPT-5')

    // GPT-5 offers different efforts; the list follows the model.
    await effort.click()
    const listbox = page.getByRole('listbox')
    await expect(listbox.getByRole('option')).toHaveText(['Low', 'Medium', 'High', 'Extra high'])
    await listbox.getByRole('option', { name: 'Extra high' }).click()
    await expect
      .poll(
        () =>
          fakeDaemon.requests.filter((r) => r.method === 'daemon.update_session_settings').at(-1)
            ?.params,
      )
      .toMatchObject({
        reasoningEffort: 'xhigh',
      })
    await expect(effort).toHaveText('Extra high')

    await autonomy.click()
    await listbox.getByRole('option', { name: 'High autonomy' }).click()
    await expect
      .poll(
        () =>
          fakeDaemon.requests.filter((r) => r.method === 'daemon.update_session_settings').at(-1)
            ?.params,
      )
      .toMatchObject({
        autonomyLevel: 'high',
      })
    await expect(autonomy).toHaveText('High autonomy')
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

  test('archiving removes the Session; the Settings preference shows it again', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
    openSidebar,
  }) => {
    await openClient()
    await pickSession(/Second session/)
    await expect(page.getByRole('heading', { level: 2, name: 'Second session' })).toBeVisible()
    let sidebar = await openSidebar()
    await sidebar.getByRole('button', { name: /Second session/ }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Archive' }).click()

    const archiveRequest = await fakeDaemon.waitForRequest('daemon.archive_session')
    expect(archiveRequest.params).toMatchObject({ sessionId: second.sessionId })
    // The open Session was the one archived, so the view goes home.
    await expect(page.getByText(/Select a session|Open the sessions list/)).toBeVisible()
    await drawerGone(page)
    sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Second session/ })).toHaveCount(0)
    await expect(sidebar.getByRole('button', { name: /First session/ })).toBeVisible()

    // The preference lives in Settings > General and is available to every Client.
    await sidebar.getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('switch', { name: 'Show archived sessions' }).check()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page.getByText(/Select a session|Open the sessions list/)).toBeVisible()
    sidebar = await openSidebar()
    const archived = sidebar.getByRole('button', { name: /Second session/ })
    await expect(archived).toBeVisible()
    await expect(archived.getByLabel('Archived')).toBeVisible()
    const listed = fakeDaemon.requests
      .filter((r) => r.method === 'daemon.list_available_sessions')
      .at(-1)
    expect(listed?.params).toMatchObject({ includeArchived: true })

    // The same menu brings it back.
    await archived.click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Unarchive' }).click()
    await fakeDaemon.waitForRequest('daemon.unarchive_session')
    await expect(archived.getByLabel('Archived')).toHaveCount(0)
  })

  test('a Session can be archived from the sidebar without opening it', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const sidebar = await openSidebar()
    await sidebar.getByRole('button', { name: /Second session/ }).click({ button: 'right' })
    const menu = page.getByRole('menu', { name: 'Actions for Second session' })
    await menu.getByRole('menuitem', { name: 'Archive' }).click()

    const archived = await fakeDaemon.waitForRequest('daemon.archive_session')
    expect(archived.params).toMatchObject({ sessionId: second.sessionId })
    await expect(menu).toHaveCount(0)
    await expect(sidebar.getByRole('button', { name: /Second session/ })).toHaveCount(0)
    await expect(sidebar.getByRole('button', { name: /First session/ })).toBeVisible()
  })
})
