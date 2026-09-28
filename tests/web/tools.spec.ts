import { expect, test } from './fixtures'
import { session, userMessage } from '../fake-daemon/scenario'

const chat = session('Chat', '/Users/dev/acme-web', [userMessage('hi')])

test.describe('skills and MCP servers', () => {
  test.use({
    scenario: {
      sessions: [chat],
      skills: [
        { name: 'review', description: 'Review a branch', location: 'builtin' },
        { name: 'deploy', description: 'Ship it', location: 'project' },
        { name: 'mine', description: 'My own', location: 'personal' },
        { name: 'locked', location: 'builtin', disabledAt: ['org'] },
      ],
      mcpServers: [
        {
          name: 'files',
          type: 'stdio',
          tools: [{ name: 'read_file', description: 'Read a file' }, { name: 'write_file' }],
        },
        { name: 'linear', type: 'http', requiresAuth: true },
        { name: 'company-wiki', type: 'http', source: 'org', toolCount: 3 },
      ],
      mcpRegistry: [
        {
          name: 'github',
          description: 'GitHub issues and pull requests',
          type: 'http',
          url: 'https://api.githubcopilot.com/mcp/',
        },
      ],
    },
  })

  test('a skill goes off for the project or everywhere and leaves the "/" menu', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
    const skills = page.getByRole('tabpanel')
    const review = skills.getByRole('listitem', { name: 'review' })
    await expect(skills.getByRole('region', { name: 'Built-in' })).toContainText('review')
    await expect(skills.getByRole('region', { name: 'Project' })).toContainText('deploy')
    // A personal skill is managed by its files; one the organization turned off is read-only.
    await expect(skills.getByRole('listitem', { name: 'mine' }).getByRole('button')).toHaveCount(0)
    const locked = skills.getByRole('listitem', { name: 'locked' })
    await expect(locked).toContainText('Disabled by organization')
    await expect(locked.getByRole('button')).toHaveCount(0)

    await review.getByRole('button', { name: 'Manage review' }).click()
    await expect(page.getByRole('menuitem')).toHaveText([
      'Disable for this project',
      'Disable across all projects',
    ])
    await page.getByRole('menuitem', { name: 'Disable across all projects' }).click()
    const request = await fakeDaemon.waitForRequest('daemon.set_skill_disabled')
    expect(request.params).toMatchObject({
      skillName: 'review',
      disabled: true,
      settingsLevel: 'user',
    })
    await expect(review).toContainText('Disabled')
    await review.getByRole('button', { name: 'Manage review' }).click()
    await expect(page.getByRole('menuitem')).toHaveText(['Enable'])
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Close' }).click()

    await page.getByRole('textbox', { name: 'Message' }).fill('/')
    const list = page.getByRole('listbox', { name: 'Commands and skills' })
    await expect(list.getByRole('option')).toHaveText([/compact/, /deploy/, /mine/])
  })

  test('servers switch off and on, sign in, show their tools, and can be removed', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
    await page.getByRole('tab', { name: 'MCP servers' }).click()
    const list = page.getByRole('list', { name: 'MCP servers' })
    const files = list.getByRole('listitem', { name: 'files' })
    await expect(files.getByRole('img', { name: 'Connected' })).toBeVisible()
    await expect(files).toContainText('2 tools')

    // The organization's server is policy: no switch, no removal.
    const wiki = list.getByRole('listitem', { name: 'company-wiki' })
    await expect(wiki).toContainText('Organization')
    await expect(wiki.getByRole('switch')).toBeDisabled()
    await expect(wiki.getByRole('button', { name: 'Remove' })).toHaveCount(0)

    await files.getByRole('switch', { name: 'files enabled' }).click()
    const toggled = await fakeDaemon.waitForRequest('daemon.toggle_mcp_server')
    expect(toggled.params).toMatchObject({ serverName: 'files', enabled: false })
    await expect(files.getByRole('img', { name: 'Disabled' })).toBeVisible()
    await files.getByRole('switch', { name: 'files enabled' }).click()
    await expect(files.getByRole('img', { name: 'Connected' })).toBeVisible()

    await files.getByRole('button', { name: '2 tools' }).click()
    const tools = files.getByRole('list', { name: 'files tools' })
    await expect(tools.getByRole('listitem')).toHaveCount(2)
    await tools.getByRole('switch', { name: 'write_file enabled' }).click()
    const tool = await fakeDaemon.waitForRequest('daemon.toggle_mcp_tool')
    expect(tool.params).toMatchObject({
      serverName: 'files',
      toolName: 'write_file',
      enabled: false,
    })
    await expect(tools.getByRole('switch', { name: 'write_file enabled' })).not.toBeChecked()

    // A server that needs signing in offers it; the Daemon names the page to open.
    const linear = list.getByRole('listitem', { name: 'linear' })
    await linear.getByRole('button', { name: 'Sign in' }).click()
    const notice = page.getByRole('status', { name: 'Sign in to linear' })
    await expect(notice.getByRole('link', { name: 'Open sign-in page' })).toHaveAttribute(
      'href',
      /auth\.example\.com.*server=linear/,
    )
    await notice.getByRole('button', { name: 'Cancel' }).click()
    await expect(notice).toHaveCount(0)

    await files.getByRole('button', { name: 'Remove' }).click()
    await files.getByRole('button', { name: 'Remove files' }).click()
    await fakeDaemon.waitForRequest('daemon.remove_mcp_server')
    await expect(files).toHaveCount(0)
  })

  test('a server is added from the catalogue or by hand', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
    await page.getByRole('tab', { name: 'MCP servers' }).click()
    await page.getByRole('button', { name: 'Add server' }).click()
    const form = page.getByRole('form', { name: 'Add MCP server' })
    await form.getByRole('list', { name: 'Catalogue' }).getByRole('button', { name: 'Use' }).click()
    await expect(form.getByRole('textbox', { name: 'Server name' })).toHaveValue('github')
    await expect(form.getByRole('textbox', { name: 'Server URL' })).toHaveValue(
      'https://api.githubcopilot.com/mcp/',
    )
    await form.getByRole('button', { name: 'Add server' }).click()
    const added = await fakeDaemon.waitForRequest('daemon.add_mcp_server')
    expect(added.params).toMatchObject({
      name: 'github',
      type: 'http',
      url: 'https://api.githubcopilot.com/mcp/',
    })
    const list = page.getByRole('list', { name: 'MCP servers' })
    await expect(list.getByRole('listitem', { name: 'github' })).toBeVisible()
    await expect(form).toHaveCount(0)

    await page.getByRole('button', { name: 'Add server' }).click()
    await form.getByRole('button', { name: 'Add server' }).click()
    await expect(form.getByRole('alert')).toContainText('name')
    await form.getByRole('textbox', { name: 'Server name' }).fill('fs')
    await form.getByRole('textbox', { name: 'Command' }).fill('npx')
    await form
      .getByRole('textbox', { name: 'Arguments' })
      .fill('-y @modelcontextprotocol/server-filesystem .')
    await form.getByRole('button', { name: 'Add server' }).click()
    const byHand = await fakeDaemon.waitForRequest('daemon.add_mcp_server', 2)
    expect(byHand.params).toMatchObject({
      name: 'fs',
      type: 'stdio',
      command: 'npx',
      args: ['-y', '@modelcontextprotocol/server-filesystem', '.'],
    })
    await expect(list.getByRole('listitem', { name: 'fs' })).toBeVisible()
  })
})
