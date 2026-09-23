import { describe, expect, test } from 'vitest'
import { DEFAULT_ALERT_PREFERENCES, parseAlertPreferences, soundGateAllows } from './alerts'

describe('parseAlertPreferences', () => {
  test('keeps valid values and falls back per field', () => {
    expect(parseAlertPreferences(null)).toEqual(DEFAULT_ALERT_PREFERENCES)
    expect(
      parseAlertPreferences({
        completionSound: 'bell',
        awaitingInputSound: 'trumpet',
        customCompletionSound: '/x.wav',
        focusMode: 'unfocused',
        notifyOnComplete: false,
        notifyOnWaitingForInput: 'yes',
      }),
    ).toEqual({
      ...DEFAULT_ALERT_PREFERENCES,
      completionSound: 'bell',
      customCompletionSound: '/x.wav',
      focusMode: 'unfocused',
      notifyOnComplete: false,
    })
  })
})

describe('soundGateAllows', () => {
  test('follows the focus mode', () => {
    expect(soundGateAllows('always', false)).toBe(true)
    expect(soundGateAllows('focused', true)).toBe(true)
    expect(soundGateAllows('focused', false)).toBe(false)
    expect(soundGateAllows('unfocused', false)).toBe(true)
    expect(soundGateAllows('unfocused', true)).toBe(false)
  })
})
