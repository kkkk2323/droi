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
    await expect(sheet.getByRole('listitem', { name: 'mine' }).getByRole('button')).toHaveCount(0)
    await expect(sheet.getByRole('listitem', { name: 'locked' })).toContainText(
      'Disabled by organization',
    )
    await review.getByRole('button', { name: 'Disable across all projects' }).click()
    const request = await fakeDaemon.waitForRequest('daemon.set_skill_disabled')
    expect(request.params).toMatchObject({
      skillName: 'review',
      disabled: true,
      settingsLevel: 'user',
    })
    await expect(review).toContainText('Disabled')
    await review.getByRole('button', { name: 'Enable' }).click()
    await fakeDaemon.waitForRequest('daemon.set_skill_disabled', 2)
    await expect(review).not.toContainText('Disabled')
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

    await sheet.getByRole('button', { name: 'Add server' }).click()
    const form = sheet.getByRole('form', { name: 'Add MCP server' })
    await form.getByRole('list', { name: 'Catalogue' }).getByRole('button', { name: 'Use' }).click()
    await expect(form.getByRole('textbox', { name: 'Server name' })).toHaveValue('github')
    await form.getByRole('button', { name: 'Add server' }).click()
    const added = await fakeDaemon.waitForRequest('daemon.add_mcp_server')
    expect(added.params).toMatchObject({ name: 'github', type: 'http' })
    await expect(list.getByRole('listitem', { name: 'github' })).toBeVisible()
    await expect(form).toHaveCount(0)
  })
})
