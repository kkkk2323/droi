import { describe, expect, test } from 'vitest'
import { activityOf, countBusy, type SessionActivity } from './use-session-activity'

describe('countBusy', () => {
  const sessions = ['a', 'b', 'c', 'd'].map((sessionId) => ({ sessionId }))

  test('counts working (compacting and running subagents too) apart from waiting', () => {
    const activity = new Map<string, SessionActivity>([
      ['a', 'working'],
      ['b', 'needs-input'],
      ['c', 'compacting'],
    ])
    expect(countBusy(sessions, activity, new Map([['d', 2]]))).toEqual({
      working: 3,
      needsInput: 1,
    })
  })

  test('is null when nothing is going on', () => {
    expect(countBusy(sessions, new Map(), new Map([['a', 0]]))).toBeNull()
  })
})

describe('activityOf', () => {
  test('tells compacting from working and waiting', () => {
    expect(activityOf('compacting_conversation')).toBe('compacting')
    expect(activityOf('executing_tool')).toBe('working')
    expect(activityOf('waiting_for_tool_confirmation')).toBe('needs-input')
    expect(activityOf('idle')).toBeNull()
  })
})
