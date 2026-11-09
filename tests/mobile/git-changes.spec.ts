import { userEntry } from './fake-kernel/history'
import { expect, pairPhone, pickSession, test } from './fixtures'

const lines = (n: number, word = 'line') =>
  Array.from({ length: n }, (_, i) => `${word} ${i + 1}`).join('\n') + '\n'

test.use({
  scenario: {
    sessions: [
      {
        title: 'Fix the login bug',
        cwd: 'acme-web',
        entries: [userEntry('Why?')],
        git: {
          branch: 'fix/login',
          committed: { 'src/auth/login.ts': lines(10) },
          // Three lines go and twelve come in; a new test file is not added to git yet.
          changed: {
            'src/auth/login.ts': lines(7) + lines(12, 'new'),
            'src/auth/login.test.ts': lines(40, 'test'),
          },
        },
      },
      {
        title: 'Add dark mode',
        cwd: 'acme-app',
        entries: [userEntry('Add dark mode')],
        git: { branch: 'main', committed: { 'README.md': 'App\n' } },
      },
      {
        title: 'Refactor billing',
        cwd: 'scratchpad',
        entries: [userEntry('Refactor the invoice generator')],
      },
    ],
  },
})

test('the header shows the branch and counts and opens the changed files', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Fix the login bug/)
  const button = page.getByRole('button', { name: 'Branch fix/login, 2 changed files' })
  await expect(button).toContainText('fix/login')
  await expect(button).toContainText('+52')
  await expect(button).toContainText('−3')
  await button.click()
  const files = page.getByRole('list', { name: 'Changed files' }).getByRole('listitem')
  await expect(files).toHaveCount(2)
  await expect(files.filter({ hasText: 'login.ts' }).first()).toContainText('src/auth/')
  await expect(files.filter({ hasText: 'login.test.ts' })).toContainText('+40')
})

test('the counts change after a tool call writes a file', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Add dark mode/)
  await expect(page.getByRole('button', { name: 'Branch main, no changes' })).toBeVisible()

  fakeKernel.model.push(
    {
      match: 'make it dark',
      call: {
        name: 'write',
        arguments: { path: 'src/theme/dark.css', content: lines(8, ':root') },
      },
    },
    { match: 'make it dark', text: 'Dark mode is in.' },
  )
  await page.getByRole('textbox', { name: 'Message' }).fill('make it dark')
  await page.getByRole('button', { name: 'Send' }).click()
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Dark mode is in.')
  const button = page.getByRole('button', { name: 'Branch main, 1 changed file' })
  await expect(button).toContainText('+8')
})

test('a plain folder shows nothing', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Refactor billing/)
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('invoice generator')
  await expect(page.getByRole('button', { name: /^Branch / })).toHaveCount(0)
})
