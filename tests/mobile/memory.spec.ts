import { session, userMessage } from '../fake-daemon/scenario'
import { expect, openDrawer, pairPhone, pickSession, test } from './fixtures'

const work = session('Invoice export', '/Users/dev/billing-service', [userMessage('a')])
// The Desktop Shell's own model work (ADR 0011); no Client lists it.
const memorySession = session(
  'Memory: extract from 0427515a',
  '/Users/dev/billing-service',
  [userMessage('{}')],
  {
    tags: [{ name: 'droi.memory' }],
  },
)

test.use({
  scenario: {
    sessions: [work, memorySession],
    mcpServers: [
      { name: 'files', type: 'stdio', tools: [{ name: 'read_file' }] },
      { name: 'droi-memory', type: 'stdio', tools: [{ name: 'memory_search' }] },
    ],
  },
})

test('Memory Sessions are not listed', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Invoice export/ })).toBeVisible()
  await expect(list.getByRole('button', { name: /Memory: extract/ })).toHaveCount(0)
})

test('the Memory Server shows as Droi’s, with no switch and a pointer to the computer', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Invoice export/)
  await page.getByRole('button', { name: 'Skills and MCP servers' }).click()
  const sheet = page.getByRole('dialog', { name: 'Skills and MCP servers' })
  await sheet.getByRole('tab', { name: 'MCP servers' }).click()
  const memory = sheet.getByRole('listitem', { name: 'droi-memory' })
  await expect(memory).toContainText('Droi')
  await expect(memory).toContainText('Settings → Memory')
  await expect(memory.getByRole('switch')).toHaveCount(0)
  await expect(sheet.getByRole('listitem', { name: 'files' }).getByRole('switch')).toBeEnabled()
})
