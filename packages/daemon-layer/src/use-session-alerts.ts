// Turns Session working states into alerts (see alerts.ts). Every Client
// marks unread the Sessions that finished or started waiting while another
// one was open; what else an alert does is the Client's `onAlert`. A
// `/compact` this Client ran counts as a completion when it finishes.
//
// The marks are the computer's when it keeps the Session Times
// (session-times.ts): a Session shown on any Client reads on every one, and
// this Client tells the computer when it shows one, as it opens or as its
// turn ends on screen. Without them the marks are this Client's own.
import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertTracker, type AlertEvent } from './alerts'
import { compactions } from './compaction'
import { useDaemonConnection } from './connection-context'
import { LOAD_STATE, SESSION_EVENT } from './sdk-enums'
import { isUnread, useSessionTimes } from './session-times'

const NONE: ReadonlySet<string> = new Set()

export function useSessionAlerts({
  selectedId,
  onAlert,
}: {
  selectedId: string | null
  onAlert: (sessionId: string, alert: AlertEvent) => void
}): ReadonlySet<string> {
  const { sessionState } = useDaemonConnection()
  const { times, kept, viewed } = useSessionTimes()
  const [unread, setUnread] = useState<ReadonlySet<string>>(NONE)
  // The subscription outlives renders; it reads the latest values from here.
  const latest = useRef({ selectedId, onAlert, viewed })
  useEffect(() => {
    latest.current = { selectedId, onAlert, viewed }
  })

  useEffect(() => {
    const raise = (sessionId: string, alert: AlertEvent) => {
      const now = latest.current
      if (now.selectedId === sessionId) {
        now.viewed(sessionId)
      } else {
        setUnread((prev) => (prev.has(sessionId) ? prev : new Set(prev).add(sessionId)))
      }
      now.onAlert(sessionId, alert)
    }
    const tracker = new AlertTracker()
    const unsubscribe = sessionState.subscribeToSessionEvents(
      [SESSION_EVENT.workingStateChanged, SESSION_EVENT.loadStateChanged],
      (event, payload) => {
        const sessionId = payload.sessionId
        if (event === SESSION_EVENT.loadStateChanged) {
          if (sessionState.getSessionLoadState(sessionId) !== LOAD_STATE.loaded) {
            tracker.forget(sessionId)
          }
          return
        }
        const manager = sessionState.getSessionManager(sessionId)
        // A subagent reports back to its calling Session; that Session's own alert is the one to see.
        if (manager?.getStore().getCallingSessionId()) return
        const state = manager?.getDroidWorkingState() ?? 'idle'
        const alert = tracker.update(sessionId, state)
        if (alert) raise(sessionId, alert)
      },
    )
    const offDone = compactions.onDone(({ sessionId }) => raise(sessionId, 'completion'))
    return () => {
      unsubscribe()
      offDone()
    }
  }, [sessionState])

  // Opening a Session reads it, here and on the computer.
  useEffect(() => {
    if (selectedId) viewed(selectedId)
  }, [selectedId, viewed])
  if (selectedId && unread.has(selectedId)) {
    const next = new Set(unread)
    next.delete(selectedId)
    setUnread(next)
  }

  // The Session on screen is never unread: the end of its turn can reach the
  // Session Times before this Client notes it seen.
  const shared = useMemo(() => {
    const out = new Set<string>()
    for (const [id, t] of times) if (id !== selectedId && isUnread(t)) out.add(id)
    return out.size > 0 ? out : NONE
  }, [times, selectedId])
  return kept ? shared : unread
}
