import type { Page } from '@playwright/test'
import { drawerGone, expect, test } from './fixtures'
import {
  assistantMessage,
  session,
  thinkingBlock,
  toolCallMessage,
  toolResultMessage,
  userMessage,
  type MessageFixture,
} from '../fake-daemon/scenario'

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
  const createId = 'call_create_1'
  const assistantWithCreate: MessageFixture = {
    ...assistantMessage(''),
    content: [
      {
        type: 'tool_use',
        id: createId,
        name: 'Create',
        input: {
          file_path: 'src/auth.test.ts',
          content: `import { login } from './auth'\n\ntest('logs in', async () => {\n  await login() // ${'a very long line that needs horizontal scrolling '.repeat(4)}\n})\n`,
        },
      },
    ],
  }
  const createResult: MessageFixture = {
    ...assistantMessage(''),
    role: 'tool',
    content: [
      {
        type: 'tool_result',
        toolUseId: createId,
        content: JSON.stringify({ success: true, file_path: 'src/auth.test.ts' }),
      },
    ],
  }
  return [
    userMessage('Why does login fail?'),
    assistantWithTool,
    toolResult,
    assistantWithEdit,
    editResult,
    assistantWithCreate,
    createResult,
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

    // The Workspace with more conversations first, newest Session first within it.
    const headings = await sidebar.getByRole('heading', { level: 2 }).allTextContents()
    expect(headings).toEqual(['acme-web', 'billing-service'])
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
    const first = (await openSidebar()).getByRole('button', { name: /Fix the login bug/ })
    await first.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('region', { name: 'Fix the login bug' })).toBeVisible()
    // On narrow screens the drawer closed after the pick; open it again.
    await drawerGone(page)
    const current = (await openSidebar()).getByRole('button', { name: /Fix the login bug/ })
    await expect(current).toHaveAttribute('aria-current', 'page')
    await current.focus()
    // Next stops: the following Workspace's fold, its new-session button, its first Session.
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'billing-service', exact: true })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'New session in billing-service' })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: /Refactor billing/ })).toBeFocused()
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
      'acme-web',
      'billing-service',
    ])
    await sidebar
      .getByRole('button', { name: 'billing-service', exact: true })
      .click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Pin workspace' }).click()
    await expect(sidebar.getByRole('heading', { level: 2 }).first()).toHaveText('billing-service')
    await expect(sidebar.getByText('Pinned', { exact: true })).toBeVisible()
    await expect(sidebar.getByText('Workspaces', { exact: true })).toBeVisible()

    const fold = sidebar.getByRole('button', { name: 'acme-web', exact: true })
    await fold.click()
    await expect(fold).toHaveAttribute('aria-expanded', 'false')

    await page.reload()
    sidebar = await openSidebar()
    await expect(sidebar.getByRole('heading', { level: 2 }).first()).toHaveText('billing-service')
    await expect(sidebar.getByRole('button', { name: 'acme-web', exact: true })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    await expect(
      sidebar.getByRole('region', { name: 'acme-web' }).getByRole('listitem'),
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

    // Line numbers stay put and hide the code scrolled under them.
    const gutter = diff.locator('[data-type="unchanged"] > span').first()
    expect(await gutter.evaluate((el) => getComputedStyle(el).position)).toBe('sticky')
    expect(await gutter.evaluate((el) => getComputedStyle(el).backgroundColor)).not.toBe(
      'rgba(0, 0, 0, 0)',
    )
    await edit.click()

    // Create answers with bare JSON; its row shows the file it wrote instead.
    const create = transcript.getByRole('button', { name: 'Create: src/auth.test.ts' })
    await expect(create.getByRole('img', { name: 'Succeeded' })).toBeVisible()
    await expect(create).toContainText('+5')
    await create.click()
    const created = transcript.getByLabel('Diff')
    await expect(created.locator('[data-type="added"]')).toHaveCount(5)
    await expect(created).toContainText("test('logs in', async () => {")
    await expect(transcript.getByText('"success"')).toHaveCount(0)
    const addedGutter = created.locator('[data-type="added"] > span').first()
    expect(await addedGutter.evaluate((el) => getComputedStyle(el).backgroundImage)).toContain(
      'gradient',
    )

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

  test('the counts follow the edits while a turn runs', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    // A turn that edits one file and then keeps working.
    fakeDaemon.scenario.on('daemon.add_user_message', (params, { daemon }) => {
      const sessionId = String(params['sessionId'])
      const fixture = daemon.scenario.sessions.find((s) => s.sessionId === sessionId)!
      daemon.notify(sessionId, { type: 'droid_working_state_changed', newState: 'executing_tool' })
      const toolUse = {
        type: 'tool_use',
        id: 'call_create_dark',
        name: 'Create',
        input: { file_path: 'src/theme/dark.css', content: ':root {}' },
      }
      daemon.notify(sessionId, { type: 'tool_call', toolUse })
      fixture.git!.files.push({
        path: 'src/theme/dark.css',
        status: 'added',
        additions: 8,
        deletions: 0,
      })
      daemon.notify(sessionId, {
        type: 'tool_result',
        toolUseId: toolUse.id,
        content: JSON.stringify({ success: true, file_path: 'src/theme/dark.css' }),
        isError: false,
        messageId: 'msg_dark',
      })
      return {}
    })
    await openClient()
    await pickSession(/Add dark mode/)
    await expect(page.getByRole('button', { name: 'Branch main, no changes' })).toBeVisible()
    await page.getByRole('textbox', { name: 'Message' }).fill('Add the dark theme file')
    await page.getByRole('button', { name: 'Send' }).click()

    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
    const button = page.getByRole('button', { name: 'Branch main, 1 changed file' })
    await expect(button).toContainText('+8')
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
  })

  test('edits made outside Droi show up without reopening the Session', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await page.clock.install()
    await openClient()
    await pickSession(/Add dark mode/)
    await expect(page.getByRole('button', { name: 'Branch main, no changes' })).toBeVisible()

    const fixture = fakeDaemon.scenario.sessions.find((s) => s.sessionId === clean.sessionId)!
    fixture.git!.files.push({ path: 'README.md', status: 'modified', additions: 1, deletions: 1 })
    await page.clock.runFor(16_000)
    await expect(page.getByRole('button', { name: 'Branch main, 1 changed file' })).toBeVisible()
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

test.describe('running tools', () => {
  test.use({
    scenario: {
      sessions: [
        session('Busy', '/Users/dev/acme-web', [
          userMessage('Check both files'),
          toolCallMessage('t1', 'Read', { file_path: '/repo/a.ts' }),
          toolCallMessage('t2', 'Read', { file_path: '/repo/b.ts' }),
        ]),
      ],
    },
  })

  test('spinners that appear at different moments turn as one', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Busy/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const cluster = transcript.getByRole('button', { name: 'Running 2 tools' })
    // Folding and reopening the cluster mounts its rows' spinners anew, later
    // than the one in the cluster's own button.
    await page.waitForTimeout(300)
    await cluster.click()
    await page.waitForTimeout(170)
    await cluster.click()
    await expect(transcript.getByRole('status', { name: 'Running' })).toHaveCount(2)
    const angles = await page.evaluate(() =>
      Array.from(document.querySelectorAll('[role="log"] .animate-spin'), (el) => {
        const { a, b } = new DOMMatrix(getComputedStyle(el).transform)
        return Math.round((Math.atan2(b, a) * 180) / Math.PI)
      }),
    )
    expect(angles).toHaveLength(3)
    for (const angle of angles) expect(Math.abs(angle - angles[0]!)).toBeLessThanOrEqual(3)
  })
})

test.describe('switching Sessions', () => {
  // Turns like a working Session's: a one-line question, then a tall block of
  // reasoning and tool calls. Rows of such different heights are what make a
  // list's estimated heights for unmeasured rows differ from mount to mount.
  const history = (name: string): MessageFixture[] =>
    Array.from({ length: 40 }, (_, turn) => [
      userMessage(`${name} question ${turn}`),
      ...Array.from({ length: 4 }, (_step, step): MessageFixture[] => {
        const id = `${name}-${turn}-${step}`
        return [
          {
            ...assistantMessage(''),
            content: [
              thinkingBlock(`Thinking about step ${step}. `.repeat(20), 4000),
              { type: 'tool_use', id, name: 'Execute', input: { command: `echo ${turn} ${step}` } },
            ],
          },
          toolResultMessage(id, `line\n`.repeat(5)),
        ]
      }).flat(),
      assistantMessage(
        `## ${name} answer ${turn}\n\nSome **markdown**:\n\n- one\n- two\n\n\`\`\`ts\nconst v = ${turn}\n\`\`\``,
      ),
    ]).flat()
  test.use({
    scenario: {
      sessions: [
        session('Alpha', '/Users/dev/acme-web', history('Alpha')),
        session('Beta', '/Users/dev/acme-web', history('Beta')),
      ],
    },
  })

  /** The row at the top of the transcript's viewport: its text and where it sits. */
  function topRow(page: Page) {
    return page.getByRole('log', { name: 'Transcript' }).evaluate((el) => {
      const top = el.getBoundingClientRect().top
      const row = [...el.querySelectorAll('[data-index]')].find(
        (r) => r.getBoundingClientRect().bottom > top,
      )
      return row
        ? { text: row.textContent, offset: Math.round(top - row.getBoundingClientRect().top) }
        : null
    })
  }

  /** Every frame, until `stop()`, the visible transcript's distance from its end. */
  async function watchFrames(page: Page) {
    await page.evaluate(() => {
      const w = window as unknown as { droiGaps: number[]; droiWatching: boolean }
      w.droiGaps = []
      w.droiWatching = true
      requestAnimationFrame(function sample() {
        const log = document.querySelector<HTMLElement>('[role="log"]')
        if (log?.checkVisibility({ visibilityProperty: true }) && log.querySelector('article')) {
          w.droiGaps.push(log.scrollHeight - log.scrollTop - log.clientHeight)
        }
        if (w.droiWatching) requestAnimationFrame(sample)
      })
    })
    return () =>
      page.evaluate(() => {
        const w = window as unknown as { droiGaps: number[]; droiWatching: boolean }
        w.droiWatching = false
        return w.droiGaps
      })
  }

  test('a Session appears at its end, never part way up first', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Alpha/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Alpha answer 39')).toBeInViewport()
    for (const title of [/Beta/, /Alpha/, /Beta/]) {
      const stop = await watchFrames(page)
      await pickSession(title)
      await expect(transcript.getByRole('article').last()).toBeInViewport()
      await page.waitForTimeout(400)
      const gaps = await stop()
      expect(gaps.length).toBeGreaterThan(0)
      expect(Math.max(...gaps)).toBeLessThanOrEqual(2)
    }
  })

  test('returning opens where the reader left off; one left at the end opens at the end', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Alpha/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Alpha answer 39')).toBeInViewport()
    // The list re-pins itself to the end shortly after opening; scroll up after that.
    await page.waitForTimeout(300)
    // Just above the end: the rows above were never measured.
    await transcript.evaluate((el) =>
      el.scrollTo({ top: el.scrollHeight - el.clientHeight - el.clientHeight * 1.5 }),
    )
    await expect(transcript.getByText('Alpha answer 39')).not.toBeInViewport()
    await page.waitForTimeout(100)
    const readingAt = await topRow(page)

    await pickSession(/Beta/)
    await expect(transcript.getByText('Beta answer 39')).toBeInViewport()
    await pickSession(/Alpha/)
    await expect.poll(() => topRow(page)).toEqual(readingAt)
    await page.waitForTimeout(300)
    expect(await topRow(page)).toEqual(readingAt)
    await expect(page.getByRole('button', { name: 'Scroll to latest' })).toBeVisible()

    await page.getByRole('button', { name: 'Scroll to latest' }).click()
    await expect(transcript.getByText('Alpha answer 39')).toBeInViewport()
    await page.waitForTimeout(300)
    await pickSession(/Beta/)
    await pickSession(/Alpha/)
    await expect(transcript.getByText('Alpha answer 39')).toBeInViewport()
    await expect(page.getByRole('button', { name: 'Scroll to latest' })).toHaveCount(0)
  })

  test('"Scroll to latest" reaches the end in one click from far up', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Alpha/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Alpha answer 39')).toBeInViewport()
    await page.waitForTimeout(300)
    await transcript.evaluate((el) => el.scrollTo({ top: el.scrollHeight / 3 }))
    await page.waitForTimeout(100)
    // Coming back, the rows below the reading position have never been measured.
    await pickSession(/Beta/)
    await expect(transcript.getByText('Beta answer 39')).toBeInViewport()
    await pickSession(/Alpha/)
    await expect(transcript.getByText('Alpha answer 39')).not.toBeInViewport()

    await page.getByRole('button', { name: 'Scroll to latest' }).click()
    // Promptly, too: a glide that crawls after rows growing on the way does not count.
    await expect
      .poll(() => transcript.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight), {
        timeout: 1500,
      })
      .toBeLessThanOrEqual(2)
    await expect(transcript.getByText('Alpha answer 39')).toBeInViewport()
  })
})

