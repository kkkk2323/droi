// The Phone App's alert switches: a sound and a haptic per event, each on or
// off, kept on the phone.
import type { AlertEvent } from '@droi/daemon-layer/alerts'
import { createPreference } from '@droi/daemon-layer/local-preference'
import type { AlertSound } from '../platform/feedback'

export interface AlertSwitches {
  finishedSound: boolean
  finishedHaptic: boolean
  needsInputSound: boolean
  needsInputHaptic: boolean
}

export const DEFAULT_ALERT_SWITCHES: AlertSwitches = {
  finishedSound: true,
  finishedHaptic: true,
  needsInputSound: true,
  needsInputHaptic: true,
}

export function parseAlertSwitches(value: unknown): AlertSwitches {
  const v = (value && typeof value === 'object' ? value : {}) as Record<string, unknown>
  const flag = (key: keyof AlertSwitches) =>
    typeof v[key] === 'boolean' ? (v[key] as boolean) : DEFAULT_ALERT_SWITCHES[key]
  return {
    finishedSound: flag('finishedSound'),
    finishedHaptic: flag('finishedHaptic'),
    needsInputSound: flag('needsInputSound'),
    needsInputHaptic: flag('needsInputHaptic'),
  }
}

export const alertSwitches = createPreference<AlertSwitches>(
  'droi.phoneAlerts',
  DEFAULT_ALERT_SWITCHES,
  { parse: (raw) => parseAlertSwitches(JSON.parse(raw)), serialize: JSON.stringify },
)

/** What an alert plays: null where its switch is off. */
export function feedbackFor(
  switches: AlertSwitches,
  alert: AlertEvent,
): { sound: AlertSound | null; haptic: AlertSound | null } {
  if (alert === 'completion') {
    return {
      sound: switches.finishedSound ? 'finished' : null,
      haptic: switches.finishedHaptic ? 'finished' : null,
    }
  }
  return {
    sound: switches.needsInputSound ? 'needs-input' : null,
    haptic: switches.needsInputHaptic ? 'needs-input' : null,
  }
}
