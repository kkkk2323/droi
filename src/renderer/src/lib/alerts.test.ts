import { describe, expect, test } from 'vitest'
import {
  AlertTracker,
  DEFAULT_ALERT_PREFERENCES,
  parseAlertPreferences,
  soundGateAllows,
} from './alerts'

describe('AlertTracker', () => {
  test('a turn that ends is one completion; idle without work is nothing', () => {
    const tracker = new AlertTracker()
    expect(tracker.update('a', 'idle')).toBeNull()
    expect(tracker.update('a', 'thinking')).toBeNull()
    expect(tracker.update('a', 'streaming_assistant_message')).toBeNull()
    expect(tracker.update('a', 'idle')).toBe('completion')
    expect(tracker.update('a', 'idle')).toBeNull()
  })

  test('waiting for an answer alerts once per wait, then the turn still completes', () => {
    const tracker = new AlertTracker()
    expect(tracker.update('a', 'executing_tool')).toBeNull()
    expect(tracker.update('a', 'waiting_for_tool_confirmation')).toBe('awaiting-input')
    expect(tracker.update('a', 'waiting_for_tool_confirmation')).toBeNull()
    expect(tracker.update('a', 'executing_tool')).toBeNull()
    expect(tracker.update('a', 'idle')).toBe('completion')
    // A new turn that waits again is a new wait.
    expect(tracker.update('a', 'waiting_for_tool_confirmation')).toBe('awaiting-input')
  })

  test('Sessions are tracked apart, and a forgotten one does not complete', () => {
    const tracker = new AlertTracker()
    tracker.update('a', 'thinking')
    tracker.update('b', 'thinking')
    tracker.forget('a')
    expect(tracker.update('a', 'idle')).toBeNull()
    expect(tracker.update('b', 'idle')).toBe('completion')
  })
})

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
