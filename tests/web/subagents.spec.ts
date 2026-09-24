// Subagents stay out of the sidebar. They hang off the Session that called
// them: a card per Task call in its transcript, a menu in its header, and a
// trail back from the subagent.
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
import { subagentScenario } from '../fake-daemon/subagents'

const { main, explorer, reviewer, other, sessions } = subagentScenario()

test.describe('subagents', () => {
  test.use({ scenario: { sessions } })

  test('are not listed; their calling Session shows each one as a card', async ({
    page,
    openClient,
    openSidebar,
    pickSession,
  }) => {
    await openClient()
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Plan the release/ })).toBeVisible()
    await expect(sidebar.getByRole('button', { name: /Explorer:/ })).toHaveCount(0)
    await expect(sidebar.getByRole('button', { name: /Reviewer:/ })).toHaveCount(0)

    await pickSession(/Plan the release/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const explorerCard = transcript.getByRole('group', { name: 'Explorer: Map the mobile app' })
    await expect(explorerCard).toContainText('Completed')
    await expect(explorerCard).toContainText('12 tools')
    await expect(explorerCard).toContainText('2m 14s')
    // The prompt and the report open under the card.
    await explorerCard.getByRole('button', { name: 'Details' }).click()
    await expect(explorerCard).toContainText('Explore apps/mobile and report its screens.')
    const report = explorerCard.getByRole('region', { name: 'Report' })
    await expect(report).toContainText('The app has three screens')
    // Read as Markdown, not shown raw.
    await expect(report).not.toContainText('**')

    const reviewerCard = transcript.getByRole('group', { name: 'Reviewer: Review the diff' })
    await expect(reviewerCard.getByRole('status')).toHaveText('Running')
    // Not a tool row in a cluster.
    await expect(transcript.getByRole('button', { name: /^Task:/ })).toHaveCount(0)

    await explorerCard.getByRole('button', { name: 'Open subagent session' }).click()
    await expect(page).toHaveURL(new RegExp(explorer.sessionId))
    await expect(transcript).toContainText('The app has three screens: list, session, settings.')
  })

  test('the header leads to each subagent and back to the calling Session', async ({
    page,
    openClient,
    openSidebar,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Plan the release/)

    const menu = page.getByRole('button', { name: '2 subagents, 1 running' })
    await menu.click()
    const popup = page.getByRole('menu', { name: 'Subagents' })
    await expect(popup.getByRole('menuitem')).toHaveCount(2)
    await popup.getByRole('menuitem', { name: /Reviewer: Review the diff/ }).click()
    await expect(page).toHaveURL(new RegExp(reviewer.sessionId))

    // Inside a subagent the title is a trail; the calling Session stays selected in the list.
    const trail = page.getByRole('navigation', { name: 'Session hierarchy' })
    await expect(trail.getByRole('button', { name: 'Plan the release' })).toBeVisible()
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Plan the release/ })).toHaveAttribute(
      'aria-current',
      'page',
    )
    if (await page.getByRole('dialog', { name: 'Sessions' }).isVisible()) {
      await page.keyboard.press('Escape')
      await drawerGone(page)
    }

    // The current crumb switches between siblings.
    await trail.getByRole('button', { name: /Reviewer: Review the diff/ }).click()
    await page.getByRole('menuitemradio', { name: /Explorer: Map the mobile app/ }).click()
    await expect(page).toHaveURL(new RegExp(explorer.sessionId))

    await trail.getByRole('button', { name: 'Plan the release' }).click()
    await expect(page).toHaveURL(new RegExp(main.sessionId))
    await expect(page.getByRole('navigation', { name: 'Session hierarchy' })).toHaveCount(0)
  })

  test('a subagent started while the Session is open joins its header and its card', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Fix the login bug/)
    const child = session('Worker: Patch the token refresh', other.cwd, [], {
      subagent: {
        callingSessionId: other.sessionId,
        callingToolUseId: 'toolu_live',
        subagentType: 'worker',
        description: 'Patch the token refresh',
        status: 'running',
      },
    })
    fakeDaemon.scenario.sessions.push(child)
    const now = Date.now()
    fakeDaemon.notify(other.sessionId, {
      type: 'create_message',
      message: {
        id: 'task_call',
        role: 'assistant',
        content: [
          {
            type: 'tool_use',
            id: 'toolu_live',
            name: 'Task',
            input: {
              subagent_type: 'worker',
              description: 'Patch the token refresh',
              prompt: 'Await the refresh.',
            },
          },
        ],
        createdAt: now,
        updatedAt: now,
      },
    })
    fakeDaemon.notify(other.sessionId, {
      type: 'child_session_available',
      childSessionId: child.sessionId,
      toolUseId: 'toolu_live',
      subagentType: 'worker',
      description: 'Patch the token refresh',
      timestamp: now,
    })

    await expect(page.getByRole('button', { name: '1 subagent, 1 running' })).toBeVisible()
    const card = page
      .getByRole('log', { name: 'Transcript' })
      .getByRole('group', { name: 'Worker: Patch the token refresh' })
    await expect(card.getByRole('status')).toHaveText('Running')
    await card.getByRole('button', { name: 'Open subagent session' }).click()
    await expect(page).toHaveURL(new RegExp(child.sessionId))
  })

  test('the calling Session’s row says how many subagents are running', async ({
    openClient,
    openSidebar,
    pickSession,
  }) => {
    await openClient()
    // Loading the Session is what tells this Client about its subagents.
    await pickSession(/Plan the release/)
    await pickSession(/Fix the login bug/)
    const row = (await openSidebar()).getByRole('button', { name: /Plan the release/ })
    await expect(row.getByRole('status')).toHaveText('1 subagent running')
  })
})

