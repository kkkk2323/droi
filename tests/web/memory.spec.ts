import type { Locator, Page } from '@playwright/test'
import { expect, openLocalClient, shellRecord, test } from './fixtures'
import { exposeShellMemory } from './memory-fixture'
import { session, userMessage } from '../fake-daemon/scenario'
import { structuredTurn } from '../fake-daemon/turns'
import type { RecordedRequest } from '../fake-daemon/fake-daemon'

const first = session('First session', '/Users/dev/app', [userMessage('hello')])

function isMemorySessionStart(request: RecordedRequest): boolean {
  const params = request.params as { tags?: Array<{ name: string }> }
  return (
    request.method === 'daemon.initialize_session' &&
    (params.tags ?? []).some((t) => t.name === 'droi.memory')
  )
}

async function openMemory(page: Page, openSidebar: () => Promise<Locator>) {
  await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('button', { name: 'Memory' }).click()
  await expect(page.getByRole('heading', { level: 2, name: 'Memory' })).toBeVisible()
}

test.describe('Settings → Memory', () => {
  test.use({ scenario: { sessions: [first] } })

  test('the Beta switch asks before it restarts the Daemon', async ({
    page,
    fakeDaemon,
    openSidebar,
  }) => {
    await openLocalClient(page, fakeDaemon)
    await openMemory(page, openSidebar)
    await expect(page.getByText('Beta', { exact: true })).toBeVisible()
    const memory = page.getByRole('switch', { name: 'Memory' })
    await expect(memory).not.toBeChecked()

    await memory.click()
    const confirm = page.getByRole('alertdialog', { name: 'Turn Memory on' })
    await expect(confirm).toContainText('Sessions that are working stop')
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    await expect(confirm).toHaveCount(0)
    await expect(memory).not.toBeChecked()
    expect((await shellRecord(page)).settingsUpdates).toEqual([])

    await memory.click()
    await page
      .getByRole('alertdialog', { name: 'Turn Memory on' })
      .getByRole('button', { name: 'Restart Daemon' })
      .click()
    await expect(memory).toBeChecked()
    await expect(page.getByRole('alertdialog')).toHaveCount(0)
    const record = await shellRecord(page)
    expect(record.settingsUpdates).toEqual([{ memoryEnabled: true }])
    expect(record.daemonRestarts).toBe(1)

    await memory.click()
    await page
      .getByRole('alertdialog', { name: 'Turn Memory off' })
      .getByRole('button', { name: 'Restart Daemon' })
      .click()
    await expect(memory).not.toBeChecked()
    expect((await shellRecord(page)).daemonRestarts).toBe(2)
  })

  test('the Memory model comes from the Daemon’s model list', async ({
    page,
    fakeDaemon,
    openSidebar,
  }) => {
    await openLocalClient(page, fakeDaemon, { memoryEnabled: true })
    await openMemory(page, openSidebar)
    const picker = page.getByRole('button', { name: 'Memory model', exact: true })
    await expect(picker).toHaveText('glm-5.3-flash')
    await picker.click()
    const dialog = page.getByRole('dialog', { name: 'Choose a model' })
    await dialog
      .getByRole('listbox', { name: 'Models' })
      .getByRole('option', { name: /GPT-5/ })
      .click()
    await expect(dialog).toBeHidden()
    await expect(picker).toHaveText('GPT-5')
    const record = await shellRecord(page)
    expect(record.settingsUpdates).toEqual([{ memoryModel: 'gpt-5' }])
    expect(record.daemonRestarts).toBe(0)
  })

  test('a Remote Client has no Memory tab', async ({ page, openClient, openSidebar }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'Settings' }).click()
    await expect(page.getByRole('heading', { level: 2, name: 'General' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Memory' })).toHaveCount(0)
  })
})

test.describe('Memory Sessions', () => {
  const memorySession = session(
    'Memory: consolidate app / convention',
    '/Users/dev/app',
    [userMessage('{}')],
    {
      tags: [{ name: 'droi.memory' }],
    },
  )
  test.use({
    scenario: {
      sessions: [first, memorySession],
      // The Memory Session keeps every entry it was sent.
      handlers: {
        'daemon.add_user_message': structuredTurn((text) => {
          const { entries } = JSON.parse(text) as { entries: Array<{ id: string }> }
          return { keep: entries.map((e) => e.id), rewrite: [], remove: [], merge: [] }
        }),
      },
    },
  })

  test('are not listed', async ({ openClient, openSidebar }) => {
    await openClient()
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /First session/ })).toBeVisible()
    await expect(sidebar.getByRole('button', { name: /Memory: consolidate/ })).toHaveCount(0)
  })

  test('Consolidate runs one, tagged and private, and archives it afterwards', async ({
    page,
    fakeDaemon,
    openSidebar,
  }) => {
    const memory = await exposeShellMemory(page, fakeDaemon, [
      {
        slot: { scope: 'project', workspace: '/Users/dev/app' },
        category: 'convention',
        text: 'uses pnpm',
      },
      {
        slot: { scope: 'project', workspace: '/Users/dev/app' },
        category: 'convention',
        text: 'tests beside code',
      },
      { slot: { scope: 'global' }, category: 'preference', text: 'terse answers' },
    ])
    try {
      await openLocalClient(page, fakeDaemon, { memoryEnabled: true })
      await openMemory(page, openSidebar)
      const memories = page.getByRole('list', { name: 'Memories' })
      const app = memories.getByRole('listitem', { name: '/Users/dev/app' })
      await expect(app).toContainText('2 entries')
      await expect(app).toContainText('never consolidated')
      const global = memories.getByRole('listitem', { name: 'Global Memory' })
      await expect(global).toContainText('1 entry')
      // One entry has nothing to consolidate.
      await expect(global.getByRole('button', { name: 'Consolidate Global Memory' })).toBeDisabled()

      await app.getByRole('button', { name: 'Consolidate /Users/dev/app' }).click()
      await expect(app.getByRole('status')).toHaveText('Consolidated 1 category.')
      await expect(app).toContainText('consolidated ')
      await expect(app).not.toContainText('never consolidated')

      // The home page opened a Draft Session of its own; the Memory Session is the tagged one.
      await expect.poll(() => fakeDaemon.requests.filter(isMemorySessionStart).length).toBe(1)
      const started = fakeDaemon.requests.find(isMemorySessionStart)!
      expect(started.params).toMatchObject({
        title: 'Memory: consolidate app / convention',
        tags: expect.arrayContaining([{ name: 'droi.memory' }]),
        privacyLevel: 'private',
        modelId: 'glm-5.3-flash',
        autoRejectPermissionRequests: true,
        systemPrompt: {
          type: 'preset',
          preset: 'droid',
          append: expect.stringContaining('consolidating'),
        },
        structuredOutputFormat: { type: 'json_schema', schema: expect.any(Object) },
      })
      const created = fakeDaemon.scenario.sessions.find(
        (s) =>
          s.sessionId !== memorySession.sessionId && s.tags?.some((t) => t.name === 'droi.memory'),
      )
      const archived = await fakeDaemon.waitForRequest('daemon.archive_session')
      expect(archived.params).toMatchObject({ sessionId: created?.sessionId })
      expect(created?.archivedAt).toBeDefined()
      // Nor does the Memory Session show up in the sidebar.
      await page.getByRole('button', { name: 'Back' }).click()
      const sidebar = await openSidebar()
      await expect(sidebar.getByRole('button', { name: /First session/ })).toBeVisible()
      await expect(sidebar.getByRole('button', { name: /Memory: consolidate/ })).toHaveCount(0)
    } finally {
      memory.dispose()
    }
  })

  test('the folder and the prompts are one click away', async ({
    page,
    fakeDaemon,
    openSidebar,
  }) => {
    await openLocalClient(page, fakeDaemon, { memoryEnabled: true })
    await openMemory(page, openSidebar)
    await page.getByRole('button', { name: 'Open memory folder' }).click()
    await page.getByRole('button', { name: 'Reset prompts to default' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'back to Droi' })).toBeVisible()
    const record = await shellRecord(page)
    expect(record.memoryFolderOpened).toBe(1)
    expect(record.memoryPromptResets).toBe(1)
  })
})

