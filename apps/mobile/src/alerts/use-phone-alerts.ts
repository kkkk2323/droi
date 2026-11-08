// Session alerts on the phone: the shared unread marks, plus a sound and a
// haptic for Sessions other than the one on screen.
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { useSessionAlerts } from '@droi/daemon-layer/use-session-alerts'
import { haptic, playSound } from '../platform/feedback'
import { alertSwitches, feedbackFor } from './alert-preferences'

export function usePhoneAlerts(
  selectedId: string | null,
  sessions: readonly SessionSummary[],
): ReadonlySet<string> {
  return useSessionAlerts({
    sessions,
    selectedId,
    onAlert: (sessionId, alert) => {
      if (sessionId === selectedId) return
      const { sound, haptic: feel } = feedbackFor(alertSwitches.get(), alert)
      if (sound) void playSound(sound).catch((cause) => console.warn('Alert sound', cause))
      if (feel) haptic(feel)
    },
  })
}
