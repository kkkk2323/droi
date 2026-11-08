// `/compact` on the Kernel: `conversation.compact` starts a compaction task, which summarises
// the older part of the conversation in place and places the summary as a `pi.compaction`
// entry. The task ends with a `task.changed` whose status is `terminal`; a failed one leaves
// `compactionFailure` in the conversation's `pi.live`. The run is logged in compaction.ts, which
// is where the list's mark and the finished alert come from.
import { useCallback, useState } from 'react'
import { compactions, useCompactions } from './compaction'
import type { KernelConnection } from './connection'
import { useKernelConnection } from './connection-context'
import { conversationStore } from './kernel-conversations'
import { conversationIdOf, type ConversationId, type Entry, type LiveDoc } from './kernel-wire'

export const COMPACT_COMMAND = /^\/compact(?:\s+([\s\S]*))?$/

export interface CompactActions {
  compact(instructions?: string): Promise<void>
  isCompacting: boolean
  error: string | null
}

export function useCompact(sessionId: string): CompactActions {
  const connection = useKernelConnection()
  const isCompacting = useCompactions().pending.has(sessionId)
  const [error, setError] = useState<string | null>(null)

  const compact = useCallback(
    async (instructions?: string) => {
      const id = conversationIdOf(sessionId)
      if (id === null) return
      compactions.start(sessionId)
      setError(null)
      try {
        const removed = await runCompaction(connection, id, instructions?.trim() || undefined)
        compactions.finish(sessionId, sessionId, removed)
      } catch (cause) {
        compactions.fail(sessionId)
        setError(cause instanceof Error ? cause.message : String(cause))
      }
    },
    [connection, sessionId],
  )

  return { compact, isCompacting, error }
}

/** Runs one compaction to its end; resolves to how many messages it summarised. */
export async function runCompaction(
  connection: KernelConnection,
  id: ConversationId,
  instructions: string | undefined,
): Promise<number> {
  const store = conversationStore(connection)
  const startedOn = connection.generation()
  const before = store.snapshot(id).entries.at(-1)?.id ?? 0
  // A task can end before its id comes back; the ends heard meanwhile are kept.
  const ended = new Set<number>()
  let taskId: number | null = null
  let finish: (() => void) | null = null
  const check = () => {
    if (taskId === null) return
    const snap = store.snapshot(id)
    const live = snap.docs['pi.live'] as LiveDoc | undefined
    const running = live?.compactions?.some((c) => c.taskId === taskId) ?? false
    // A socket that dropped may have missed the end: a view read on a new one says whether it ran on.
    const reread = connection.generation() !== startedOn && snap.loadState === 'loaded'
    if (ended.has(taskId) || (reread && !running)) finish?.()
  }
  const offNote = connection.onNotification(({ method, params }) => {
    if (method !== 'task.changed' || params['status'] !== 'terminal') return
    ended.add(params['taskId'] as number)
    check()
  })
  const offStore = store.subscribe(id, check)
  try {
    const answer = await connection.call<{ taskId: number }>('conversation.compact', {
      id,
      ...(instructions ? { instructions } : {}),
    })
    taskId = answer.taskId
    await new Promise<void>((resolve) => {
      finish = resolve
      check()
    })
  } finally {
    offNote()
    offStore()
  }
  await viewCaughtUp(store, id, before, taskId)
  const snap = store.snapshot(id)
  const failure = (snap.docs['pi.live'] as LiveDoc | undefined)?.compactionFailure
  if (failure && failure.taskId === taskId) throw new Error(failure.error)
  return summarised(snap.entries, before)
}

/** How long the view may trail the task's end before the count goes with what it holds. */
const VIEW_LAG_MS = 3_000

/**
 * The task's end reaches a Client before the view that holds its summary: wait for the summary
 * (or the failure) to arrive, so the count reads it.
 */
function viewCaughtUp(
  store: ReturnType<typeof conversationStore>,
  id: ConversationId,
  before: number,
  taskId: number | null,
): Promise<void> {
  return new Promise((resolve) => {
    let off = () => {}
    let timer: ReturnType<typeof setTimeout> | undefined
    const settle = () => {
      off()
      if (timer) clearTimeout(timer)
      resolve()
    }
    const check = () => {
      const snap = store.snapshot(id)
      const failure = (snap.docs['pi.live'] as LiveDoc | undefined)?.compactionFailure
      if (
        failure?.taskId === taskId ||
        snap.entries.some((e) => e.id > before && e.kind === 'pi.compaction')
      )
        settle()
    }
    off = store.subscribe(id, check)
    timer = setTimeout(settle, VIEW_LAG_MS)
    check()
  })
}

/** The messages the newest compaction after `after` folded away, as far as the entries held show. */
export function summarised(entries: readonly Entry[], after: number): number {
  let at = -1
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i]!
    if (entry.id <= after) break
    if (entry.kind === 'pi.compaction') {
      at = i
      break
    }
  }
  if (at === -1) return 0
  const head = entries[at]!.head
  const kept = typeof head === 'number' ? head : entries[at]!.id
  let count = 0
  for (let i = at - 1; i >= 0; i--) {
    const entry = entries[i]!
    if (entry.kind === 'pi.compaction' || entry.kind === 'pi.reset') break
    if (entry.id >= kept) continue
    if (entry.kind === 'pi.user' || entry.kind === 'pi.assistant') count += 1
  }
  return count
}
