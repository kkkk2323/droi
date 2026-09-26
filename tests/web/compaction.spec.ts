// `/compact` hands the conversation to a new Session. The Client tags the child
// with its parent, moves there, folds the parent out of the sidebar and can show
// the parent's transcript above the boundary.
import { expect, test } from './fixtures'
import { session, userMessage, assistantMessage } from '../fake-daemon/scenario'

const chat = session('Long chat', '/Users/dev/acme-web', [
  userMessage('first question'),
  assistantMessage('first answer'),
  userMessage('second question'),
  assistantMessage('second answer'),
])

test.describe('automatic compaction', () => {
  const long = session('Busy chat', '/Users/dev/acme-web', [
    userMessage('first question'),
    assistantMessage('first answer'),
    userMessage('second question'),
    assistantMessage('second answer'),
  ])
  test.use({ scenario: { sessions: [long] } })

  test('the Daemon compacting mid-turn keeps the earlier messages on screen', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    // The Daemon summarises the context in place: the Session stays, and its
    // notification names the first message the model still sees.
    const boundary = long.messages[2]!.id
    fakeDaemon.scenario.on('daemon.add_user_message', (params, { daemon }, request) => {
      const sessionId = String(params['sessionId'])
      const now = Date.now()
      daemon.notify(sessionId, {
        type: 'create_message',
        message: {
          id: String(params['messageId'] ?? 'sent'),
          role: 'user',
          content: [{ type: 'text', text: String(params['text']) }],
          createdAt: now,
          updatedAt: now,
        },
        requestId: String(request.id),
      })
      daemon.notify(sessionId, {
        type: 'droid_working_state_changed',
        newState: 'compacting_conversation',
      })
      daemon.notify(sessionId, {
        type: 'session_compacted',
        summaryId: 'summary_1',
        removedCount: 2,
        visibleBoundaryMessageId: boundary,
      })
      daemon.notify(sessionId, {
        type: 'create_message',
        message: {
          id: 'after_compaction',
          role: 'assistant',
          content: [{ type: 'text', text: 'Carrying on from the summary.' }],
          createdAt: now,
          updatedAt: now,
        },
      })
      daemon.notify(sessionId, { type: 'droid_working_state_changed', newState: 'idle' })
      return {}
    })
    await openClient()
    await pickSession(/Busy chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('first question')
    await page.getByRole('textbox', { name: 'Message' }).fill('keep going')
    await page.getByRole('button', { name: 'Send' }).click()

    await expect(transcript).toContainText('Carrying on from the summary.')
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
    await expect(transcript).toContainText('second answer')
    await expect(transcript).toContainText('first question')
    await expect(transcript).toContainText('first answer')
  })
})

test.describe('compaction handoff', () => {
  test.use({ scenario: { sessions: [chat] } })

  test('/compact moves to the child, links it to the parent and folds the parent', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
    openSidebar,
  }) => {
    await openClient()
    await pickSession(/Long chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    // The built-in command is offered like any other.
    await input.fill('/comp')
    await expect(page.getByRole('option', { name: /compact/ })).toBeVisible()
    await input.press('Enter')
    await expect(page.getByRole('group', { name: 'Command compact' })).toBeVisible()
    await input.pressSequentially('keep the decisions')
    await input.press('Enter')

    const compacted = await fakeDaemon.waitForRequest('daemon.compact_session')
    expect(compacted.params).toMatchObject({
      sessionId: chat.sessionId,
      customInstructions: 'keep the decisions',
    })
    // It is a command, not a message.
    expect(fakeDaemon.requests.filter((r) => r.method === 'daemon.add_user_message')).toHaveLength(
      0,
    )

    const tagged = await fakeDaemon.waitForRequest('daemon.update_session_settings')
    const child = String((tagged.params as Record<string, unknown>)['sessionId'])
    expect(child).not.toBe(chat.sessionId)
    expect(tagged.params).toMatchObject({
      tags: [{ name: 'droi.continues', metadata: { parent: chat.sessionId } }],
    })

    // The view moved to the child, which starts with the summary.
    await expect(page).toHaveURL(new RegExp(child))
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('Summary of the earlier conversation')
    await expect(transcript).not.toContainText('first question')

    // One row for the chain; the parent is folded away.
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Long chat/ })).toHaveCount(1)
    await expect(sidebar.getByRole('button', { name: /Long chat/ })).toContainText('1 message')

    // On the phone the list is a drawer over the transcript; put it away first.
    if (await page.getByRole('dialog', { name: 'Sessions' }).isVisible()) {
      await page.keyboard.press('Escape')
    }

    // Earlier messages load on request and sit above the boundary.
    await page.getByRole('button', { name: /Continued from “Long chat”/ }).click()
    await fakeDaemon.waitForRequest('daemon.load_session', 2)
    await expect(transcript.getByRole('article', { name: 'You' }).first()).toContainText(
      'first question',
    )
    await expect(
      transcript.getByRole('separator', { name: 'Context compacted here' }),
    ).toBeVisible()
    // (No exact article count: the list is virtualised and the phone viewport is short.)
    await expect(transcript).toContainText('second answer')
    await expect(transcript.getByRole('article').last()).toContainText(
      'Summary of the earlier conversation',
    )
  })
})

