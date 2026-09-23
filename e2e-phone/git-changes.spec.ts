import { session, userMessage } from '../e2e/fake-daemon/scenario'
import { expect, pairPhone, pickSession, test } from './fixtures'

const dirty = session('Fix the login bug', '/Users/dev/acme-web', [userMessage('Why?')], {
  git: {
    branch: 'fix/login',
    files: [
      { path: 'src/auth/login.ts', status: 'modified', additions: 12, deletions: 3 },
      { path: 'src/auth/login.test.ts', status: 'added', additions: 40, deletions: 0 },
    ],
  },
})
const clean = session('Add dark mode', '/Users/dev/acme-web', [userMessage('Add dark mode')], {
  git: { branch: 'main', files: [] },
})
const notRepo = session('Refactor billing', '/Users/dev/scratch', [
  userMessage('Refactor the invoice generator'),
])

test.use({ scenario: { sessions: [dirty, clean, notRepo] } })

test('the header shows the branch and counts and opens the changed files', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Fix the login bug/)
  const button = page.getByRole('button', { name: 'Branch fix/login, 2 changed files' })
  await expect(button).toContainText('fix/login')
  await expect(button).toContainText('+52')
  await expect(button).toContainText('−3')
  await button.click()
  const files = page.getByRole('list', { name: 'Changed files' }).getByRole('listitem')
  await expect(files).toHaveCount(2)
  await expect(files.first()).toContainText('login.ts')
  await expect(files.first()).toContainText('src/auth/')
  await expect(files.nth(1)).toContainText('+40')
})

test('the counts change after a tool result edits files', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Add dark mode/)
  await expect(page.getByRole('button', { name: 'Branch main, no changes' })).toBeVisible()

  const fixture = fakeDaemon.scenario.sessions.find((s) => s.sessionId === clean.sessionId)!
  fakeDaemon.notify(clean.sessionId, {
    type: 'droid_working_state_changed',
    newState: 'executing_tool',
  })
  const toolUse = {
    type: 'tool_use',
    id: 'call_create_dark',
    name: 'Create',
    input: { file_path: 'src/theme/dark.css', content: ':root {}' },
  }
  fakeDaemon.notify(clean.sessionId, { type: 'tool_call', toolUse })
  fixture.git!.files.push({
    path: 'src/theme/dark.css',
    status: 'added',
    additions: 8,
    deletions: 0,
  })
  fakeDaemon.notify(clean.sessionId, {
    type: 'tool_result',
    toolUseId: toolUse.id,
    content: JSON.stringify({ success: true, file_path: 'src/theme/dark.css' }),
    isError: false,
    messageId: 'msg_dark',
  })
  const button = page.getByRole('button', { name: 'Branch main, 1 changed file' })
  await expect(button).toContainText('+8')
})

test('a plain directory shows nothing', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Refactor billing/)
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('invoice generator')
  await expect(page.getByRole('button', { name: /^Branch / })).toHaveCount(0)
})
