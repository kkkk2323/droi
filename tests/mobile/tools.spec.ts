import type { Page } from '@playwright/test'
import { fakeMcpServer } from './fake-kernel/fake-kernel'
import { userEntry } from './fake-kernel/history'
import { startOAuthMcpServer } from './fake-kernel/oauth-mcp'
import { expect, pairPhone, pickSession, standIns, test } from './fixtures'

test.use({
  scenario: {
    sessions: [{ title: 'Chat', cwd: 'acme-web', entries: [userEntry('hi')] }],
    skills: [
      { name: 'review', description: 'Review a branch' },
      { name: 'mine', description: 'My own' },
      { name: 'deploy-check', description: 'Check a deploy', cwd: 'acme-web' },
    ],
    mcpServers: { files: fakeMcpServer(['read_file', 'write_file']) },
    repoMcpServers: { 'acme-web': { wiki: fakeMcpServer(['search_wiki']) } },
  },
})

async function openTools(page: Page, tab: 'Skills' | 'MCP servers') {
  await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
  const sheet = page.getByRole('dialog', { name: 'Skills and MCP servers' })
  await sheet.getByRole('tab', { name: tab }).click()
  return sheet
}

test('skills are grouped by where they live; one is switched off for every Session and back', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  const chat = fakeKernel.session('Chat').conversationId
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Chat/)
  const sheet = await openTools(page, 'Skills')
  await expect(sheet.getByRole('group', { name: 'Project' }).getByRole('listitem')).toHaveText([
    /deploy-check/,
  ])
  await expect(sheet.getByRole('group', { name: 'Personal' }).getByRole('listitem')).toHaveText([
    /mine/,
    /review/,
  ])

  const review = sheet.getByRole('listitem', { name: 'review' })
  const toggle = review.getByRole('switch', { name: 'review enabled' })
  await toggle.click()
  expect(await phoneWire.waitForCall('skills.setDisabled')).toMatchObject({ disabled: true })
  await expect(review).toContainText('Disabled')
  await expect(toggle).not.toBeChecked()
  const listed = async () =>
    (
      await fakeKernel.kernel.call<{ skills: Array<{ name: string; enabled: boolean }> }>(
        'skills.list',
        { id: chat },
      )
    ).skills.find((s) => s.name === 'review')?.enabled
  await expect.poll(listed).toBe(false)

  await toggle.click()
  await phoneWire.waitForCall('skills.setDisabled', 2)
  await expect(review).not.toContainText('Disabled')
  await expect(toggle).toBeChecked()
  await expect.poll(listed).toBe(true)
})

test('a server switches off and on, and lists its tools', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Chat/)
  const sheet = await openTools(page, 'MCP servers')
  const files = sheet
    .getByRole('list', { name: 'MCP servers' })
    .getByRole('listitem', { name: 'files' })
  await expect(files.getByRole('img', { name: 'Connected' })).toBeVisible()

  await files.getByRole('button', { name: '2 tools' }).click()
  await expect(files.getByRole('list', { name: 'files tools' }).getByRole('listitem')).toHaveText([
    'read_file',
    'write_file',
  ])

  await files.getByRole('switch', { name: 'files enabled' }).click()
  expect(await phoneWire.waitForCall('mcp.set')).toEqual({ name: 'files', enabled: false })
  await expect(files.getByRole('img', { name: 'Disabled' })).toBeVisible()
  await files.getByRole('switch', { name: 'files enabled' }).click()
  await expect(files.getByRole('img', { name: 'Connected' })).toBeVisible()
})

test('a repository’s server runs once allowed, and is not the phone’s to switch', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Chat/)
  const sheet = await openTools(page, 'MCP servers')
  const wiki = sheet.getByRole('listitem', { name: 'wiki' })
  await expect(wiki.getByRole('img', { name: 'Waiting for your OK' })).toBeVisible()
  await expect(wiki).toContainText('Project')
  await expect(wiki.getByRole('switch')).toHaveCount(0)
  const ask = wiki.getByRole('status', { name: 'Allow wiki?' })
  await expect(ask).toContainText('mcp-server.mjs')
  await ask.getByRole('button', { name: 'Allow wiki' }).click()
  await expect(wiki.getByRole('img', { name: 'Connected' })).toBeVisible()
  await expect(ask).toHaveCount(0)
})

