// Workstreams (ADR 0036): the Host's orchestrator stays in the Session list,
// its agents leave it. The orchestrator's row counts them, its header lists
// them by whether they wait on the user, and an agent leads back to it.
import type { FakeDaemon } from '@droi/fake-daemon/fake-daemon'
import type { JsonRpcRequest } from '@droi/fake-daemon/protocol'
import {
  assistantMessage,
  session,
  userMessage,
  type SessionFixture,
} from '@droi/fake-daemon/scenario'
import { permissionTurn } from '@droi/fake-daemon/turns'
import {
  expect,
  inReadingOrder,
  openSessionList,
  pairPhone,
  pickSession,
  sheetsClosed,
  test,
} from './fixtures'

const repo = '/Users/dev/acme-pay'
const workstreamId = 'ws-payments'

const orchestrator = session(
  'Payments migration',
  repo,
  [
    userMessage('Move the payment service from REST to gRPC, one endpoint at a time.'),
    assistantMessage('I started two agents: one for the API, one for the clients.'),
  ],
  { tags: [{ name: 'droi.workstream', metadata: { workstreamId, role: 'orchestrator' } }] },
)

function agent(
  title: string,
  branch: string,
  messages = [userMessage(`[From orchestrator]\n${title}`)],
) {
  const path = `/Users/dev/.factory/worktrees/acme-pay-${branch}`
  return session(title, path, messages, {
    tags: [
      {
        name: 'droi.workstream',
        metadata: {
          workstreamId,
          role: 'agent',
          orchestratorId: orchestrator.sessionId,
          agentModel: 'glm-5.3-flash',
          agentReasoningEffort: 'high',
        },
      },
    ],
    worktree: { path, branch: `droi/${branch}`, lifecycle: 'persistent', repoRoot: repo },
  })
}

const api = agent('Port the charge endpoint', 'charge')
const clients = agent('Switch the web client', 'web-client')
const docs = agent('Write the migration notes', 'notes')
const retired: SessionFixture = {
  ...agent('Spike the proto layout', 'spike'),
  archivedAt: '2026-11-01T10:00:00.000Z',
}
const other = session('Fix the login bug', repo, [userMessage('Why does login fail?')])
// A run of one of Droi's own Automations, which leaves the list as the Daemon's runs do.
const run = session('Nightly triage', repo, [userMessage('Triage the new issues.')], {
  tags: [{ name: 'droi.automation', metadata: { automationId: 'nightly' } }],
})

test.use({
  scenario: {
    sessions: [orchestrator, api, clients, docs, retired, other, run],
    // What list_opened_sessions reports at connect; askPermission plays the waiting turn itself.
    opened: [
      { sessionId: api.sessionId, workingState: 'waiting_for_tool_confirmation' },
      { sessionId: clients.sessionId, workingState: 'streaming_assistant_message' },
    ],
  },
})

/**
 * The Host sends the agent a message and its turn stops on a permission
 * request, which a Client that loads the agent later gets with the load
 * (`pendingPermissions`).
 */
function askPermission(daemon: FakeDaemon) {
  void permissionTurn({
    command: 'go test ./payments/...',
    reply: 'The charge endpoint passes its tests.',
  })(
    { sessionId: api.sessionId, text: '[From orchestrator]\nRun the tests.' },
    { daemon, connectionId: 0 },
    { id: 'host-turn' } as unknown as JsonRpcRequest,
  )
}
test('the list shows the orchestrator with its counts, and not its agents', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  askPermission(fakeDaemon)
  const list = await openSessionList(page)
  const row = list.getByRole('button', { name: /Payments migration/ })
  await expect(row.getByRole('img', { name: 'Workstream · 3 agents' })).toBeVisible()
  await expect(row.getByRole('status', { name: '1 waiting' })).toBeVisible()
  await expect(list.getByRole('button', { name: /Fix the login bug/ })).toBeVisible()
  for (const hidden of [api, clients, docs, retired, run]) {
    await expect(list.getByRole('button', { name: new RegExp(hidden.title) })).toHaveCount(0)
  }
})

