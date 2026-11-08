// Turns what the Sessions are doing into alerts (see alerts.ts): once when one starts waiting
// for an answer, and once when one goes idle after working. What an alert does is the Client's
// `onAlert`. A `/compact` this Client ran counts as a completion when it finishes.
//
// The unread marks are the Kernel's (session-times.ts): a Session whose turn ends while it is
// on screen is marked seen here, so it reads on every Client.
import { useEffect, useMemo, useRef } from 'react'
import { AlertTracker, type AlertEvent } from './alerts'
import { compactions } from './compaction'
import { useKernelConnection } from './connection-context'
import { kernelStore, type KernelSnapshot } from './kernel-state'
import { isUnread, useMarkViewed } from './session-times'
import type { SessionSummary } from './sessions'
import { activities } from './use-session-activity'

const NONE: ReadonlySet<string> = new Set()

/** Each Session's working state, in the words the AlertTracker reads. */
export function workingStates(kernel: KernelSnapshot): Map<string, string> {
  const states = new Map<string, string>()
  for (const [id, activity] of activities(kernel)) {
    states.set(id, activity === 'needs-input' ? 'waiting_for_tool_confirmation' : 'thinking')
  }
  return states
}

export function useSessionAlerts({
  sessions,
  selectedId,
  onAlert,
}: {
  /** The listed Sessions, whose marks say which are unread. */
  sessions: readonly SessionSummary[]
  selectedId: string | null
  onAlert: (sessionId: string, alert: AlertEvent) => void
}): ReadonlySet<string> {
  const connection = useKernelConnection()
  const markViewed = useMarkViewed()
  // The subscription outlives renders; it reads the latest values from here.
  const latest = useRef({ selectedId, onAlert, sessions, markViewed })
  useEffect(() => {
    latest.current = { selectedId, onAlert, sessions, markViewed }
  })

  useEffect(() => {
    const store = kernelStore(connection)
    const tracker = new AlertTracker()
    let generation = -1
    let tracked = new Set<string>()
    const raise = (sessionId: string, alert: AlertEvent) => {
      const now = latest.current
      if (now.selectedId === sessionId) {
        const shown = now.sessions.find((s) => s.sessionId === sessionId)
        now.markViewed({ sessionId, activityAt: shown?.activityAt ?? 0 })
      }
      now.onAlert(sessionId, alert)
    }
    const update = () => {
      const snapshot = store.snapshot()
      // A new socket starts afresh: what ran meanwhile is no news.
      if (snapshot.generation !== generation) {
        generation = snapshot.generation
        for (const id of tracked) tracker.forget(id)
        tracked = new Set()
      }
      const states = workingStates(snapshot)
      for (const id of new Set([...tracked, ...states.keys()])) {
        const alert = tracker.update(id, states.get(id) ?? 'idle')
        if (alert) raise(id, alert)
      }
      tracked = new Set(states.keys())
    }
    update()
    const offStore = store.subscribe(update)
    const offDone = compactions.onDone(({ sessionId }) => raise(sessionId, 'completion'))
    return () => {
      offStore()
      offDone()
    }
  }, [connection])

  // The Session on screen is never unread: the end of its turn can reach the list before
  // this Client marks it seen.
  return useMemo(() => {
    const out = new Set<string>()
    for (const s of sessions) if (s.sessionId !== selectedId && isUnread(s)) out.add(s.sessionId)
    return out.size > 0 ? out : NONE
  }, [sessions, selectedId])
}
