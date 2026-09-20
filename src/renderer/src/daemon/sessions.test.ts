import { describe, expect, test } from 'vitest'
import {
  RECENT_WINDOW_MS,
  groupByWorkspace,
  visibleSessions,
  workspaceLabel,
  type SessionSummary,
} from './sessions'

function summary(overrides: Partial<SessionSummary>): SessionSummary {
  return {
    sessionId: Math.random().toString(36).slice(2),
    title: 't',
    cwd: null,
    repoRoot: null,
    updatedAt: 0,
    messagesCount: null,
    archivedAt: null,
    ...overrides,
  }
}

describe('groupByWorkspace', () => {
  test('groups by repoRoot, falling back to cwd, newest first everywhere', () => {
    const groups = groupByWorkspace([
      summary({ sessionId: 'a', cwd: '/w/alpha', updatedAt: 10 }),
      summary({ sessionId: 'b', cwd: '/w/beta/sub', repoRoot: '/w/beta', updatedAt: 30 }),
      summary({ sessionId: 'c', cwd: '/w/alpha', updatedAt: 20 }),
      summary({ sessionId: 'd', cwd: '/w/beta', updatedAt: 5 }),
    ])
    expect(groups.map((g) => g.label)).toEqual(['beta', 'alpha'])
    expect(groups[0]!.sessions.map((s) => s.sessionId)).toEqual(['b', 'd'])
    expect(groups[1]!.sessions.map((s) => s.sessionId)).toEqual(['c', 'a'])
  })

  test('sessions without any path land in an unknown group', () => {
    const groups = groupByWorkspace([summary({ sessionId: 'x' })])
    expect(groups[0]!.label).toBe('Unknown workspace')
  })
})

describe('workspaceLabel', () => {
  test('uses the last path segment', () => {
    expect(workspaceLabel('/Users/me/dev/droi')).toBe('droi')
    expect(workspaceLabel('/Users/me/dev/droi/')).toBe('droi')
    expect(workspaceLabel('C:\\code\\thing')).toBe('thing')
  })
})

describe('visibleSessions', () => {
  const now = 1_800_000_000_000
  const day = 24 * 60 * 60
  const list = [
    summary({ sessionId: 'today', updatedAt: now / 1000 - day }),
    summary({ sessionId: 'lastWeek', updatedAt: now / 1000 - 7 * day }),
    summary({ sessionId: 'lastMonth', updatedAt: now / 1000 - 30 * day }),
  ]

  test('shows recent sessions and counts the rest as hidden', () => {
    expect(visibleSessions(list, 0, now)).toEqual({ visible: [list[0]], hidden: 2 })
    expect(RECENT_WINDOW_MS).toBe(3 * day * 1000)
  })

  test('reveals older sessions newest first, up to the requested number', () => {
    expect(visibleSessions(list, 1, now)).toEqual({ visible: [list[0], list[1]], hidden: 1 })
    expect(visibleSessions(list, 30, now).hidden).toBe(0)
  })
})
