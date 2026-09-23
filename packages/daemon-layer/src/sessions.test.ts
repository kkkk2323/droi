import { describe, expect, test } from 'vitest'
import {
  CONTINUES_TAG,
  RECENT_WINDOW_MS,
  continuationChain,
  continuationParent,
  continuationTags,
  foldContinued,
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
    tags: [],
    parentId: null,
    callingSessionId: null,
    callingToolUseId: null,
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

  test('pinned workspaces and sessions come first, otherwise newest first', () => {
    const groups = groupByWorkspace(
      [
        summary({ sessionId: 'a', cwd: '/w/alpha', updatedAt: 10 }),
        summary({ sessionId: 'b', cwd: '/w/beta', updatedAt: 30 }),
        summary({ sessionId: 'c', cwd: '/w/alpha', updatedAt: 20 }),
        summary({ sessionId: 'g', cwd: '/w/gamma', updatedAt: 40 }),
      ],
      { workspaces: new Set(['/w/alpha']), sessions: new Set(['a']) },
    )
    expect(groups.map((g) => g.label)).toEqual(['alpha', 'gamma', 'beta'])
    expect(groups[0]!.sessions.map((s) => s.sessionId)).toEqual(['a', 'c'])
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

  test('a pinned session always shows and is not counted as hidden', () => {
    expect(visibleSessions(list, 0, now, new Set(['lastMonth']))).toEqual({
      visible: [list[0], list[2]],
      hidden: 1,
    })
  })
})

describe('continuation chain', () => {
  test('a child folds its parent out of the list and reads the link from its tag', () => {
    const parent = summary({ sessionId: 'old' })
    const tags = continuationTags('old', [{ name: 'team', metadata: { id: '1' } }])
    const child = summary({ sessionId: 'new', tags, parentId: continuationParent(tags) })
    expect(child.parentId).toBe('old')
    expect(tags.map((t) => t.name)).toEqual(['team', CONTINUES_TAG])
    expect(
      foldContinued([child, parent, summary({ sessionId: 'other' })]).map((s) => s.sessionId),
    ).toEqual(['new', 'other'])
  })

  test('continuationTags replaces an older link instead of stacking them', () => {
    const tags = continuationTags('b', continuationTags('a', []))
    expect(tags).toEqual([{ name: CONTINUES_TAG, metadata: { parent: 'b' } }])
  })
})

describe('continuationChain', () => {
  test('walks back through every compaction, nearest first', () => {
    const first = summary({ sessionId: 'a' })
    const second = summary({ sessionId: 'b', parentId: 'a' })
    const third = summary({ sessionId: 'c', parentId: 'b' })
    expect(continuationChain([third, first, second], third)).toEqual([second, first])
  })

  test('stops where the list no longer has the parent, and at a loop', () => {
    const orphan = summary({ sessionId: 'b', parentId: 'gone' })
    expect(continuationChain([orphan], orphan)).toEqual([])
    const x = summary({ sessionId: 'x', parentId: 'y' })
    const y = summary({ sessionId: 'y', parentId: 'x' })
    expect(continuationChain([x, y], x)).toEqual([y])
  })
})
