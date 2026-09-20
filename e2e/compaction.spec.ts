// `/compact` hands the conversation to a new Session. The Client tags the child
// with its parent, moves there, folds the parent out of the sidebar and can show
// the parent's transcript above the boundary.
import { expect, test } from './fixtures'
import { session, userMessage, assistantMessage } from './fake-daemon/scenario'

const chat = session('Long chat', '/Users/dev/acme-web', [
  userMessage('first question'),
  assistantMessage('first answer'),
  userMessage('second question'),
  assistantMessage('second answer'),
])

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
    await expect(input).toHaveValue('/compact ')
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
