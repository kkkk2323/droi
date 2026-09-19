import { expect, test } from './fixtures'
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
  return [
    userMessage('Why does login fail?'),
    assistantWithTool,
    toolResult,
    assistantMessage('The `login` function never awaits the token refresh. Here is the fix.'),
  ]
}

const richSession = session('Fix the login bug', '/Users/dev/acme-web', richHistory())

test.describe('sidebar', () => {
  test.use({ scenario: { sessions: [droiSession, anotherDroiSession, cliSession] } })

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
    await expect(acme.getByRole('button')).toHaveCount(2)
    await expect(billing.getByRole('button', { name: /Refactor billing/ })).toBeVisible()

    // Newest Workspace first, newest Session first within it.
    const headings = await sidebar.getByRole('heading', { level: 2 }).allTextContents()
    expect(headings).toEqual(['billing-service', 'acme-web'])
    const acmeTitles = await acme.getByRole('button').allTextContents()
    expect(acmeTitles[0]).toMatch(/Add dark mode/)
  })

  test('is keyboard navigable', async ({ page, openClient, openSidebar }) => {
    await openClient()
    const first = (await openSidebar()).getByRole('button', { name: /Refactor billing/ })
    await first.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('region', { name: 'Refactor billing' })).toBeVisible()
    // On narrow screens the drawer closed after the pick; open it again.
    const current = (await openSidebar()).getByRole('button', { name: /Refactor billing/ })
    await expect(current).toHaveAttribute('aria-current', 'page')
    await current.focus()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: /Add dark mode/ })).toBeFocused()
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
