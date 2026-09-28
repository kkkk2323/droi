// A `/compact` the Daemon runs in place: the Daemon reports no working state
// for it, so the Phone App marks the Session from its own log while it runs
// and says what it did when it is done.
import { assistantMessage, session, userMessage } from '../fake-daemon/scenario'
import { expect, openDrawer, pairPhone, pickSession, test } from './fixtures'

const chat = session('Long chat', '/Users/dev/acme-web', [
  userMessage('first question'),
  assistantMessage('first answer'),
  userMessage('second question'),
  assistantMessage('second answer'),
])

test.use({ scenario: { sessions: [chat] } })

test('/compact marks the Session while it runs and reports what it folded away', async ({
  page,
  fakeDaemon,
}) => {
  let finish!: () => void
  const summarising = new Promise<void>((resolve) => (finish = resolve))
  fakeDaemon.scenario.on('daemon.compact_session', async (params, { daemon }) => {
    const sessionId = String(params['sessionId'])
    await summarising
    daemon.notify(sessionId, {
      type: 'session_compacted',
      summaryId: 'summary_1',
      removedCount: 2,
      visibleBoundaryMessageId: chat.messages[2]!.id,
    })
    return { newSessionId: sessionId, removedCount: 2 }
  })
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Long chat/)
  const input = page.getByRole('textbox', { name: 'Message' })
  await expect(input).toBeEditable()
  await input.pressSequentially('/compact ')
  await expect(page.getByRole('group', { name: 'Command compact' })).toBeVisible()
  await page.getByRole('button', { name: 'Send' }).click()
  await fakeDaemon.waitForRequest('daemon.compact_session')

  const list = await openDrawer(page)
  const row = list.getByRole('button', { name: /Long chat/ })
  await expect(row.getByRole('status', { name: 'Compacting' })).toBeVisible()
  await page.getByLabel('Close sessions').click()
  await expect(page.getByRole('dialog', { name: 'Sessions' })).toBeHidden()

  finish()
  await expect(page.getByRole('status').filter({ hasText: 'Compacted' })).toHaveText(
    'Compacted, 2 messages summarised',
  )
  await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
  await expect((await openDrawer(page)).getByRole('status', { name: 'Compacting' })).toHaveCount(0)
})
