import { drawerGone, expect, test } from './fixtures'
import {
  assistantMessage,
  session,
  thinkingBlock,
  userMessage,
  type MessageFixture,
} from './fake-daemon/scenario'

const droiSession = session('Fix the login bug', '/Users/dev/acme-web', [
  userMessage('Why does login fail?'),
  assistantMessage('Let me look at the auth module.'),
])
const anotherDroiSession = session('Add dark mode', '/Users/dev/acme-web', [
  userMessage('Add dark mode'),
])
// Created by the interactive `droid` CLI; the Daemon lists it all the same.
const cliSession = session('Refactor billing', '/Users/dev/billing-service', [
  userMessage('Refactor the invoice generator'),
])

function richHistory(): MessageFixture[] {
  const toolUseId = 'call_read_1'
  const assistantWithTool: MessageFixture = {
    ...assistantMessage(''),
    content: [
      thinkingBlock('I should read the file before changing it.', 1200),
      { type: 'tool_use', id: toolUseId, name: 'Read', input: { file_path: 'src/auth.ts' } },
    ],
  }
  const toolResult: MessageFixture = {
    ...assistantMessage(''),
    role: 'tool',
    content: [{ type: 'tool_result', toolUseId, content: 'export function login() {}' }],
  }
  const editId = 'call_edit_1'
  const assistantWithEdit: MessageFixture = {
    ...assistantMessage(''),
    content: [
      {
        type: 'tool_use',
        id: editId,
        name: 'Edit',
        input: { file_path: 'src/auth.ts', old_str: 'login()', new_str: 'await login()' },
      },
    ],
  }
  const editResult: MessageFixture = {
    ...assistantMessage(''),
    role: 'tool',
    content: [
      {
        type: 'tool_result',
        toolUseId: editId,
        content: JSON.stringify({
          success: true,
          file_path: 'src/auth.ts',
          diffLines: [
            {
              type: 'unchanged',
              content: 'export async function run() {',
              lineNumber: { old: 1, new: 1 },
            },
            { type: 'removed', content: '  login()', lineNumber: { old: 2 } },
            { type: 'added', content: '  await login()', lineNumber: { new: 2 } },
            { type: 'unchanged', content: '}', lineNumber: { old: 3, new: 3 } },
          ],
          linesAdded: 1,
          linesRemoved: 1,
        }),
      },
    ],
  }
  return [
    userMessage('Why does login fail?'),
    assistantWithTool,
    toolResult,
    assistantWithEdit,
    editResult,
    assistantMessage('The `login` function never awaits the token refresh. Here is the fix.'),
  ]
}

const richSession = session('Fix the login bug', '/Users/dev/acme-web', richHistory())

const DAY = 24 * 60 * 60
const staleSession = session('Old spike', '/Users/dev/acme-web', [userMessage('spike')], {
  updatedAt: Math.floor(Date.now() / 1000) - 10 * DAY,
})

test.describe('sidebar', () => {
  test.use({ scenario: { sessions: [droiSession, anotherDroiSession, cliSession] } })

  test('rows show the title, the message count and the age on two lines', async ({
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const row = (await openSidebar()).getByRole('button', { name: /Fix the login bug/ })
    await expect(row).toContainText('2 messages')
    await expect(row.locator('time')).toHaveText(/^\d+[mh]$|^now$/)
  })

  test('lists every Session grouped by Workspace, including ones made outside Droi', async ({
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const sidebar = await openSidebar()

    const acme = sidebar.getByRole('region', { name: 'acme-web' })
    const billing = sidebar.getByRole('region', { name: 'billing-service' })
    await expect(acme).toBeVisible()
    await expect(billing).toBeVisible()
    await expect(acme.getByRole('listitem')).toHaveCount(2)
    await expect(billing.getByRole('button', { name: /Refactor billing/ })).toBeVisible()

    // Newest Workspace first, newest Session first within it.
    const headings = await sidebar.getByRole('heading', { level: 2 }).allTextContents()
    expect(headings).toEqual(['billing-service', 'acme-web'])
    const acmeTitles = await acme.getByRole('listitem').allTextContents()
    expect(acmeTitles[0]).toMatch(/Add dark mode/)

    // A Workspace folds away and comes back.
    const fold = acme.getByRole('button', { name: 'acme-web', exact: true })
    await expect(fold).toHaveAttribute('aria-expanded', 'true')
    await fold.click()
    await expect(acme.getByRole('listitem')).toHaveCount(0)
    await fold.click()
    await expect(acme.getByRole('listitem')).toHaveCount(2)
  })

  test('is keyboard navigable', async ({ page, openClient, openSidebar }) => {
    await openClient()
    const first = (await openSidebar()).getByRole('button', { name: /Refactor billing/ })
    await first.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('region', { name: 'Refactor billing' })).toBeVisible()
    // On narrow screens the drawer closed after the pick; open it again.
    await drawerGone(page)
    const current = (await openSidebar()).getByRole('button', { name: /Refactor billing/ })
    await expect(current).toHaveAttribute('aria-current', 'page')
    await current.focus()
    // Next stops: the following Workspace's fold, its new-session button, its first Session.
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'acme-web', exact: true })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'New session in acme-web' })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: /Add dark mode/ })).toBeFocused()
  })
})

