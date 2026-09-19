import { describe, expect, test } from 'vitest'
import { groupByWorkspace, workspaceLabel, type SessionSummary } from './sessions'

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
