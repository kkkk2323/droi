import type { Page } from '@playwright/test'
import { session, userMessage } from '../fake-daemon/scenario'
import { streamedReply } from '../fake-daemon/turns'
import {
  expect,
  openNewSession,
  openSessionList,
  pairPhone,
  relaunch,
  sheetsClosed,
  test,
} from './fixtures'

const REPO = '/Users/dev/acme-web'
const WORKTREES = '/Users/test/.factory/worktrees'

const finance = session('Budget', '/Users/dev/finance', [userMessage('sum')])
const ephemeral = session('Fix login', `${WORKTREES}/aaaa1111/acme-web`, [userMessage('fix')], {
  worktree: {
    path: `${WORKTREES}/aaaa1111/acme-web`,
    branch: 'droid/fix-login',
    lifecycle: 'ephemeral',
    repoRoot: REPO,
  },
})
const persistent = session('Try Redis', `${WORKTREES}/bbbb2222/acme-web`, [userMessage('try')], {
  worktree: {
    path: `${WORKTREES}/bbbb2222/acme-web`,
    branch: 'droid/try-redis',
    lifecycle: 'persistent',
    repoRoot: REPO,
  },
})
const main = session('Main work', REPO, [userMessage('hi')])

test.use({
  scenario: {
    // The last Session made is the most recent, so New session opens on acme-web.
    sessions: [finance, ephemeral, persistent, main],
    gitRepos: { [REPO]: { currentBranch: 'main', branches: ['main', 'develop'] } },
    handlers: { 'daemon.add_user_message': streamedReply({ deltas: ['On it.'] }) },
  },
})

/** A long press opens a row's actions, as on the phone. */
async function actionsFor(page: Page, name: RegExp | string) {
  const actions = page.getByRole('dialog', { name: /^Actions for / })
  const list = await openSessionList(page)
  await list.getByRole('button', { name }).click({ delay: 800 })
  await expect(actions).toBeVisible()
  return actions
}

test('worktree Sessions list under their repository with their branch', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const list = await openSessionList(page)
  await expect(list.getByRole('group').getByRole('heading')).toHaveText(['acme-web', 'finance'])
  const repo = list.getByRole('group', { name: 'acme-web' })
  await expect(repo.getByRole('button', { name: /Fix login/ })).toContainText('droid/fix-login')
  await expect(repo.getByRole('button', { name: /Try Redis/ })).toContainText('droid/try-redis')
  await expect(repo.getByRole('button', { name: /Main work/ })).not.toContainText('droid/')

  // The worktree's folder is not a Workspace to start in.
  const form = await openNewSession(page)
  await form.getByRole('button', { name: 'Workspace' }).click()
  await expect(page.getByRole('dialog', { name: 'Workspace' }).getByRole('radio')).toHaveText([
    'None',
    'acme-web',
    'finance',
  ])
})

test('a branch change in a worktree relabels its row', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const list = await openSessionList(page)
  await expect(list.getByRole('button', { name: /Try Redis/ })).toContainText('droid/try-redis')
  const listed = fakeDaemon.scenario.sessions.find((s) => s.sessionId === persistent.sessionId)
  listed!.worktree!.branch = 'droid/redis-cache'
  fakeDaemon.notifyDaemon('daemon.worktree.branch_changed', {
    checkoutPath: persistent.worktree!.path,
    branch: 'droid/redis-cache',
  })
  await expect(list.getByRole('button', { name: /Try Redis/ })).toContainText('droid/redis-cache')
})

test('a removed worktree leaves its Session in the repository without a branch', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const list = await openSessionList(page)
  const row = list
    .getByRole('group', { name: 'acme-web' })
    .getByRole('button', { name: /Fix login/ })
  await expect(row).toContainText('droid/fix-login')
  const listed = fakeDaemon.scenario.sessions.find((s) => s.sessionId === ephemeral.sessionId)
  listed!.worktree!.removedAt = new Date().toISOString()
  fakeDaemon.notifyDaemon('daemon.worktree.removed', { checkoutPath: ephemeral.worktree!.path })
  await expect(row).toBeVisible()
  await expect(row).not.toContainText('droid/fix-login')
})

