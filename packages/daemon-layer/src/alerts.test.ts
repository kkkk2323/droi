import { describe, expect, test } from 'vitest'
import { AlertTracker } from './alerts'

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