test('the orchestrator lists its agents; one waiting is answered in its own Session', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  askPermission(fakeDaemon)
  await pickSession(page, /Payments migration/)
  await page.getByRole('button', { name: '3 agents, 1 waiting on you' }).click()
  const sheet = page.getByRole('dialog', { name: 'Agents' })
  const group = (name: string) => sheet.getByRole('group', { name })
  await expect(group('Waiting on you').getByRole('button')).toHaveText([/Port the charge endpoint/])
  await expect(group('Working').getByRole('button')).toHaveText([/Switch the web client/])
  await expect(group('Idle').getByRole('button')).toHaveText([/Write the migration notes/])
  await expect(sheet.getByRole('button', { name: retired.title })).toHaveCount(0)

  await group('Waiting on you').getByRole('button', { name: api.title }).click()
  await sheetsClosed(page)
  const workstream = page.getByRole('navigation', { name: 'Workstream' })
  await expect(workstream.getByRole('heading', { name: api.title })).toBeVisible()
  const card = page.getByRole('group', { name: 'Permission request: Execute' })
  await expect(card).toContainText('go test ./payments/...')
  await card.getByRole('button', { name: 'Yes, allow' }).click()
  await expect(card).toHaveCount(0)
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText(
    'The charge endpoint passes its tests.',
  )

  // The way back leads to the orchestrator, now with nobody waiting.
  await workstream.getByRole('button', { name: 'Workstream: Payments migration' }).click()
  await expect(page.getByRole('navigation', { name: 'Workstream' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: orchestrator.title })).toBeVisible()
  await expect(page.getByRole('button', { name: '3 agents', exact: true })).toBeVisible()
})

test.describe('before the orchestrator starts any agent', () => {
  test.use({ scenario: { sessions: [orchestrator, other] } })

  test('its row and its Agents list say so', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    const list = await openSessionList(page)
    await expect(
      list
        .getByRole('button', { name: /Payments migration/ })
        .getByRole('img', { name: 'Workstream · 0 agents' }),
    ).toBeVisible()
    await pickSession(page, /Payments migration/)
    await page.getByRole('button', { name: '0 agents' }).click()
    await expect(page.getByRole('dialog', { name: 'Agents' })).toContainText(
      'No agents yet. The orchestrator starts them as it splits the work.',
    )
  })
})

test.describe('an orchestrator last used long ago', () => {
  const tenDaysAgo = Math.floor(Date.now() / 1000) - 10 * 24 * 60 * 60
  const quiet = { ...orchestrator, updatedAt: tenDaysAgo }
  const review = session('Review the release notes', repo, [userMessage('Read them.')])
  test.use({
    scenario: {
      sessions: [quiet, api, docs, other, review],
      opened: [{ sessionId: api.sessionId, workingState: 'waiting_for_tool_confirmation' }],
    },
  })

  test('comes first and stays in sight while an agent waits on you', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    const list = await openSessionList(page)
    const row = list.getByRole('button', { name: /Payments migration/ })
    await expect(row).toBeVisible()
    await expect(row.getByRole('status', { name: '1 waiting' })).toBeVisible()
    const rows = list.getByRole('group', { name: 'acme-pay' }).getByRole('button', {
      name: /Payments migration|Fix the login bug|Review the release notes/,
    })
    expect(await inReadingOrder(rows)).toEqual([
      expect.stringContaining('Payments migration'),
      expect.stringContaining('Review the release notes'),
      expect.stringContaining('Fix the login bug'),
    ])
  })

  test('in the time view it is in Right now, with its Workspace and its agents', async ({
    page,
    fakeDaemon,
  }) => {
    // The computer's list order, which the Fake Daemon's Gateway does not serve: by time.
    await page.route(
      (url) => url.pathname === '/session-list',
      (route) =>
        route.fulfill({
          headers: { 'access-control-allow-origin': '*' },
          json: {
            pinnedSessions: [],
            pinnedWorkspaces: [],
            workspaceOrder: [],
            sessionSort: '',
            sessionView: 'time',
          },
        }),
    )
    await pairPhone(page, fakeDaemon)
    const list = await openSessionList(page)
    const row = list
      .getByRole('group', { name: 'Right now' })
      .getByRole('button', { name: /Payments migration/ })
    await expect(row.getByRole('img', { name: 'In acme-pay' })).toBeVisible()
    await expect(row.getByRole('img', { name: 'Workstream · 2 agents' })).toBeVisible()
    await expect(row.getByRole('status', { name: '1 waiting' })).toBeVisible()
  })
})
