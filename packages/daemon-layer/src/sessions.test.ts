import { describe, expect, test } from 'vitest'
import {
  CONTINUES_TAG,
  EARLIER_GROUP_KEY,
  NOW_GROUP_KEY,
  RANK_BUSY,
  RANK_REST,
  RANK_UNREAD,
  RECENT_WINDOW_MS,
  SCRATCH_TAG,
  TODAY_GROUP_KEY,
  YESTERDAY_GROUP_KEY,
  groupByTime,
  sortKey,
  usedAt,
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
  splitStale,
  STALE_WORKSPACE_MS,
  withoutAutomationRuns,
  AUTOMATION_TAG,
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
    worktree: null,
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

  test('a worktree Session groups under its main checkout, not its own folder', () => {
    const tree = {
      path: '/wt/ab12cd34/app',
      branch: 'droid/fix-it',
      lifecycle: 'ephemeral' as const,
    }
    const groups = groupByWorkspace([
      summary({ sessionId: 'a', cwd: '/w/app', updatedAt: 10 }),
      summary({
        sessionId: 'b',
        cwd: tree.path,
        repoRoot: '/w/app',
        worktree: { ...tree, repoRoot: '/w/app' },
        updatedAt: 20,
      }),
      summary({
        sessionId: 'c',
        cwd: tree.path,
        worktree: { ...tree, repoRoot: '/w/app' },
        updatedAt: 30,
      }),
    ])
    expect(groups.map((g) => [g.path, g.sessions.map((s) => s.sessionId)])).toEqual([
      ['/w/app', ['c', 'b', 'a']],
    ])
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

describe('splitStale', () => {
  test('keeps the order and parts out the Workspaces unused for a month', () => {
    const now = STALE_WORKSPACE_MS * 2
    const fresh = (now - 1000) / 1000
    const old = (now - STALE_WORKSPACE_MS - 1000) / 1000
    const groups = groupByWorkspace([
      summary({ cwd: '/w/a', updatedAt: old }),
      summary({ cwd: '/w/b', updatedAt: fresh }),
      summary({ cwd: '/w/c', updatedAt: old }),
      summary({ cwd: '/w/c', updatedAt: fresh - 1 }),
    ])
    const { active, stale } = splitStale(groups, now)
    expect(active.map((g) => g.key)).toEqual(['/w/c', '/w/b'])
    expect(stale.map((g) => g.key)).toEqual(['/w/a'])
  })
})

test('withoutAutomationRuns leaves out runs only', () => {
  const run = summary({
    sessionId: 'run',
    tags: [{ name: AUTOMATION_TAG, metadata: { type: 'run' } }],
  })
  const setup = summary({
    sessionId: 'setup',
    tags: [{ name: AUTOMATION_TAG, metadata: { type: 'create' } }],
  })
  const plain = summary({ sessionId: 'plain' })
  expect(withoutAutomationRuns([run, setup, plain]).map((s) => s.sessionId)).toEqual([
    'setup',
    'plain',
  ])
})

describe('Session Times in the order', () => {
  const day = 24 * 60 * 60 * 1000
  // Noon, local time: the time view's days are the phone's.
  const now = new Date(2026, 5, 15, 12, 0)
  const at = (offsetMs: number) => now.getTime() + offsetMs

  test('a Session is dated by its last message sent, else by the Daemon', () => {
    const times = new Map([['a', { sentAt: 5_000 }]])
    expect(usedAt(summary({ sessionId: 'a', updatedAt: 9 }), times)).toBe(5_000)
    expect(usedAt(summary({ sessionId: 'b', updatedAt: 9 }), times)).toBe(9_000)
    expect(usedAt(summary({ sessionId: 'b', updatedAt: 9 }))).toBe(9_000)
  })

  test('working Sessions rank first, then unread; the Created order has no ranks', () => {
    const order: SortOrder = {
      ...DEFAULT_SORT,
      busy: new Set(['w']),
      unread: new Set(['w', 'u']),
    }
    expect(sortKey(summary({ sessionId: 'w', updatedAt: 1 }), order)).toEqual({
      rank: RANK_BUSY,
      at: 1_000,
    })
    expect(sortKey(summary({ sessionId: 'u' }), order).rank).toBe(RANK_UNREAD)
    expect(sortKey(summary({ sessionId: 'r' }), order).rank).toBe(RANK_REST)
    expect(sortKey(summary({ sessionId: 'w' }), { ...order, sessions: 'created' }).rank).toBe(
      RANK_REST,
    )
  })

  test('inside a Workspace: working, unread, then by the last message sent', () => {
    const groups = groupByWorkspace(
      [
        summary({ sessionId: 'hooked', cwd: '/w/a', updatedAt: 900 }),
        summary({ sessionId: 'sent', cwd: '/w/a', updatedAt: 100 }),
        summary({ sessionId: 'unread', cwd: '/w/a', updatedAt: 50 }),
        summary({ sessionId: 'working', cwd: '/w/a', updatedAt: 10 }),
      ],
      NO_PINS,
      {
        ...DEFAULT_SORT,
        times: new Map([
          ['sent', { sentAt: 950_000 }],
          ['hooked', { sentAt: 200_000 }],
        ]),
        busy: new Set(['working']),
        unread: new Set(['unread']),
      },
    )
    expect(groups[0]!.sessions.map((s) => s.sessionId)).toEqual([
      'working',
      'unread',
      'sent',
      'hooked',
    ])
  })

  test('Recently active ranks the Workspaces by their last message sent', () => {
    const labels = groupByWorkspace(
      [
        summary({ sessionId: 'a', cwd: '/w/alpha', updatedAt: 900 }),
        summary({ sessionId: 'b', cwd: '/w/beta', updatedAt: 100 }),
      ],
      NO_PINS,
      { ...DEFAULT_SORT, workspaces: 'recent', times: new Map([['b', { sentAt: 950_000 }]]) },
    ).map((g) => g.label)
    expect(labels).toEqual(['beta', 'alpha'])
  })

  test('the time view: pinned, right now, then by day, each naming its Workspace', () => {
    const scratch = [{ name: SCRATCH_TAG }]
    const sessions = [
      summary({ sessionId: 'early', cwd: '/w/alpha', updatedAt: at(-3 * day) / 1000 }),
      summary({ sessionId: 'yday', cwd: '/w/beta', updatedAt: at(-day) / 1000 }),
      summary({ sessionId: 'sent', cwd: '/w/alpha', updatedAt: at(-day) / 1000 }),
      summary({ sessionId: 'note', cwd: '/c/x', tags: scratch, updatedAt: at(-2e6) / 1000 }),
      summary({ sessionId: 'unread', cwd: '/w/beta', updatedAt: at(-5 * day) / 1000 }),
      summary({ sessionId: 'working', cwd: '/w/alpha', updatedAt: at(-9 * day) / 1000 }),
      summary({ sessionId: 'pinned', cwd: '/w/beta', updatedAt: at(-9 * day) / 1000 }),
    ]
    const groups = groupByTime(
      sessions,
      { workspaces: new Set(), sessions: new Set(['pinned']) },
      {
        ...DEFAULT_SORT,
        times: new Map([['sent', { sentAt: at(-1e6) }]]),
        busy: new Set(['working', 'pinned']),
        unread: new Set(['unread']),
      },
      now,
    )
    expect(groups.map((g) => [g.key, g.sessions.map((s) => s.sessionId)])).toEqual([
      [PINNED_SESSIONS_GROUP_KEY, ['pinned']],
      [NOW_GROUP_KEY, ['working', 'unread']],
      [TODAY_GROUP_KEY, ['sent', 'note']],
      [YESTERDAY_GROUP_KEY, ['yday']],
      [EARLIER_GROUP_KEY, ['early']],
    ])
    expect(groups[0]!.places.get('sent')).toBe('alpha')
    expect(groups[0]!.places.has('note')).toBe(false)
  })

  test('the time view in the Created order goes by when each Session was first seen', () => {
    const groups = groupByTime(
      [
        summary({ sessionId: 'old', updatedAt: at(0) / 1000 }),
        summary({ sessionId: 'new', updatedAt: at(-3 * day) / 1000 }),
      ],
      NO_PINS,
      {
        ...DEFAULT_SORT,
        sessions: 'created',
        firstSeen: { old: at(-3 * day), new: at(-1000) },
        busy: new Set(['old']),
      },
      now,
    )
    expect(groups.map((g) => [g.key, g.sessions.map((s) => s.sessionId)])).toEqual([
      [TODAY_GROUP_KEY, ['new']],
      [EARLIER_GROUP_KEY, ['old']],
    ])
  })

  test('an old Session working or unread is neither hidden nor stale', () => {
    const old = (now.getTime() - STALE_WORKSPACE_MS - day) / 1000
    const order: SortOrder = { ...DEFAULT_SORT, busy: new Set(['w']), unread: new Set(['u']) }
    const list = [
      summary({ sessionId: 'w', cwd: '/w/a', updatedAt: old }),
      summary({ sessionId: 'u', cwd: '/w/b', updatedAt: old }),
      summary({ sessionId: 'r', cwd: '/w/c', updatedAt: old }),
    ]
    expect(visibleSessions(list, 0, now.getTime(), NO_PINS.sessions, order)).toEqual({
      visible: [list[0], list[1]],
      hidden: 1,
    })
    const { active, stale } = splitStale(groupByWorkspace(list), now.getTime(), order)
    expect(active.map((g) => g.key).sort()).toEqual(['/w/a', '/w/b'])
    expect(stale.map((g) => g.key)).toEqual(['/w/c'])
  })
})
