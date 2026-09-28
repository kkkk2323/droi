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
          name: 'adyen',
          description: 'Payment processing for Adyen',
          type: 'stdio',
          command: 'npx',
          args: ['-y', '@adyen/mcp', '--adyenApiKey=ADYEN_API_KEY'],
          note: 'Replace ADYEN_API_KEY before use.',
        },
        {
          name: 'github',
          description: 'GitHub issues and pull requests',
          type: 'http',
          url: 'https://api.githubcopilot.com/mcp/',
        },
        {
          name: 'linear',
          description: 'Already added, so not offered',
          type: 'http',
          url: 'https://mcp.linear.app/mcp',
        },
      ],
    },
  })

  test('a skill goes off from the composer and leaves the "/" menu', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    // The button in the composer's footer counts what the Session has; the
    // organization's switch already shows one skill off.
    const entry = page.getByRole('button', { name: 'Skills and MCP servers' })
    await expect(entry).toHaveText(/Skills 3\/4.*MCP 3/)
    await expect(entry).toHaveAttribute('title', /4 skills, 1 disabled[\s\S]*files · Connected/)
    await entry.click()
    const skills = page.getByRole('tabpanel')
    const review = skills.getByRole('listitem', { name: 'review' })
    await expect(skills.getByRole('region', { name: 'Built-in' })).toContainText('review')
    await expect(skills.getByRole('region', { name: 'Project' })).toContainText('deploy')
    // A personal skill is managed by its files; one the organization turned off is read-only.
    await expect(skills.getByRole('listitem', { name: 'mine' }).getByRole('switch')).toHaveCount(0)
    const locked = skills.getByRole('listitem', { name: 'locked' })
    await expect(locked).toContainText('Disabled by organization')
    await expect(locked.getByRole('switch')).toBeDisabled()
    await expect(locked.getByRole('switch')).not.toBeChecked()

    // A built-in skill is switched for the user, a project skill for its project.
    await review.getByRole('switch', { name: 'review enabled' }).click()
    const request = await fakeDaemon.waitForRequest('daemon.set_skill_disabled')
    expect(request.params).toMatchObject({
      skillName: 'review',
      disabled: true,
      settingsLevel: 'user',
    })
    await expect(review).toContainText('Disabled')
    await expect(review.getByRole('switch')).not.toBeChecked()
    await skills
      .getByRole('listitem', { name: 'deploy' })
      .getByRole('switch', { name: 'deploy enabled' })
      .click()
    const project = await fakeDaemon.waitForRequest('daemon.set_skill_disabled', 2)
    expect(project.params).toMatchObject({
      skillName: 'deploy',
      disabled: true,
      settingsLevel: 'project',
    })
    await page.getByRole('button', { name: 'Close' }).click()
    await expect(entry).toHaveText(/Skills 1\/4/)

    await page.getByRole('textbox', { name: 'Message' }).fill('/')
    const list = page.getByRole('listbox', { name: 'Commands and skills' })
    await expect(list.getByRole('option')).toHaveText([/compact/, /mine/])
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
    // The catalogue is a view of its own: search, then one click adds.
    await page.getByRole('button', { name: 'Add server' }).click()
    const catalogue = page.getByRole('list', { name: 'Catalogue' })
    const search = page.getByRole('searchbox', { name: 'Search the catalogue' })
    await expect(search).toBeFocused()
    await expect(catalogue.getByRole('listitem')).toHaveText([/adyen/, /github/])
    await search.fill('pull request')
    await expect(catalogue.getByRole('listitem')).toHaveText([/github/])
    await search.fill('nothing like it')
    await expect(
      page.getByText('Nothing in the catalogue matches “nothing like it”.'),
    ).toBeVisible()
    await search.fill('git')
    await catalogue.getByRole('button', { name: 'Add github' }).click()
    const added = await fakeDaemon.waitForRequest('daemon.add_mcp_server')
    expect(added.params).toMatchObject({
      name: 'github',
      type: 'http',
      url: 'https://api.githubcopilot.com/mcp/',
    })
    const list = page.getByRole('list', { name: 'MCP servers' })
    await expect(list.getByRole('listitem', { name: 'github' })).toBeVisible()
    await expect(catalogue).toHaveCount(0)

    // An entry with a value to fill in opens the form, filled, with its note.
    await page.getByRole('button', { name: 'Add server' }).click()
    await catalogue.getByRole('button', { name: 'Set up adyen' }).click()
    const form = page.getByRole('form', { name: 'Add MCP server' })
    await expect(form).toContainText('Set up adyen')
    await expect(form).toContainText('Replace ADYEN_API_KEY before use.')
    const args = form.getByRole('textbox', { name: 'Arguments' })
    await expect(args).toHaveValue('-y @adyen/mcp --adyenApiKey=ADYEN_API_KEY')
    await args.fill('-y @adyen/mcp --adyenApiKey=test_key')
    await form.getByRole('button', { name: 'Add server' }).click()
    const setUp = await fakeDaemon.waitForRequest('daemon.add_mcp_server', 2)
    expect(setUp.params).toMatchObject({
      name: 'adyen',
      command: 'npx',
      args: ['-y', '@adyen/mcp', '--adyenApiKey=test_key'],
    })
    await expect(list.getByRole('listitem', { name: 'adyen' })).toBeVisible()

    // Back leads from the form to the catalogue and from there to the list.
    await page.getByRole('button', { name: 'Add server' }).click()
    await page.getByRole('button', { name: 'Add a server by hand' }).click()
    await expect(form).toContainText('Add a server by hand')
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(search).toBeVisible()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(list).toBeVisible()

    await page.getByRole('button', { name: 'Add server' }).click()
    await page.getByRole('button', { name: 'Add a server by hand' }).click()
    await form.getByRole('button', { name: 'Add server' }).click()
    await expect(form.getByRole('alert')).toContainText('name')
    await form.getByRole('textbox', { name: 'Server name' }).fill('fs')
    await form.getByRole('textbox', { name: 'Command' }).fill('npx')
    await form
      .getByRole('textbox', { name: 'Arguments' })
      .fill('-y @modelcontextprotocol/server-filesystem .')
    await form.getByRole('button', { name: 'Add server' }).click()
    const byHand = await fakeDaemon.waitForRequest('daemon.add_mcp_server', 3)
    expect(byHand.params).toMatchObject({
      name: 'fs',
      type: 'stdio',
      command: 'npx',
      args: ['-y', '@modelcontextprotocol/server-filesystem', '.'],
    })
    await expect(list.getByRole('listitem', { name: 'fs' })).toBeVisible()
  })
})
