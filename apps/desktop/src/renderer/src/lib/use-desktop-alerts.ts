// Session alerts in the web Client: the shared unread marks, plus, in the
// Local Client, the Desktop Shell's sound and a desktop notification that
// opens the Session when it is out of sight.
import { useEffect, useRef } from 'react'
import { useSessionAlerts } from '@droi/daemon-layer/use-session-alerts'
import type { AlertsBridge } from '@shared/alerts'
import { alertPreferences, playAlertSound } from './alerts'

export function useDesktopAlerts({
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
  const latestOnOpen = useRef(onOpen)
  useEffect(() => {
    latestOnOpen.current = onOpen
  })

  const unread = useSessionAlerts({
    selectedId,
    onAlert: (sessionId, alert) => {
      if (!bridge) return
      const preferences = alertPreferences.get()
      void playAlertSound(bridge, preferences, alert)
      if (document.hasFocus() && selectedId === sessionId) return
      const title = titleOf(sessionId)
      if (alert === 'completion' && preferences.notifyOnComplete) {
        void bridge.notify({ title: 'Droid finished', body: title, sessionId })
      }
      if (alert === 'awaiting-input' && preferences.notifyOnWaitingForInput) {
        void bridge.notify({
          title: 'Droid needs input',
          body: `${title} — waiting for your answer`,
          sessionId,
        })
      }
    },
  })

  useEffect(
    () => bridge?.onNotificationClick((sessionId) => latestOnOpen.current(sessionId)),
    [bridge],
  )
  return unread
}
