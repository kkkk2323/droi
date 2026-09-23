// Turns Session working states into alerts (see alerts.ts). Every Client
// keeps unread marks for Sessions that finished or started waiting while
// another one was open; what else an alert does is the Client's `onAlert`.
import { useEffect, useRef, useState } from 'react'
import { AlertTracker, type AlertEvent } from './alerts'
import { useDaemonConnection } from './connection-context'
import { LOAD_STATE, SESSION_EVENT } from './sdk-enums'

const NONE: ReadonlySet<string> = new Set()

export function useSessionAlerts({
  selectedId,
  onAlert,
}: {
  selectedId: string | null
  onAlert: (sessionId: string, alert: AlertEvent) => void
}): ReadonlySet<string> {
  const { sessionState } = useDaemonConnection()
  const [unread, setUnread] = useState<ReadonlySet<string>>(NONE)
  // The subscription outlives renders; it reads the latest values from here.
  const latest = useRef({ selectedId, onAlert })
  useEffect(() => {
    latest.current = { selectedId, onAlert }
  })

  useEffect(() => {
    const tracker = new AlertTracker()
    return sessionState.subscribeToSessionEvents(
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
        if (!alert) return
        const now = latest.current
        if (now.selectedId !== sessionId) {
          setUnread((prev) => (prev.has(sessionId) ? prev : new Set(prev).add(sessionId)))
        }
        now.onAlert(sessionId, alert)
      },
    )
  }, [sessionState])

  // Opening a Session reads it.
  if (selectedId && unread.has(selectedId)) {
    const next = new Set(unread)
    next.delete(selectedId)
    setUnread(next)
  }
  return unread
}