test.describe('opening a tool row at the bottom of the transcript', () => {
  const toolCall = (id: string, command: string): MessageFixture[] => [
    {
      ...assistantMessage(''),
      content: [{ type: 'tool_use', id, name: 'Execute', input: { command, summary: command } }],
    },
    {
      ...assistantMessage(''),
      role: 'tool',
      content: [
        {
          type: 'tool_result',
          toolUseId: id,
          content: Array.from({ length: 15 }, (_, n) => `line ${n} of ${command}`).join('\n'),
        },
      ],
    },
  ]
  const history: MessageFixture[] = []
  for (let i = 0; i < 12; i++) {
    history.push(
      userMessage(`Question ${i} `.repeat(8)),
      assistantMessage(`Answer ${i} `.repeat(30)),
    )
  }
  history.push(userMessage('run the steps'))
  for (let i = 0; i < 4; i++) history.push(...toolCall(`call_step_${i}`, `echo step ${i}`))
  history.push(assistantMessage('All steps done.'))
  const steps = session('Many steps', '/Users/dev/acme-web', history)
  test.use({ scenario: { sessions: [steps] } })

  test('keeps the row where it was clicked instead of jumping to the end', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Many steps/)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('All steps done.')
    const row = page.getByRole('button', { name: 'Execute: echo step 3' })
    // Let the opening scroll to the latest message settle first.
    await page.waitForTimeout(400)
    const before = (await row.boundingBox())!.y
    await row.click()
    await expect(row).toHaveAttribute('aria-expanded', 'true')
    await page.waitForTimeout(600)
    expect(Math.abs((await row.boundingBox())!.y - before)).toBeLessThan(2)
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
