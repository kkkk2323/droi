// Turns Session working states into alerts (see lib/alerts.ts). Every Client
// keeps unread marks for Sessions that finished or started waiting while
// another one was open; the Local Client also plays the sound and, when the
// Session is out of sight, shows a desktop notification that opens it.
import { useEffect, useRef, useState } from 'react'
import type { AlertsBridge } from '@shared/alerts'
import { AlertTracker, alertPreferences, playAlertSound } from '@/lib/alerts'
import { useDaemonConnection } from './connection-context'
import { LOAD_STATE, SESSION_EVENT } from './sdk-enums'

const NONE: ReadonlySet<string> = new Set()

export function useSessionAlerts({
  bridge,
  selectedId,
  titleOf,
  onOpen,
}: {
  bridge: AlertsBridge | null
  selectedId: string | null
  titleOf: (sessionId: string) => string
  onOpen: (sessionId: string) => void
}): ReadonlySet<string> {
  const { sessionState } = useDaemonConnection()
  const [unread, setUnread] = useState<ReadonlySet<string>>(NONE)
  // The subscription outlives renders; it reads the latest values from here.
  const latest = useRef({ bridge, selectedId, titleOf, onOpen })
  useEffect(() => {
    latest.current = { bridge, selectedId, titleOf, onOpen }
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
        const state = sessionState.getSessionManager(sessionId)?.getDroidWorkingState() ?? 'idle'
        const alert = tracker.update(sessionId, state)
        if (!alert) return
        const now = latest.current
        if (now.selectedId !== sessionId) {
          setUnread((prev) => (prev.has(sessionId) ? prev : new Set(prev).add(sessionId)))
        }
        if (!now.bridge) return
        const preferences = alertPreferences.get()
        void playAlertSound(now.bridge, preferences, alert)
        if (document.hasFocus() && now.selectedId === sessionId) return
        const title = now.titleOf(sessionId)
        if (alert === 'completion' && preferences.notifyOnComplete) {
          void now.bridge.notify({ title: 'Droid finished', body: title, sessionId })
        }
        if (alert === 'awaiting-input' && preferences.notifyOnWaitingForInput) {
          void now.bridge.notify({
            title: 'Droid needs input',
            body: `${title} — waiting for your answer`,
            sessionId,
          })
        }
      },
    )
  }, [sessionState])

  useEffect(
    () => bridge?.onNotificationClick((sessionId) => latest.current.onOpen(sessionId)),
    [bridge],
  )

  // Opening a Session reads it.
  if (selectedId && unread.has(selectedId)) {
    const next = new Set(unread)
    next.delete(selectedId)
    setUnread(next)
  }
  return unread
}
