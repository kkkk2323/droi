import type { Harness } from '@earendil-works/pi-durable'
import type { Context } from '@earendil-works/chord'
import { info } from './log.ts'

/** The entry kinds that wait for a person's answer. */
const WAITING = new Set(['droi.approval', 'droi.ask'])
const ABANDONED = 'abandoned'
const SCAN = 500

/**
 * Close the questions a killed process left open.
 *
 * Entries are immutable: a `droi.approval` or `droi.ask` that was waiting when the process died stays
 * `pending` forever, and a client would show "waiting for an answer" nobody can give. Pi does not resume
 * the waiting tool — it ends as `interrupted` — so the kernel appends a second entry with the same
 * `requestId` and status `abandoned`, which is what a client reads as "this question is over".
 *
 * Only conversations with a live task are scanned: a question can wait only inside a run, so a
 * conversation without one cannot hold one open from this crash.
 *
 * **M3 must revisit this.** A non-blocking `ask_user` (design §3.2) records its `droi.ask` and returns at
 * once, so the question outlives the run and a crashed process leaves a `pending` entry in a conversation
 * with no live task — which this scan would not see. When that lands, the scan has to cover every
 * conversation with a pending entry, or the kernel needs a marker the start-up path can find cheaply.
 * Recorded in the Workstream's hand-off notes.
 */
export async function reconcilePending(harness: Harness, context: Context): Promise<number> {
  const live = await harness.inspect(context)
  const conversations = new Set<number>()
  for (const task of live.tasks) conversations.add(task.record.conversationId as unknown as number)

  let closed = 0
  for (const conversationId of conversations) {
    const conversation = await harness.conversation(conversationId as never, context)
    if (conversation === undefined) continue
    // The last entry per request decides, so a request answered before the crash is not reopened.
    const open = new Map<string, string>()
    let cursor: never | undefined
    do {
      const page = await conversation.entries({ order: 'ascending' }, SCAN, cursor, context)
      for (const entry of page.items) {
        if (!WAITING.has(entry.kind)) continue
        const data = entry.data as { requestId?: unknown; status?: unknown } | undefined
        if (typeof data?.requestId !== 'string') continue
        if (data.status === 'pending') open.set(data.requestId, entry.kind)
        else open.delete(data.requestId)
      }
      cursor = page.next as never
    } while (cursor !== undefined)
    if (open.size === 0) continue
    const requests = [...open.entries()]
    await harness.commit(async (tx) => {
      for (const [requestId, kind] of requests) {
        await tx.appendEntry(conversationId as never, {
          kind,
          data: { requestId, status: ABANDONED },
        })
      }
    }, context)
    closed += open.size
    info('abandoned open questions', { conversationId, count: open.size })
  }
  return closed
}
