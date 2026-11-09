// What the Sessions are doing right now, for the list's activity marks: working, or
// stopped until a person answers (an approval, a question). The Kernel tells every
// connection when a conversation's run starts and ends, and the list says which run when
// this Client connects. A `/compact` this Client started shows as compacting from its own
// log (compaction.ts).
import { useMemo } from 'react'
import { useCompactions } from './compaction'
import type { KernelSnapshot } from './kernel-state'
import { useKernelSnapshot } from './kernel-state'
import { sessionIdOf } from './kernel-wire'

export type SessionActivity = 'working' | 'needs-input' | 'compacting'

const NONE: ReadonlyMap<string, SessionActivity> = new Map()

/** A Session's activity from what it is doing, and how many questions it holds. */
export function activityOf(
  workingState: string | null | undefined,
  pendingAskUser = 0,
): SessionActivity | null {
  if (!workingState || workingState === 'idle') return pendingAskUser > 0 ? 'needs-input' : null
  if (workingState === 'waiting_for_tool_confirmation') return 'needs-input'
  if (workingState === 'compacting_conversation') return 'compacting'
  return 'working'
}

/** How many of some Sessions are busy, for a folded group's header. */
export interface Busy {
  working: number
  needsInput: number
}

export function countBusy(
  sessions: readonly { sessionId: string }[],
  activity: ReadonlyMap<string, SessionActivity>,
  subagentsRunning: ReadonlyMap<string, number> = new Map(),
): Busy | null {
  let working = 0
  let needsInput = 0
  for (const { sessionId } of sessions) {
    const doing = activity.get(sessionId)
    if (doing === 'needs-input') needsInput += 1
    else if (doing || (subagentsRunning.get(sessionId) ?? 0) > 0) working += 1
  }
  return working + needsInput > 0 ? { working, needsInput } : null
}

/** Every Session's activity from the Kernel's snapshot. */
export function activities(kernel: KernelSnapshot): ReadonlyMap<string, SessionActivity> {
  const out = new Map<string, SessionActivity>()
  for (const id of kernel.busy.keys()) out.set(sessionIdOf(id), 'working')
  for (const request of kernel.approvals.values()) out.set(sessionIdOf(request.id), 'needs-input')
  // A blocking question stops the run; one that does not block waits once the run rests.
  for (const request of kernel.asks.values()) {
    if (request.blocking !== false || !kernel.busy.has(request.id))
      out.set(sessionIdOf(request.id), 'needs-input')
  }
  return out.size === 0 ? NONE : out
}

export function useSessionActivity(): ReadonlyMap<string, SessionActivity> {
  const kernel = useKernelSnapshot()
  const reported = useMemo(() => activities(kernel), [kernel])
  const { pending } = useCompactions()
  return useMemo(() => {
    if (pending.size === 0) return reported
    const merged = new Map(reported)
    for (const id of pending) merged.set(id, 'compacting')
    return merged
  }, [reported, pending])
}
