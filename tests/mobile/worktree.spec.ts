// Worktrees from the phone on a Kernel (M4 4N): the Gateway makes one with git on the computer, on a new branch,
// and the conversation works in it. There is no lifecycle: nothing removes a worktree but a person, in Settings on
// the computer. A worktree's Sessions are listed under their repository.
import { execFileSync } from 'node:child_process'
import { realpathSync } from 'node:fs'
import { join } from 'node:path'
import { userEntry } from './fake-kernel/history'
import {
  expect,
  openNewSession,
  openSessionList,
  pairPhone,
  relaunch,
  sheetsClosed,
  test,
  transcriptOf,
} from './fixtures'

function git(dir: string, ...args: string[]): string {
  return execFileSync('git', args, { cwd: dir, encoding: 'utf8' }).trim()
}

/** The repository's worktrees but its own checkout, as `git worktree list` names them. */
function worktreesOf(repo: string): Array<{ path: string; head: string; branch: string }> {
  const blocks = git(repo, 'worktree', 'list', '--porcelain').split('\n\n').slice(1)
  return blocks.map((block) => {
    const field = (name: string) =>
      block
        .split('\n')
        .find((line) => line.startsWith(`${name} `))
        ?.slice(name.length + 1) ?? ''
    return {
      path: field('worktree'),
      head: field('HEAD'),
      branch: field('branch').replace('refs/heads/', ''),
    }
  })
}

test.use({
  scenario: {
    sessions: [
      { title: 'Budget', cwd: 'finance', entries: [userEntry('sum')] },
      // The last Session made is the most recent, so New session opens on acme-web.
      {
        title: 'Main work',
        cwd: 'acme-web',
        git: { branches: ['develop'], committed: { 'README.md': 'App\n' } },
        entries: [userEntry('hi')],
      },
    ],
  },
})

test('a worktree’s Sessions list under its repository, and its folder is no Workspace to start in', async ({
  page,
  fakeKernel,
}) => {
  const repo = fakeKernel.folder('acme-web')
  const tree = join(fakeKernel.folder('trees'), 'fix-login')
  git(repo, 'worktree', 'add', '-q', '-b', 'droi/fix-login', tree)
  await fakeKernel.seed({
    title: 'Fix login',
    cwd: tree,
    entries: [userEntry('fix', Date.now() - 3 * 60 * 60_000)],
  })

  await pairPhone(page, fakeKernel)
  const list = await openSessionList(page)
  await expect(list.getByRole('group').getByRole('heading')).toHaveText(['acme-web', 'finance'])
  const group = list.getByRole('group', { name: 'acme-web' })
  await expect(group.getByRole('button', { name: /Fix login/ })).toBeVisible()
  await expect(group.getByRole('button', { name: /Main work/ })).toBeVisible()

  const form = await openNewSession(page)
  await form.getByRole('button', { name: 'Workspace' }).click()
  await expect(page.getByRole('dialog', { name: 'Workspace' }).getByRole('radio')).toHaveText([
    'None',
    'acme-web',
    'finance',
  ])
})

test('starts a Session in a new worktree from a chosen branch', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  const repo = fakeKernel.folder('acme-web')
  // develop moves on, so a worktree made from it is told apart from one made from main.
  const develop = git(repo, 'commit-tree', 'HEAD^{tree}', '-p', 'HEAD', '-m', 'develop work')
  git(repo, 'branch', '-f', 'develop', develop)
  fakeKernel.model.push({ match: 'Refactor the invoices', text: 'On it.' })

  await pairPhone(page, fakeKernel)
  const form = await openNewSession(page)
  const worktree = form.getByRole('button', { name: 'Worktree' })
  await expect(worktree).toHaveText(/Work locally/)

  await worktree.click()
  const sheet = page.getByRole('dialog', { name: 'Worktree' })
  await expect(sheet.getByRole('radio', { name: 'Work locally' })).toBeChecked()
  await expect(sheet.getByRole('button', { name: 'Base branch' })).toHaveCount(0)
  await sheet.getByRole('radio', { name: 'New worktree' }).click()
  await sheet.getByRole('button', { name: 'Base branch' }).click()
  const branches = page.getByRole('dialog', { name: 'Base branch' })
  await expect(branches.getByRole('radio')).toHaveText([/develop/, /main.*Current branch/])
  await branches.getByRole('radio', { name: /develop/ }).click()
  await expect(sheet.getByRole('button', { name: 'Base branch' })).toContainText('develop')
  await page.keyboard.press('Escape')
  await sheetsClosed(page)
  await expect(worktree).toHaveText(/New worktree · from develop/)

  await form.getByRole('textbox', { name: 'Message' }).fill('Refactor the invoices')
  await form.getByRole('button', { name: 'Start session' }).click()
  const created = await phoneWire.waitForCall('conversation.create')
  const [made] = worktreesOf(repo)
  expect(made).toBeDefined()
  expect(made!.branch).toMatch(/^droi\/refactor-the-invoices-[0-9a-f]{4}$/)
  expect(made!.head).toBe(develop)
  expect(realpathSync(String(created['cwd']))).toBe(realpathSync(made!.path))
  await expect(transcriptOf(page).getByRole('article', { name: 'You' })).toContainText(
    'Refactor the invoices',
  )
  await expect(transcriptOf(page)).toContainText('On it.')

  const list = await openSessionList(page)
  await expect(list.getByRole('group').getByRole('heading')).toHaveText(['acme-web', 'finance'])
  const group = list.getByRole('group', { name: 'acme-web' })
  await expect(group.getByRole('button', { name: /Refactor the invoices/ })).toBeVisible()
  await expect(group.getByRole('button', { name: /Main work/ })).toBeVisible()
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
})

test('Work locally starts in the Workspace itself and makes no worktree', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  const repo = fakeKernel.folder('acme-web')
  fakeKernel.model.push({ match: 'Look around', text: 'Looking.' })
  await pairPhone(page, fakeKernel)
  const form = await openNewSession(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveText(/Work locally/)
  await form.getByRole('textbox', { name: 'Message' }).fill('Look around')
  await form.getByRole('button', { name: 'Start session' }).click()
  const created = await phoneWire.waitForCall('conversation.create')
  expect(realpathSync(String(created['cwd']))).toBe(realpathSync(repo))
  await expect(transcriptOf(page)).toContainText('Looking.')
  expect(worktreesOf(repo)).toEqual([])
})

test('a folder that is not a Git repository has no worktree option', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  const form = await openNewSession(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toBeVisible()
  await form.getByRole('button', { name: 'Workspace' }).click()
  await page
    .getByRole('dialog', { name: 'Workspace' })
    .getByRole('radio', { name: 'finance' })
    .click()
  await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/finance\?/)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveCount(0)
})

test('starting with a worktree by default is remembered on this phone', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  const repo = fakeKernel.folder('acme-web')
  await pairPhone(page, fakeKernel)
  let form = await openNewSession(page)
  await form.getByRole('button', { name: 'Worktree' }).click()
  await page
    .getByRole('dialog', { name: 'Worktree' })
    .getByRole('switch', { name: 'Start with a worktree by default' })
    .click()
  await page.keyboard.press('Escape')
  await sheetsClosed(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveText(/New worktree/)

  await relaunch(page)
  form = await openNewSession(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveText(/New worktree/)
  await form.getByRole('button', { name: 'Start session' }).click()
  const created = await phoneWire.waitForCall('conversation.create')
  const [made] = worktreesOf(repo)
  expect(made).toBeDefined()
  expect(realpathSync(String(created['cwd']))).toBe(realpathSync(made!.path))
})
