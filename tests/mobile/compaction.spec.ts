// `/compact` on the Kernel: a compaction task summarises the older part of the conversation in
// place. The phone marks the Session while it runs and says what it folded away.
import { assistantEntry, userEntry } from './fake-kernel/history'
import { expect, openSessionList, pairPhone, pickSession, test } from './fixtures'

// Long enough that the Kernel keeps only the newest part and summarises the rest.
const long = (n: number) => `${'detail '.repeat(6_000)}${n}`

test.use({
  scenario: {
    sessions: [
      {
        title: 'Long chat',
        cwd: 'acme-web',
        entries: [1, 2, 3, 4].flatMap((n) => [userEntry(`question ${n}`), assistantEntry(long(n))]),
      },
    ],
  },
})

test('/compact marks the Session while it runs and reports what it folded away', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  // The summary the Kernel asks the model for, slowly.
  fakeKernel.model.push({ text: 'The summary so far.', delayMs: 1_500 })
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Long chat/)
  const input = page.getByRole('textbox', { name: 'Message' })
  await expect(input).toBeEditable()
  await input.pressSequentially('/compact ')
  await expect(page.getByRole('group', { name: 'Command compact' })).toBeVisible()
  await page.getByRole('button', { name: 'Send' }).click()
  await phoneWire.waitForCall('conversation.compact')

  const list = await openSessionList(page)
  const row = list.getByRole('button', { name: /Long chat/ })
  await expect(row.getByRole('status', { name: 'Compacting' })).toBeVisible()
  await row.click()
  await expect(list).toBeHidden()

  const done = page.getByRole('status').filter({ hasText: 'Compacted' })
  await expect(done).toHaveText(/^Compacted, \d+ messages? summarised$/, { timeout: 15_000 })
  const shown = Number(/(\d+)/.exec((await done.textContent()) ?? '')![1])
  await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
  await expect(
    (await openSessionList(page)).getByRole('status', { name: 'Compacting' }),
  ).toHaveCount(0)

  // The Kernel kept the summary, and the model was asked for it.
  const { items } = await fakeKernel.kernel.call<{
    items: Array<{ id: number; kind: string; head?: number }>
  }>('conversation.entries', { id: fakeKernel.session('Long chat').conversationId })
  const summary = items.find((e) => e.kind === 'pi.compaction')!
  // What it folded away: the messages before the first one it kept.
  const folded = items.filter(
    (e) => (e.kind === 'pi.user' || e.kind === 'pi.assistant') && e.id < summary.head!,
  )
  expect(folded.length).toBeGreaterThan(0)
  expect(shown).toBe(folded.length)
})