test('a server that asks for a sign-in gives a link to open on the computer, and connects once used', async ({
  page,
  fakeKernel,
}) => {
  const linear = await startOAuthMcpServer()
  try {
    // Added on the computer.
    await fakeKernel.kernel.call('mcp.set', {
      name: 'linear',
      server: { transport: 'http', url: linear.url },
    })
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    const sheet = await openTools(page, 'MCP servers')
    const row = sheet.getByRole('listitem', { name: 'linear' })
    await row.getByRole('button', { name: 'Sign in' }).click()
    const notice = sheet.getByRole('status', { name: 'Sign in to linear' })
    await expect(notice).toContainText('on the computer running Droi')
    await notice.getByRole('button', { name: 'Copy sign-in link' }).click()
    await expect(notice.getByRole('button', { name: 'Link copied' })).toBeVisible()
    const link = (await standIns(page)).clipboard
    expect(link).toMatch(/^http:\/\/127\.0\.0\.1:\d+\/authorize\?/)

    // The person opens it in the computer's browser; the server sends it back to the Kernel.
    const opened = await fetch(link!)
    expect(opened.ok).toBe(true)
    await expect(row.getByRole('img', { name: 'Connected' })).toBeVisible({ timeout: 10_000 })
    await expect(notice).toHaveCount(0)
  } finally {
    await linear.close()
  }
})

test('a sign-in can be called off', async ({ page, fakeKernel, phoneWire }) => {
  const linear = await startOAuthMcpServer()
  try {
    await fakeKernel.kernel.call('mcp.set', {
      name: 'linear',
      server: { transport: 'http', url: linear.url },
    })
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    const sheet = await openTools(page, 'MCP servers')
    await sheet
      .getByRole('listitem', { name: 'linear' })
      .getByRole('button', { name: 'Sign in' })
      .click()
    const notice = sheet.getByRole('status', { name: 'Sign in to linear' })
    await notice.getByRole('button', { name: 'Cancel' }).click()
    await expect(notice).toHaveCount(0)
    expect(await phoneWire.waitForCall('mcp.authorize.cancel')).toEqual({ name: 'linear' })
  } finally {
    await linear.close()
  }
})

test('a server is added by hand, connects, and is removed', async ({ page, fakeKernel }) => {
  const server = fakeMcpServer(['take_note']) as { command: string; args: string[] }
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Chat/)
  const sheet = await openTools(page, 'MCP servers')
  await sheet.getByRole('button', { name: 'Add server' }).click()
  const form = sheet.getByRole('form', { name: 'Add MCP server' })
  await form.getByRole('button', { name: 'Add server' }).click()
  await expect(form.getByRole('alert')).toHaveText('Enter a server name.')
  await form.getByRole('textbox', { name: 'Server name' }).fill('files')
  await form.getByRole('textbox', { name: 'Command' }).fill(server.command)
  await form.getByRole('textbox', { name: 'Arguments' }).fill(server.args.join(' '))
  await form.getByRole('button', { name: 'Add server' }).click()
  await expect(form.getByRole('alert')).toHaveText('There is already a server named "files".')
  await form.getByRole('textbox', { name: 'Server name' }).fill('notes')
  await form.getByRole('button', { name: 'Add server' }).click()

  const notes = sheet
    .getByRole('list', { name: 'MCP servers' })
    .getByRole('listitem', { name: 'notes' })
  await expect(notes.getByRole('img', { name: 'Connected' })).toBeVisible({ timeout: 10_000 })
  const known = async () =>
    (await fakeKernel.kernel.call<{ servers: Array<{ name: string }> }>('mcp.list')).servers.map(
      (s) => s.name,
    )
  await expect.poll(known).toContain('notes')

  await notes.getByRole('button', { name: 'Remove', exact: true }).click()
  await notes.getByRole('button', { name: 'Remove notes' }).click()
  await expect(notes).toHaveCount(0)
  await expect.poll(known).not.toContain('notes')
})