test.describe('back from a subagent', () => {
  // A long Session whose Task call sits part way up, among tall tool turns.
  const turns = (name: string, count: number): MessageFixture[] =>
    Array.from({ length: count }, (_, turn) => {
      const id = `${name}-${turn}`
      return [
        userMessage(`${name} question ${turn}`),
        {
          ...assistantMessage(''),
          content: [
            thinkingBlock('Thinking it over. '.repeat(40), 4000),
            { type: 'tool_use', id, name: 'Execute', input: { command: `echo ${name} ${turn}` } },
          ],
        },
        toolResultMessage(id, 'line\n'.repeat(5)),
        assistantMessage(`## ${name} answer ${turn}\n\n- one\n- two\n- three`),
      ]
    }).flat()
  const caller = session('Long release plan', '/Users/dev/acme-web', [
    ...turns('Before', 20),
    userMessage('Map the app'),
    toolCallMessage('toolu_map', 'Task', {
      subagent_type: 'explorer',
      description: 'Map the app',
      prompt: 'Explore the app.',
    }),
    toolResultMessage('toolu_map', 'Three screens.'),
    ...turns('After', 10),
  ])
  const mapper = session(
    'Explorer: Map the app',
    '/Users/dev/acme-web',
    [userMessage('Explore the app.')],
    {
      sessionId: '0c1d2e3f-0000-4000-8000-00000000e009',
      subagent: {
        callingSessionId: caller.sessionId,
        callingToolUseId: 'toolu_map',
        subagentType: 'explorer',
        description: 'Map the app',
        status: 'completed',
      },
    },
  )
  test.use({ scenario: { sessions: [caller, mapper] } })

  test('lands on the card it was opened from', async ({ page, openClient, pickSession }) => {
    await openClient()
    await pickSession(/Long release plan/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('After answer 9')).toBeInViewport()
    await page.waitForTimeout(300)
    const card = transcript.getByRole('group', { name: 'Explorer: Map the app' })
    // Scroll up until the card is rendered, the way a reader would.
    await expect(async () => {
      await transcript.evaluate((el) => el.scrollBy({ top: -500 }))
      await expect(card).toBeInViewport({ timeout: 100 })
    }).toPass()
    await page.waitForTimeout(100)
    const before = (await card.boundingBox())!.y

    await card.getByRole('button', { name: 'Open subagent session' }).click()
    await expect(page).toHaveURL(new RegExp(mapper.sessionId))
    await page
      .getByRole('navigation', { name: 'Session hierarchy' })
      .getByRole('button', { name: 'Long release plan' })
      .click()
    await expect(page).toHaveURL(new RegExp(caller.sessionId))
    await expect(card).toBeInViewport()
    await page.waitForTimeout(300)
    expect(Math.abs((await card.boundingBox())!.y - before)).toBeLessThanOrEqual(2)
  })
})