test.describe('sidebar with old sessions', () => {
  test.use({ scenario: { sessions: [droiSession, staleSession] } })

  test('sessions untouched for days hide behind "Show older" until asked for', async ({
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const acme = (await openSidebar()).getByRole('region', { name: 'acme-web' })
    await expect(acme.getByRole('listitem')).toHaveCount(1)
    await expect(acme.getByRole('button', { name: /Old spike/ })).toHaveCount(0)
    await acme.getByRole('button', { name: 'Show 1 older' }).click()
    await expect(acme.getByRole('button', { name: /Old spike/ })).toBeVisible()
    await expect(acme.getByRole('button', { name: /Show \d+ older/ })).toHaveCount(0)
  })

  test('a pinned Session stays visible and on top; the pin survives a reload', async ({
    page,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    let acme = (await openSidebar()).getByRole('region', { name: 'acme-web' })
    await acme.getByRole('button', { name: 'Show 1 older' }).click()
    await acme.getByRole('button', { name: /Old spike/ }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Pin', exact: true }).click()

    const rows = acme.getByRole('listitem')
    await expect(rows.first()).toContainText('Old spike')
    await expect(rows.first().getByLabel('Pinned')).toBeVisible()

    await page.reload()
    acme = (await openSidebar()).getByRole('region', { name: 'acme-web' })
    await expect(acme.getByRole('listitem').first()).toContainText('Old spike')
    await expect(acme.getByRole('button', { name: /Show \d+ older/ })).toHaveCount(0)

    await acme.getByRole('button', { name: /Old spike/ }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Unpin' }).click()
    await expect(acme.getByRole('button', { name: /Old spike/ })).toHaveCount(0)
  })
})

test.describe('sidebar memory', () => {
  test.use({ scenario: { sessions: [droiSession, anotherDroiSession, cliSession] } })

  test('a pinned Workspace leads the list and a folded one stays folded after a reload', async ({
    page,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    let sidebar = await openSidebar()
    expect(await sidebar.getByRole('heading', { level: 2 }).allTextContents()).toEqual([
      'billing-service',
      'acme-web',
    ])
    await sidebar.getByRole('button', { name: 'acme-web', exact: true }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Pin workspace' }).click()
    await expect(sidebar.getByRole('heading', { level: 2 }).first()).toHaveText('acme-web')
    await expect(sidebar.getByText('Pinned', { exact: true })).toBeVisible()
    await expect(sidebar.getByText('Workspaces', { exact: true })).toBeVisible()

    const fold = sidebar.getByRole('button', { name: 'billing-service', exact: true })
    await fold.click()
    await expect(fold).toHaveAttribute('aria-expanded', 'false')

    await page.reload()
    sidebar = await openSidebar()
    await expect(sidebar.getByRole('heading', { level: 2 }).first()).toHaveText('acme-web')
    await expect(
      sidebar.getByRole('button', { name: 'billing-service', exact: true }),
    ).toHaveAttribute('aria-expanded', 'false')
    await expect(
      sidebar.getByRole('region', { name: 'billing-service' }).getByRole('listitem'),
    ).toHaveCount(0)
  })

  test('launching on the home route reopens the Session that was open last', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Add dark mode/)
    await expect(page.getByRole('region', { name: 'Add dark mode' })).toBeVisible()

    await page.goto('/')
    await expect(page.getByRole('region', { name: 'Add dark mode' })).toBeVisible()
    expect(new URL(page.url()).hash).toMatch(/^#\/s\//)
  })
})

test.describe('session history', () => {
  test.use({ scenario: { sessions: [richSession] } })

  test('opening a Session renders text, a tool call with its result, and reasoning', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Fix the login bug/)

    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('article', { name: 'You' })).toContainText(
      'Why does login fail?',
    )
    await expect(transcript.getByText(/never awaits the token refresh/)).toBeVisible()

    const reasoning = transcript.getByRole('button', { name: /Reasoned for 1s/ })
    await expect(reasoning).toBeVisible()
    await reasoning.click()
    await expect(transcript.getByText('I should read the file before changing it.')).toBeVisible()

    const tool = transcript.getByRole('button', { name: 'Read: src/auth.ts' })
    await expect(tool).toBeVisible()
    await tool.click()
    await expect(transcript.getByText('export function login() {}')).toBeVisible()

    // An Edit shows its counts on the row and its result as a coloured diff.
    const edit = transcript.getByRole('button', { name: 'Edit: src/auth.ts' })
    await expect(edit).toContainText('+1')
    await expect(edit).toContainText('−1')
    await edit.click()
    const diff = transcript.getByLabel('Diff')
    await expect(diff.locator('[data-type="removed"]')).toContainText('login()')
    await expect(diff.locator('[data-type="added"]')).toContainText('await login()')
    await expect(diff.locator('[data-type="unchanged"]')).toHaveCount(2)

    const load = await fakeDaemon.waitForRequest('daemon.load_session')
    expect(load.params).toMatchObject({ sessionId: richSession.sessionId })
    expect(new URL(page.url()).hash).toBe(`#/s/${richSession.sessionId}`)
  })

  test('a reload keeps the open Session', async ({ page, openClient, pickSession }) => {
    await openClient()
    await pickSession(/Fix the login bug/)
    await expect(page.getByRole('log', { name: 'Transcript' })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('region', { name: 'Fix the login bug' })).toBeVisible()
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText(
      'Why does login fail?',
    )
  })
})

test.describe('git changes in the header', () => {
  const dirty = session(
    'Fix the login bug',
    '/Users/dev/acme-web',
    [userMessage('Why does login fail?')],
    {
      git: {
        branch: 'fix/login',
        files: [
          { path: 'src/auth/login.ts', status: 'modified', additions: 12, deletions: 3 },
          { path: 'src/auth/login.test.ts', status: 'added', additions: 40, deletions: 0 },
        ],
      },
    },
  )
  const clean = session('Add dark mode', '/Users/dev/acme-web', [userMessage('Add dark mode')], {
    git: { branch: 'main', files: [] },
  })
  const notRepo = session('Refactor billing', '/Users/dev/scratch', [
    userMessage('Refactor the invoice generator'),
  ])
  test.use({ scenario: { sessions: [dirty, clean, notRepo] } })

  test('shows the branch with the uncommitted counts and lists the files', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Fix the login bug/)
    const button = page.getByRole('button', { name: 'Branch fix/login, 2 changed files' })
    await expect(button).toContainText('fix/login')
    await expect(button).toContainText('+52')
    await expect(button).toContainText('−3')
    await button.click()
    const files = page.getByRole('list', { name: 'Changed files' })
    await expect(files.getByRole('listitem')).toHaveCount(2)
    await expect(files.getByRole('listitem').first()).toContainText('login.ts')
    await expect(files.getByRole('listitem').first()).toContainText('src/auth/')
    await expect(files.getByRole('listitem').nth(1)).toContainText('+40')
    await page.keyboard.press('Escape')
    await expect(files).toHaveCount(0)
  })

  test('a clean tree shows only the branch; a plain directory shows nothing', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Add dark mode/)
    const button = page.getByRole('button', { name: 'Branch main, no changes' })
    await expect(button).toHaveText('main')
    await pickSession(/Refactor billing/)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('invoice generator')
    await expect(page.getByRole('button', { name: /^Branch / })).toHaveCount(0)
  })
})