test.describe('compaction in place', () => {
  const other = session('Other chat', '/Users/dev/acme-web', [userMessage('elsewhere')])
  test.use({ scenario: { sessions: [chat, other] } })

  test('/compact that keeps the Session leaves it listed and untagged', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
    openSidebar,
  }) => {
    // Newer Daemons summarise into the same Session and answer with its own id.
    fakeDaemon.scenario.on('daemon.compact_session', (params, { daemon }) => {
      const sessionId = String(params['sessionId'])
      daemon.notify(sessionId, {
        type: 'session_compacted',
        summaryId: 'summary_1',
        removedCount: 2,
        visibleBoundaryMessageId: chat.messages[2]!.id,
      })
      return { newSessionId: sessionId, removedCount: 2 }
    })
    await openClient()
    await pickSession(/Long chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    await input.fill('/compact')
    await input.press('Enter')
    await expect(page.getByRole('group', { name: 'Command compact' })).toBeVisible()
    await input.press('Enter')
    await fakeDaemon.waitForRequest('daemon.compact_session')
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
    await expect(page).toHaveURL(new RegExp(chat.sessionId))

    await pickSession(/Other chat/)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('elsewhere')
    await pickSession(/Long chat/)
    await expect(page).toHaveURL(new RegExp(chat.sessionId))
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('second answer')
    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('button', { name: /Long chat/ })).toHaveCount(1)
    expect(
      fakeDaemon.requests.filter(
        (r) =>
          r.method === 'daemon.update_session_settings' &&
          JSON.stringify(r.params).includes('droi.continues'),
      ),
    ).toHaveLength(0)
  })
})

test.describe('a chain of compactions', () => {
  const first = session('Plan v1', '/Users/dev/acme-web', [
    userMessage('first question'),
    assistantMessage('first answer'),
  ])
  const continues = (parent: { sessionId: string }) => ({
    tags: [{ name: 'droi.continues', metadata: { parent: parent.sessionId } }],
  })
  const second = session(
    'Plan v2',
    '/Users/dev/acme-web',
    [userMessage('middle question'), assistantMessage('middle answer')],
    continues(first),
  )
  const third = session(
    'Plan v3',
    '/Users/dev/acme-web',
    [userMessage('latest question')],
    continues(second),
  )
  test.use({ scenario: { sessions: [first, second, third] } })

  test('offers each earlier Session in turn, back to the first', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Plan v3/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('latest question')
    await expect(transcript).not.toContainText('middle question')

    await page.getByRole('button', { name: /Continued from “Plan v2”/ }).click()
    await expect(transcript).toContainText('middle answer')
    await expect(transcript).not.toContainText('first question')
    await expect(transcript.getByRole('separator', { name: 'Context compacted here' })).toHaveCount(
      1,
    )

    await page.getByRole('button', { name: /Continued from “Plan v1”/ }).click()
    await expect(transcript.getByRole('article', { name: 'You' }).first()).toContainText(
      'first question',
    )
    await expect(transcript.getByRole('separator', { name: 'Context compacted here' })).toHaveCount(
      2,
    )
    await expect(page.getByRole('button', { name: /Continued from/ })).toHaveCount(0)
    await expect(transcript.getByRole('article').last()).toContainText('latest question')
  })
})
