import { describe, expect, test } from 'vitest'
import {
  CONTINUES_TAG,
  RECENT_WINDOW_MS,
  SCRATCH_TAG,
  continuationChain,
  continuationParent,
  continuationTags,
  foldContinued,
  DEFAULT_SORT,
  NO_PINS,
  PINNED_SESSIONS_GROUP_KEY,
  groupByWorkspace,
  moveWorkspace,
  noteFirstSeen,
  type SortOrder,
  visibleSessions,
  withAnnouncedSubagents,
  workspaceLabel,
  type AnnouncedSubagent,
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

  test('workspaces with more conversations come first, empty ones last', () => {
    const groups = groupByWorkspace([
      summary({ cwd: '/w/busy', updatedAt: 10, messagesCount: 4 }),
      summary({ cwd: '/w/busy', updatedAt: 11, messagesCount: 9 }),
      summary({ cwd: '/w/once', updatedAt: 50, messagesCount: 800 }),
      summary({ cwd: '/w/empty', updatedAt: 90, messagesCount: 0 }),
      summary({ cwd: '/w/empty', updatedAt: 91, messagesCount: 0 }),
    ])
    expect(groups.map((g) => g.label)).toEqual(['busy', 'once', 'empty'])
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

  test('pinned apart, Sessions leave their groups for one of their own, first', () => {
    const groups = groupByWorkspace(
      [
        summary({ sessionId: 'a', cwd: '/w/alpha', updatedAt: 10 }),
        summary({ sessionId: 'b', cwd: '/w/beta', updatedAt: 30 }),
        summary({ sessionId: 'c', cwd: '/w/alpha', updatedAt: 20 }),
        summary({ sessionId: 'g', cwd: '/w/gamma', updatedAt: 40 }),
      ],
      { workspaces: new Set(['/w/alpha']), sessions: new Set(['a', 'b']) },
      DEFAULT_SORT,
      { pinnedApart: true },
    )
    expect(groups.map((g) => [g.key, g.sessions.map((s) => s.sessionId)])).toEqual([
      [PINNED_SESSIONS_GROUP_KEY, ['b', 'a']],
      ['/w/alpha', ['c']],
      ['/w/gamma', ['g']],
    ])
  })
})

describe('sorting the groups', () => {
  const sessions = [
    summary({ sessionId: 'b1', cwd: '/w/beta', updatedAt: 100 }),
    summary({ sessionId: 'b2', cwd: '/w/beta', updatedAt: 200 }),
    summary({ sessionId: 'a1', cwd: '/w/alpha', updatedAt: 900 }),
    summary({ sessionId: 'c1', cwd: '/w/gamma', updatedAt: 500 }),
  ]
  const keys = (order: Partial<SortOrder>) =>
    groupByWorkspace(sessions, NO_PINS, { ...DEFAULT_SORT, ...order }).map((g) => g.label)

  test('by most sessions, recently active, name, or by hand', () => {
    expect(keys({})).toEqual(['beta', 'alpha', 'gamma'])
    expect(keys({ workspaces: 'recent' })).toEqual(['alpha', 'gamma', 'beta'])
    expect(keys({ workspaces: 'name' })).toEqual(['alpha', 'beta', 'gamma'])
    // A Workspace not placed yet follows the placed ones, in the default order.
    expect(keys({ workspaces: 'manual', manual: ['/w/gamma'] })).toEqual(['gamma', 'beta', 'alpha'])
  })

  test('pinned Workspaces stay first whatever the order', () => {
    const pinned = groupByWorkspace(
      sessions,
      { workspaces: new Set(['/w/gamma']), sessions: new Set() },
      { ...DEFAULT_SORT, workspaces: 'name' },
    )
    expect(pinned.map((g) => g.label)).toEqual(['gamma', 'alpha', 'beta'])
  })

  test('Sessions newest first, or by when they were first seen', () => {
    const beta = (order: Partial<SortOrder>) =>
      groupByWorkspace(sessions, NO_PINS, { ...DEFAULT_SORT, ...order })
        .find((g) => g.label === 'beta')!
        .sessions.map((s) => s.sessionId)
    expect(beta({})).toEqual(['b2', 'b1'])
    expect(beta({ sessions: 'created', firstSeen: { b1: 5_000, b2: 1_000 } })).toEqual(['b1', 'b2'])
  })

  test('a move places one Workspace before or after another, from the order shown', () => {
    const shown = ['/w/a', '/w/b', '/w/c']
    expect(moveWorkspace(shown, '/w/c', '/w/a', 'before')).toEqual(['/w/c', '/w/a', '/w/b'])
    expect(moveWorkspace(shown, '/w/a', '/w/b', 'after')).toEqual(['/w/b', '/w/a', '/w/c'])
    expect(moveWorkspace(shown, '/w/a', '/w/a', 'after')).toEqual(shown)
  })
})

describe('noteFirstSeen', () => {
  test('adds only new Sessions, at the earlier of now and their last change', () => {
    const known = { old: 1 }
    expect(noteFirstSeen(known, [summary({ sessionId: 'old' })], 10_000)).toBe(known)
    expect(
      noteFirstSeen(
        known,
        [
          summary({ sessionId: 'fresh', updatedAt: 50 }),
          summary({ sessionId: 'later', updatedAt: 99 }),
        ],
        60_000,
      ),
    ).toEqual({ old: 1, fresh: 50_000, later: 60_000 })
  })
})

describe('groupByWorkspace with Scratch Workspaces', () => {
  const scratch = [{ name: SCRATCH_TAG }]

  test('every Scratch Session sits in one Recents group, last, whatever its folder', () => {
    const groups = groupByWorkspace([
      summary({
        sessionId: 's1',
        cwd: '/u/.droi/chats/2026-09-25-aaaaaa',
        tags: scratch,
        updatedAt: 50,
        messagesCount: 9,
      }),
      summary({ sessionId: 'p', cwd: '/w/alpha', updatedAt: 10 }),
      summary({
        sessionId: 's2',
        cwd: '/u/.droi/chats/2026-09-26-bbbbbb',
        tags: scratch,
        updatedAt: 60,
        messagesCount: 9,
      }),
    ])
    expect(groups.map((g) => [g.label, g.scratch])).toEqual([
      ['alpha', false],
      ['Recents', true],
    ])
    expect(groups[1]!.sessions.map((s) => s.sessionId)).toEqual(['s2', 's1'])
  })

  test('a Session in a Scratch folder without the tag still goes to Recents', () => {
    const groups = groupByWorkspace([
      summary({ sessionId: 'lost', cwd: '/u/.droi/chats/2026-10-02-5c4802', updatedAt: 70 }),
      summary({ sessionId: 'p', cwd: '/w/alpha', updatedAt: 10 }),
      summary({ sessionId: 'dated', cwd: '/w/2026-10-02-notes', updatedAt: 5 }),
    ])
    expect(groups.map((g) => [g.label, g.sessions.map((s) => s.sessionId)])).toEqual([
      ['alpha', ['p']],
      ['2026-10-02-notes', ['dated']],
      ['Recents', ['lost']],
    ])
  })

  test('Recents stays last even when pinned; its pinned Sessions, apart, go to the pinned group', () => {
    const groups = groupByWorkspace(
      [
        summary({ sessionId: 's1', cwd: '/c/a', tags: scratch, updatedAt: 50 }),
        summary({ sessionId: 's2', cwd: '/c/b', tags: scratch, updatedAt: 60 }),
        summary({ sessionId: 'p', cwd: '/w/alpha', updatedAt: 10 }),
      ],
      {
        workspaces: new Set([groupByWorkspace([summary({ tags: scratch })])[0]!.key]),
        sessions: new Set(['s1']),
      },
      DEFAULT_SORT,
      { pinnedApart: true },
    )
    expect(groups.map((g) => [g.label, g.sessions.map((s) => s.sessionId)])).toEqual([
      ['Pinned sessions', ['s1']],
      ['alpha', ['p']],
      ['Recents', ['s2']],
    ])
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

  test('a Session that names itself as its parent stays listed', () => {
    // Droi 1.10 and earlier tagged a Session compacted in place with itself.
    const self = summary({ sessionId: 'same', parentId: 'same' })
    expect(foldContinued([self, summary({ sessionId: 'other' })]).map((s) => s.sessionId)).toEqual([
      'same',
      'other',
    ])
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

describe('withAnnouncedSubagents', () => {
  const announced = (overrides: Partial<AnnouncedSubagent>): AnnouncedSubagent => ({
    sessionId: 'child',
    callingSessionId: 'main',
    callingToolUseId: 'toolu_1',
    subagentType: 'explorer',
    description: 'Map the app',
    cwd: null,
    announcedAt: 50,
    ...overrides,
  })

  test('adds a row for a subagent the list lacks, in its caller’s Workspace', () => {
    const main = summary({ sessionId: 'main', cwd: '/w/app/sub', repoRoot: '/w/app' })
    const [row, ...rest] = withAnnouncedSubagents([main], [announced({})])
    expect(rest).toEqual([main])
    expect(row).toMatchObject({
      sessionId: 'child',
      title: 'Explorer: Map the app',
      cwd: '/w/app/sub',
      repoRoot: '/w/app',
      updatedAt: 50,
      callingSessionId: 'main',
      callingToolUseId: 'toolu_1',
    })
  })

  test('the listed row wins once the Daemon lists the subagent', () => {
    const listed = [summary({ sessionId: 'main' }), summary({ sessionId: 'child', title: 'x' })]
    expect(withAnnouncedSubagents(listed, [announced({})])).toBe(listed)
  })

  test('without a type or description the row is still named', () => {
    const [row] = withAnnouncedSubagents([], [announced({ subagentType: '', description: '' })])
    expect(row?.title).toBe('Subagent')
  })
})
