import type { Locator, Page } from '@playwright/test'
import { drawerGone, expect, test } from './fixtures'
import { session, userMessage } from '../fake-daemon/scenario'

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
    // Factory's usage multiplier for each model, as the Factory App shows it.
    await expect(models.getByRole('option', { name: /Claude Opus 4\.1/ })).toContainText('1.6×')
    await expect(models.getByRole('option', { name: /GPT-5/ })).toContainText('0.8×')
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

  test('Back from Settings returns to the Session it was opened from, even after a reload', async ({
    page,
    openClient,
    pickSession,
    openSidebar,
  }) => {
    await openClient()
    await pickSession(/Second session/)
    await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page).toHaveURL(new RegExp(second.sessionId))
    await expect(page.getByRole('heading', { level: 2, name: 'Second session' })).toBeVisible()

    // A reload on Settings forgets where it came from; the last Session stands in.
    await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
    await page.reload()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page).toHaveURL(new RegExp(second.sessionId))
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
    await expect(page.getByRole('region', { name: 'New session' })).toBeVisible()
    await drawerGone(page)
    sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Second session/ })).toHaveCount(0)
    await expect(sidebar.getByRole('button', { name: /First session/ })).toBeVisible()

    // The preference lives in Settings > General and is available to every Client.
    await sidebar.getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('switch', { name: 'Show archived sessions' }).check()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page.getByRole('region', { name: 'New session' })).toBeVisible()
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

test.describe('session defaults', () => {
  test.use({ scenario: { sessions: [first] } })

  async function openDefaults(page: Page, openSidebar: () => Promise<Locator>) {
    await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('button', { name: 'Session defaults' }).click()
    await expect(page.getByRole('heading', { level: 2, name: 'Session defaults' })).toBeVisible()
  }

  async function pick(page: Page, select: string, option: string | RegExp) {
    await page.getByRole('combobox', { name: select, exact: true }).click()
    await page.getByRole('listbox').getByRole('option', { name: option }).click()
    await expect(page.getByRole('listbox')).toHaveCount(0)
  }

  test('each change goes to the Daemon at once and outlasts a reload', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await openDefaults(page, openSidebar)
    const saved = () =>
      fakeDaemon.requests.filter((r) => r.method === 'daemon.update_session_defaults').at(-1)
        ?.params

    const model = page.getByRole('combobox', { name: 'Default model' })
    const effort = page.getByRole('combobox', { name: 'Default reasoning level' })
    await expect(model).toHaveText('Auto Model')
    await expect(effort).toHaveText('None')
    // GPT-5 has no "None"; its first level comes with it.
    await pick(page, 'Default model', 'GPT-5')
    await expect.poll(saved).toEqual({ modelId: 'gpt-5', reasoningEffort: 'low' })
    await expect(effort).toHaveText('Low')
    await effort.click()
    await expect(page.getByRole('listbox').getByRole('option')).toHaveText([
      'Low',
      'Medium',
      'High',
      'Extra high',
    ])
    await page.keyboard.press('Escape')

    await pick(page, 'Default interaction mode', 'Spec')
    await expect.poll(saved).toEqual({ interactionMode: 'spec' })
    await pick(page, 'Default autonomy level', /^High/)
    await expect.poll(saved).toEqual({ autonomyLevel: 'high' })

    await pick(page, 'Spec mode model', 'Claude Opus 4.1')
    await expect.poll(saved).toEqual({
      specModeModelId: 'claude-opus-4-1',
      specModeReasoningEffort: null,
    })
    await pick(page, 'Spec mode model', 'Same as main')
    await expect.poll(saved).toEqual({ specModeModelId: null, specModeReasoningEffort: null })
    await pick(page, 'Spec save folder', 'Project')
    await expect.poll(saved).toEqual({ specSaveDir: '.factory/docs' })

    await pick(page, 'Compaction token limit', '500K')
    await expect.poll(saved).toEqual({ compactionTokenLimit: 500_000 })
    await pick(page, 'Add a model limit', 'GPT-5')
    await expect.poll(saved).toEqual({ compactionTokenLimitPerModel: { 'gpt-5': 500_000 } })
    const limits = page.getByRole('list', { name: 'Model compaction limits' })
    await limits.getByRole('button', { name: 'Remove the GPT-5 limit' }).click()
    await expect.poll(saved).toEqual({ compactionTokenLimitPerModel: {} })
    await page.getByRole('switch', { name: 'Compact automatically' }).click()
    await expect.poll(saved).toEqual({ compactionThresholdCheckEnabled: false })

    // The Daemon replaces the tier settings whole, so every change sends all tiers.
    await pick(page, 'Light task model', 'GPT-5')
    await expect.poll(saved).toEqual({ subagentModelSettings: { lightModel: 'gpt-5' } })
    await pick(page, 'Heavy task model', 'Claude Opus 4.1')
    await expect.poll(saved).toEqual({
      subagentModelSettings: { lightModel: 'gpt-5', heavyModel: 'claude-opus-4-1' },
    })
    await pick(page, 'Light task model', 'Inherit (calling session)')
    await expect.poll(saved).toEqual({ subagentModelSettings: { heavyModel: 'claude-opus-4-1' } })
    await pick(page, 'Subagent autonomy level', 'Medium autonomy')
    await expect.poll(saved).toEqual({ subagentAutonomyLevel: 'medium' })

    // The system prompt belongs to the Desktop Shell; a browser has no such row.
    await expect(page.getByRole('region', { name: 'System prompt' })).toHaveCount(0)

    await page.reload()
    await page.getByRole('button', { name: 'Session defaults' }).click()
    await expect(model).toHaveText('GPT-5')
    await expect(page.getByRole('combobox', { name: 'Default interaction mode' })).toHaveText(
      'Spec',
    )
    await expect(page.getByRole('combobox', { name: 'Spec save folder' })).toHaveText('Project')
    await expect(page.getByRole('combobox', { name: 'Heavy task model' })).toHaveText(
      'Claude Opus 4.1',
    )
    await expect(page.getByRole('switch', { name: 'Compact automatically' })).not.toBeChecked()
  })
})

test.describe('session defaults the organization manages', () => {
  test.use({
    scenario: {
      sessions: [first],
      defaults: { management: { modelId: { disabled: true, source: 'org' } } },
    },
  })

  test('are shown but cannot be changed', async ({ page, openClient, openSidebar }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('button', { name: 'Session defaults' }).click()
    await expect(page.getByRole('combobox', { name: 'Default model' })).toBeDisabled()
    await expect(page.getByText('Set by your organization.')).toBeVisible()
    await expect(page.getByRole('combobox', { name: 'Default reasoning level' })).toBeEnabled()
  })
})