test.describe('long history', () => {
  const many = Array.from({ length: 120 }, (_, i) =>
    i % 2 === 0 ? userMessage(`Question ${i}`) : assistantMessage(`Answer ${i}`),
  )
  const longSession = session('Long chat', '/Users/dev/acme-web', many)
  test.use({ scenario: { sessions: [longSession] } })

  test('is virtualised and opens scrolled to the latest message', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Long chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Answer 119')).toBeVisible()
    const rendered = await transcript.getByRole('article').count()
    expect(rendered).toBeLessThan(80)
    await expect(transcript.getByText('Question 0')).toHaveCount(0)
  })
})

test.describe('reconnect keeps the Session', () => {
  test.use({ scenario: { sessions: [droiSession] } })

  test('after the Daemon returns the open Session is still shown', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Fix the login bug/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('Why does login fail?')

    fakeDaemon.goDown()
    await expect(page.getByRole('alert')).toBeVisible()
    fakeDaemon.comeBack()
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/, {
      timeout: 15_000,
    })
    await expect(page.getByRole('region', { name: 'Fix the login bug' })).toBeVisible()
    await expect(transcript).toContainText('Why does login fail?')
    // The Daemon streams only to sockets that loaded the Session, so the
    // Client must load it again on the new connection.
    await expect
      .poll(() => fakeDaemon.requests.filter((r) => r.method === 'daemon.load_session').length)
      .toBe(2)
  })
})
