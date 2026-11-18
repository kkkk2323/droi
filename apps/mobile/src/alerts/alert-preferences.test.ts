import { describe, expect, test } from 'vitest'
import { DEFAULT_ALERT_SWITCHES, feedbackFor, parseAlertSwitches } from './alert-preferences'

describe('feedbackFor', () => {
  test('by default each event plays its own sound and haptic', () => {
    expect(feedbackFor(DEFAULT_ALERT_SWITCHES, 'completion')).toEqual({
      sound: 'finished',
      haptic: 'finished',
    })
    expect(feedbackFor(DEFAULT_ALERT_SWITCHES, 'awaiting-input')).toEqual({
      sound: 'needs-input',
      haptic: 'needs-input',
    })
  })

  test('a switch turns off only its own event and kind', () => {
    const switches = { ...DEFAULT_ALERT_SWITCHES, finishedSound: false, needsInputHaptic: false }
    expect(feedbackFor(switches, 'completion')).toEqual({ sound: null, haptic: 'finished' })
    expect(feedbackFor(switches, 'awaiting-input')).toEqual({
      sound: 'needs-input',
      haptic: null,
    })
  })
})

describe('parseAlertSwitches', () => {
  test('keeps the stored switches', () => {
    const stored = { ...DEFAULT_ALERT_SWITCHES, needsInputSound: false }
    expect(parseAlertSwitches(stored)).toEqual(stored)
  })

  test('anything missing or malformed falls back to on', () => {
    expect(parseAlertSwitches({ finishedSound: 'no', needsInputHaptic: false })).toEqual({
      ...DEFAULT_ALERT_SWITCHES,
      needsInputHaptic: false,
    })
    expect(parseAlertSwitches(null)).toEqual(DEFAULT_ALERT_SWITCHES)
  })
})