test.describe('a large Project Memory', () => {
  test.use({ scenario: { sessions: [first] } })

  test('gets a corner card once, which leads to Settings → Memory', async ({
    page,
    fakeDaemon,
  }) => {
    const chunk = 'x'.repeat(50_000)
    const memory = await exposeShellMemory(
      page,
      fakeDaemon,
      Array.from({ length: 5 }, (_, i) => ({
        slot: { scope: 'project' as const, workspace: '/Users/dev/app' },
        category: 'insight' as const,
        text: `${i}${chunk}`,
      })),
    )
    try {
      await openLocalClient(page, fakeDaemon, { memoryEnabled: true })
      const card = page.getByRole('status', { name: 'Memory is large' })
      await expect(card).toContainText('app’s Memory is large')
      await card.getByRole('button', { name: 'Dismiss' }).click()
      await expect(card).toHaveCount(0)
      await page.reload()
      await expect(
        page
          .getByRole('button', { name: 'Start session' })
          .or(page.getByRole('textbox', { name: 'Message' }))
          .first(),
      ).toBeVisible()
      await expect(card).toHaveCount(0)
    } finally {
      memory.dispose()
    }
  })

  test('says nothing while Memory is off', async ({ page, fakeDaemon }) => {
    const memory = await exposeShellMemory(page, fakeDaemon, [
      {
        slot: { scope: 'project', workspace: '/Users/dev/app' },
        category: 'insight',
        text: 'x'.repeat(210_000),
      },
    ])
    try {
      await openLocalClient(page, fakeDaemon)
      await expect(page.getByRole('textbox', { name: 'Message' }).first()).toBeVisible()
      await expect(page.getByRole('status', { name: 'Memory is large' })).toHaveCount(0)
    } finally {
      memory.dispose()
    }
  })
})

test.describe('the MCP panel', () => {
  test.use({
    scenario: {
      sessions: [first],
      mcpServers: [
        { name: 'files', type: 'stdio', tools: [{ name: 'read_file' }] },
        {
          name: 'droi-memory',
          type: 'stdio',
          tools: [{ name: 'memory_search' }, { name: 'memory_add' }],
        },
      ],
    },
  })

  test('shows Droi’s Memory Server beside the user’s, with no switch', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/First session/)
    await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
    await page.getByRole('tab', { name: 'MCP servers' }).click()
    const list = page.getByRole('list', { name: 'MCP servers' })
    const memory = list.getByRole('listitem', { name: 'droi-memory' })
    await expect(memory).toContainText('Droi')
    await expect(memory).toContainText('Settings → Memory')
    await expect(memory.getByRole('switch', { name: 'droi-memory enabled' })).toHaveCount(0)
    await expect(memory.getByRole('button', { name: 'Remove' })).toHaveCount(0)
    await expect(list.getByRole('listitem', { name: 'files' }).getByRole('switch')).toBeEnabled()
    // Its tools are read-only too.
    await memory.getByRole('button', { name: '2 tools' }).click()
    await expect(memory.getByRole('switch', { name: 'memory_add enabled' })).toBeDisabled()
  })
})
