import { describe, expect, test } from 'vitest'
import { recentWorkspaces } from './use-new-session'
import type { SessionSummary } from './sessions'

const summary = (overrides: Partial<SessionSummary>): SessionSummary => ({
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
})

describe('recentWorkspaces', () => {
  test('deduplicates by path and orders by most recent use', () => {
    const recent = recentWorkspaces([
      summary({ cwd: '/w/alpha', updatedAt: 10 }),
      summary({ cwd: '/w/beta/sub', repoRoot: '/w/beta', updatedAt: 30 }),
      summary({ cwd: '/w/alpha', updatedAt: 50 }),
      summary({ cwd: null }),
    ])
    expect(recent.map((w) => [w.label, w.lastUsedAt])).toEqual([
      ['alpha', 50],
      ['beta', 30],
    ])
  })

  test('offers the main checkout, never a worktree’s folder', () => {
    const recent = recentWorkspaces([
      summary({
        cwd: '/wt/ab12cd34/app',
        repoRoot: '/w/app',
        worktree: {
          path: '/wt/ab12cd34/app',
          branch: 'droid/x',
          lifecycle: 'persistent',
          repoRoot: '/w/app',
        },
        updatedAt: 40,
      }),
    ])
    expect(recent.map((w) => w.path)).toEqual(['/w/app'])
  })

  test('leaves Scratch Workspaces out: each is a conversation, not a place to go back to', () => {
    const recent = recentWorkspaces([
      summary({
        cwd: '/u/.droi/chats/2026-09-26-aaaaaa',
        tags: [{ name: 'droi.scratch' }],
        updatedAt: 90,
      }),
      summary({ cwd: '/w/alpha', updatedAt: 10 }),
    ])
    expect(recent.map((w) => w.label)).toEqual(['alpha'])
  })

  test('leaves out a Scratch folder whose Session lost its tag', () => {
    const recent = recentWorkspaces([
      summary({ cwd: '/u/.droi/chats/2026-10-02-5c4802', updatedAt: 90 }),
      summary({ cwd: '/w/alpha', updatedAt: 10 }),
    ])
    expect(recent.map((w) => w.label)).toEqual(['alpha'])
  })
})