test('starts a Session in a new worktree from a chosen branch', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const form = await openNewSession(page)
  const worktree = form.getByRole('button', { name: 'Worktree' })
  await expect(worktree).toHaveText(/Work locally/)

  await worktree.click()
  const sheet = page.getByRole('dialog', { name: 'Worktree' })
  await expect(sheet.getByRole('radio', { name: 'Work locally' })).toBeChecked()
  await expect(sheet.getByRole('radiogroup', { name: 'Lifecycle' })).toHaveCount(0)
  await sheet.getByRole('radio', { name: 'New worktree' }).click()
  const lifecycle = sheet.getByRole('radiogroup', { name: 'Lifecycle' })
  await expect(lifecycle.getByRole('radio')).toHaveText([
    /Ephemeral.*Single-session worktrees that are cleaned up automatically\./,
    /Persistent.*Multi-session worktrees that can only be deleted manually\./,
  ])
  await expect(lifecycle.getByRole('radio', { name: 'Ephemeral' })).toBeChecked()
  await lifecycle.getByRole('radio', { name: 'Persistent' }).click()
  await sheet.getByRole('button', { name: 'Base branch' }).click()
  const branches = page.getByRole('dialog', { name: 'Base branch' })
  await expect(branches.getByRole('radio', { name: 'main' })).toBeChecked()
  await branches.getByRole('radio', { name: 'develop' }).click()
  await expect(sheet.getByRole('button', { name: 'Base branch' })).toContainText('develop')
  await page.keyboard.press('Escape')
  await sheetsClosed(page)
  await expect(worktree).toHaveText(/New worktree · Persistent · from develop/)

  await form.getByRole('textbox', { name: 'Message' }).fill('Refactor the invoices')
  await form.getByRole('button', { name: 'Start session' }).click()
  const created = await fakeDaemon.waitForRequest('daemon.initialize_session', 2)
  expect(created.params).toMatchObject({
    cwd: REPO,
    worktree: true,
    worktreeLifecycle: 'persistent',
    worktreePromptSlug: 'Refactor the invoices',
    worktreeBaseBranch: 'develop',
    worktreeBranchMode: 'copy',
  })
  // The draft in the repository itself gives way to the worktree's Session.
  await fakeDaemon.waitForRequest('daemon.close_session')
  expect(
    fakeDaemon.requests.filter((r) => r.method === 'daemon.update_session_settings'),
  ).toHaveLength(0)
  await expect(
    page.getByRole('log', { name: 'Transcript' }).getByRole('article', { name: 'You' }),
  ).toContainText('Refactor the invoices')

  const list = await openSessionList(page)
  await expect(list.getByRole('group').getByRole('heading')).toHaveText(['acme-web', 'finance'])
  await expect(
    list.getByRole('group', { name: 'acme-web' }).getByRole('button', { name: /New session/ }),
  ).toContainText('droid/refactor-the-invoices')
})

test('Work locally asks the Daemon for no worktree', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const form = await openNewSession(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveText(/Work locally/)
  const draft = await fakeDaemon.waitForRequest('daemon.initialize_session')
  expect(draft.params).not.toHaveProperty('worktree')
  await form.getByRole('textbox', { name: 'Message' }).fill('Look around')
  await form.getByRole('button', { name: 'Start session' }).click()
  await fakeDaemon.waitForRequest('daemon.update_session_settings')
  await fakeDaemon.waitForRequest('daemon.add_user_message')
  expect(fakeDaemon.requests.filter((r) => r.method === 'daemon.initialize_session')).toHaveLength(
    1,
  )
})

test('a folder that is not a Git repository has no worktree option', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const form = await openNewSession(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toBeVisible()
  await form.getByRole('button', { name: 'Workspace' }).click()
  await page
    .getByRole('dialog', { name: 'Workspace' })
    .getByRole('radio', { name: 'finance' })
    .click()
  await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/finance\?/)
  await fakeDaemon.waitForRequest('daemon.list_git_branches', 2)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveCount(0)
})

test('starting with a worktree by default is remembered on this phone', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  let form = await openNewSession(page)
  await form.getByRole('button', { name: 'Worktree' }).click()
  await page
    .getByRole('dialog', { name: 'Worktree' })
    .getByRole('switch', { name: 'Start with a worktree by default' })
    .click()
  await page.keyboard.press('Escape')
  await sheetsClosed(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveText(
    /New worktree · Ephemeral/,
  )

  await relaunch(page)
  form = await openNewSession(page)
  await expect(form.getByRole('button', { name: 'Worktree' })).toHaveText(
    /New worktree · Ephemeral/,
  )
  await form.getByRole('button', { name: 'Start session' }).click()
  const created = await fakeDaemon.waitForRequest('daemon.initialize_session', 3)
  expect(created.params).toMatchObject({ worktree: true, worktreeLifecycle: 'ephemeral' })
  expect(created.params).not.toHaveProperty('worktreePromptSlug')
})

test('archiving a Session in an ephemeral worktree asks first', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const actions = await actionsFor(page, /Fix login/)
  await actions.getByRole('button', { name: 'Archive' }).click()
  await expect(actions.getByText('Archiving this session will delete its worktree.')).toBeVisible()
  expect(fakeDaemon.requests.filter((r) => r.method === 'daemon.archive_session')).toHaveLength(0)
  await actions.getByRole('button', { name: 'Cancel' }).click()
  await expect(actions.getByRole('button', { name: 'Archive' })).toBeVisible()
  expect(fakeDaemon.requests.filter((r) => r.method === 'daemon.archive_session')).toHaveLength(0)

  await actions.getByRole('button', { name: 'Archive' }).click()
  await actions.getByRole('button', { name: 'Archive' }).click()
  const archived = await fakeDaemon.waitForRequest('daemon.archive_session')
  expect(archived.params).toMatchObject({ sessionId: ephemeral.sessionId })
})

test('archiving a Session in a persistent worktree needs no confirmation', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const actions = await actionsFor(page, /Try Redis/)
  await actions.getByRole('button', { name: 'Archive' }).click()
  const archived = await fakeDaemon.waitForRequest('daemon.archive_session')
  expect(archived.params).toMatchObject({ sessionId: persistent.sessionId })
})
