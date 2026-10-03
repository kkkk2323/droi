// The Session list's order and what a folded group says about its Sessions.
import { expect, test } from './fixtures'
import { session, userMessage } from '../fake-daemon/scenario'

// Seconds, all within the last hour so every row shows.
const NOW = Math.floor(Date.now() / 1000)

const alpha = session('Alpha work', '/Users/dev/alpha', [userMessage('a')], { updatedAt: NOW - 60 })
const betaOld = session('Beta old', '/Users/dev/beta', [userMessage('b')], { updatedAt: NOW - 900 })
const betaNew = session('Beta new', '/Users/dev/beta', [userMessage('b')], { updatedAt: NOW - 600 })
const gamma = session('Gamma work', '/Users/dev/gamma', [userMessage('c')], {
  updatedAt: NOW - 300,
})

test.use({
  scenario: {
    sessions: [alpha, betaOld, betaNew, gamma],
    opened: [
      { sessionId: betaOld.sessionId, workingState: 'executing_tool' },
      { sessionId: betaNew.sessionId, workingState: 'waiting_for_tool_confirmation' },
    ],
  },
})

const order = async (sidebar: import('@playwright/test').Locator) =>
  sidebar
    .getByRole('region')
    .evaluateAll((regions) => regions.map((r) => r.getAttribute('aria-label')))

test('a folded Workspace says how many of its Sessions work or wait', async ({
  openClient,
  openSidebar,
}) => {
  await openClient()
  const sidebar = await openSidebar()
  const beta = sidebar.getByRole('region', { name: 'beta' })
  const header = beta.getByRole('button', { name: /beta/, expanded: true })
  await expect(beta.getByRole('status', { name: 'Working' })).toBeVisible()
  await expect(header.getByRole('status')).toHaveCount(0)
  await header.click()
  await expect(beta.getByRole('button', { name: /beta/, expanded: false })).toContainText('1')
  await expect(beta.getByRole('status', { name: '1 needs input, 1 working' })).toBeVisible()
})

test('Workspaces sort by sessions, activity, name or by hand', async ({
  page,
  openClient,
  openSidebar,
}) => {
  await openClient()
  let sidebar = await openSidebar()
  await expect.poll(() => order(sidebar)).toEqual(['beta', 'alpha', 'gamma'])

  const pick = async (name: string) => {
    await sidebar.getByRole('button', { name: 'Sort' }).click()
    await page.getByRole('menuitemradio', { name }).first().click()
  }
  await pick('Name')
  await expect.poll(() => order(sidebar)).toEqual(['alpha', 'beta', 'gamma'])
  await pick('Recently active')
  await expect.poll(() => order(sidebar)).toEqual(['alpha', 'gamma', 'beta'])

  // By hand: from the order shown, moved with the context menu or dragged.
  await pick('Manual')
  await expect.poll(() => order(sidebar)).toEqual(['alpha', 'gamma', 'beta'])
  await sidebar
    .getByRole('region', { name: 'beta' })
    .getByRole('heading')
    .click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Move up' }).click()
  await expect.poll(() => order(sidebar)).toEqual(['alpha', 'beta', 'gamma'])
  await sidebar
    .getByRole('region', { name: 'gamma' })
    .getByRole('heading')
    .dragTo(sidebar.getByRole('region', { name: 'alpha' }).getByRole('heading'), {
      targetPosition: { x: 20, y: 2 },
    })
  await expect.poll(() => order(sidebar)).toEqual(['gamma', 'alpha', 'beta'])

  // The order outlives a reload.
  await page.reload()
  sidebar = await openSidebar()
  await expect.poll(() => order(sidebar)).toEqual(['gamma', 'alpha', 'beta'])
})

test('Sessions sort newest first or by when this Client first saw them', async ({
  page,
  openClient,
  openSidebar,
}) => {
  // The old one was seen first long ago… and the new one, oddly, earlier still.
  await page.addInitScript(
    ({ older, newer }) => {
      localStorage.setItem(
        'droi.sessionsFirstSeen',
        JSON.stringify({ [older]: 5_000, [newer]: 1_000 }),
      )
    },
    { older: betaOld.sessionId, newer: betaNew.sessionId },
  )
  await openClient()
  const sidebar = await openSidebar()
  const titles = () =>
    sidebar
      .getByRole('region', { name: 'beta' })
      .getByRole('listitem')
      .evaluateAll((rows) => rows.map((r) => r.textContent ?? ''))
  await expect.poll(async () => (await titles())[0]).toContain('Beta new')
  await sidebar.getByRole('button', { name: 'Sort' }).click()
  await page.getByRole('menuitemradio', { name: 'Created' }).click()
  await expect.poll(async () => (await titles())[0]).toContain('Beta old')
})

test.describe('a Session that starts working', () => {
  const fresh = session('Fresh talk', '/Users/dev/delta', [userMessage('d')], {
    updatedAt: NOW - 60,
  })
  const old = session('Old talk', '/Users/dev/delta', [userMessage('d')], {
    updatedAt: NOW - 12 * 60 * 60,
  })
  test.use({ scenario: { sessions: [fresh, old] } })

  test('moves to the top of its Workspace, and stays there once it is done', async ({
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const sidebar = await openSidebar()
    const rows = sidebar.getByRole('region', { name: 'delta' }).getByRole('listitem')
    await expect(rows.first()).toContainText('Fresh talk')

    // Driven from another Client: this one has not loaded it.
    fakeDaemon.notify(old.sessionId, {
      type: 'droid_working_state_changed',
      newState: 'executing_tool',
    })
    await expect(rows.first()).toContainText('Old talk')
    await expect(rows.first().getByRole('status', { name: 'Working' })).toBeVisible()

    fakeDaemon.notify(old.sessionId, { type: 'droid_working_state_changed', newState: 'idle' })
    await expect(rows.first().getByRole('status', { name: 'Working' })).toHaveCount(0)
    await expect(rows.first()).toContainText('Old talk')
  })
})
