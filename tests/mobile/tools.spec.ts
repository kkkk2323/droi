import { session, userMessage } from '../fake-daemon/scenario'
import { expect, pairPhone, pickSession, test } from './fixtures'

const chat = session('Chat', '/Users/dev/acme-web', [userMessage('hi')])

test.describe('skills and MCP servers', () => {
  test.use({
    scenario: {
      sessions: [chat],
      skills: [
        { name: 'review', description: 'Review a branch', location: 'builtin' },
        { name: 'mine', description: 'My own', location: 'personal' },
        { name: 'locked', location: 'builtin', disabledAt: ['org'] },
      ],
      mcpServers: [
        { name: 'files', type: 'stdio', tools: [{ name: 'read_file' }, { name: 'write_file' }] },
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
          description: 'GitHub',
          type: 'http',
          url: 'https://api.githubcopilot.com/mcp/',
        },
      ],
    },
  })

  test('a skill is switched off and comes back', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
    const sheet = page.getByRole('dialog', { name: 'Skills and MCP servers' })
    const review = sheet.getByRole('listitem', { name: 'review' })
    await expect(sheet.getByRole('listitem', { name: 'mine' }).getByRole('switch')).toHaveCount(0)
    const locked = sheet.getByRole('listitem', { name: 'locked' })
    await expect(locked).toContainText('Disabled by organization')
    await expect(locked.getByRole('switch')).toBeDisabled()
    const toggle = review.getByRole('switch', { name: 'review enabled' })
    await toggle.click()
    const request = await fakeDaemon.waitForRequest('daemon.set_skill_disabled')
    expect(request.params).toMatchObject({
      skillName: 'review',
      disabled: true,
      settingsLevel: 'user',
    })
    await expect(review).toContainText('Disabled')
    await expect(toggle).not.toBeChecked()
    await toggle.click()
    await fakeDaemon.waitForRequest('daemon.set_skill_disabled', 2)
    await expect(review).not.toContainText('Disabled')
    await expect(toggle).toBeChecked()
  })

  test('servers switch, show tools, ask to sign in on the computer, and are added', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
    const sheet = page.getByRole('dialog', { name: 'Skills and MCP servers' })
    await sheet.getByRole('tab', { name: 'MCP servers' }).click()
    const list = sheet.getByRole('list', { name: 'MCP servers' })
    const files = list.getByRole('listitem', { name: 'files' })
    await expect(files.getByRole('img', { name: 'Connected' })).toBeVisible()
    await expect(
      list.getByRole('listitem', { name: 'company-wiki' }).getByRole('switch'),
    ).toBeDisabled()

    await files.getByRole('switch', { name: 'files enabled' }).click()
    const toggled = await fakeDaemon.waitForRequest('daemon.toggle_mcp_server')
    expect(toggled.params).toMatchObject({ serverName: 'files', enabled: false })
    await expect(files.getByRole('img', { name: 'Disabled' })).toBeVisible()
    await files.getByRole('switch', { name: 'files enabled' }).click()
    await expect(files.getByRole('img', { name: 'Connected' })).toBeVisible()

    await files.getByRole('button', { name: '2 tools' }).click()
    await files
      .getByRole('list', { name: 'files tools' })
      .getByRole('switch', { name: 'write_file enabled' })
      .click()
    const tool = await fakeDaemon.waitForRequest('daemon.toggle_mcp_tool')
    expect(tool.params).toMatchObject({
      serverName: 'files',
      toolName: 'write_file',
      enabled: false,
    })

    const linear = list.getByRole('listitem', { name: 'linear' })
    await linear.getByRole('button', { name: 'Sign in' }).click()
    const notice = sheet.getByRole('status', { name: 'Sign in to linear' })
    await expect(notice).toContainText('on the computer running Droi')
    await notice.getByRole('button', { name: 'Copy sign-in link' }).click()
    await expect(notice.getByRole('button', { name: 'Link copied' })).toBeVisible()
    await notice.getByRole('button', { name: 'Cancel' }).click()
    await expect(notice).toHaveCount(0)

    // The catalogue is a view of its own: search, then one tap adds.
    await sheet.getByRole('button', { name: 'Add server' }).click()
    const catalogue = sheet.getByRole('list', { name: 'Catalogue' })
    await sheet.getByRole('textbox', { name: 'Search the catalogue' }).fill('git')
    await expect(catalogue.getByRole('listitem')).toHaveCount(1)
    await catalogue.getByRole('button', { name: 'Add github' }).click()
    const added = await fakeDaemon.waitForRequest('daemon.add_mcp_server')
    expect(added.params).toMatchObject({ name: 'github', type: 'http' })
    await expect(list.getByRole('listitem', { name: 'github' })).toBeVisible()
    await expect(catalogue).toHaveCount(0)

    // An entry with a value to fill in opens the form, filled, with its note.
    await sheet.getByRole('button', { name: 'Add server' }).click()
    await catalogue.getByRole('button', { name: 'Set up adyen' }).click()
    const form = sheet.getByRole('form', { name: 'Add MCP server' })
    await expect(form).toContainText('Replace ADYEN_API_KEY before use.')
    await expect(form.getByRole('textbox', { name: 'Arguments' })).toHaveValue(
      '-y @adyen/mcp --adyenApiKey=ADYEN_API_KEY',
    )
    await form.getByRole('button', { name: 'Back' }).click()
    await sheet.getByRole('button', { name: 'Add a server by hand' }).click()
    await expect(form).toContainText('Add a server by hand')
    await expect(form.getByRole('textbox', { name: 'Server name' })).toHaveValue('')
  })
})
