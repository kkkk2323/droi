import { describe, expect, test } from 'vitest'
import type { SessionSummary } from './sessions'
import { agentFor, openSession, recentWorkspaces } from './use-new-session'

const summary = (overrides: Partial<SessionSummary>): SessionSummary => ({
  sessionId: Math.random().toString(36).slice(2),
  title: 't',
  cwd: null,
  repoRoot: null,
  worktree: null,
  updatedAt: 0,
  activityAt: 0,
  viewedAt: 0,
  kind: 'chat',
  messagesCount: null,
  archivedAt: null,
  tags: [],
  parentId: null,
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

  test('leaves Scratch Workspaces out: each is a conversation, not a place to go back to', () => {
    const recent = recentWorkspaces([
      summary({
        cwd: '/u/.droi/chats/2026-09-26-aaaaaa',
        tags: [{ name: 'droi.scratch' }],
        updatedAt: 90,
      }),
      summary({ cwd: '/u/.droi/chats/2026-10-02-5c4802', updatedAt: 80 }),
      summary({ cwd: '/w/alpha', updatedAt: 10 }),
    ])
    expect(recent.map((w) => w.label)).toEqual(['alpha'])
  })
})

const models = [
  { provider: 'factory', id: 'claude-x', thinking: ['off', 'high'] },
  { provider: 'openai', id: 'gpt-y' },
]

describe('agentFor', () => {
  test('names the model with its provider, and a thinking level it takes', () => {
    expect(
      agentFor(models, { modelId: 'claude-x', reasoningEffort: 'high', autonomyLevel: null }),
    ).toEqual({ model: { provider: 'factory', modelId: 'claude-x' }, thinkingLevel: 'high' })
    expect(
      agentFor(models, { modelId: 'claude-x', reasoningEffort: 'max', autonomyLevel: null }),
    ).toEqual({ model: { provider: 'factory', modelId: 'claude-x' } })
  })

  test('leaves a model the Kernel does not offer to its default; Direct takes the scripts away', () => {
    expect(agentFor(models, { modelId: 'nope', reasoningEffort: null, autonomyLevel: null })).toBe(
      null,
    )
    expect(
      agentFor(models, {
        modelId: null,
        reasoningEffort: null,
        autonomyLevel: null,
        toolExecutionMode: 'direct_only',
      }),
    ).toEqual({ extensions: { remove: ['codemode'] } })
  })
})

describe('openSession', () => {
  const connection = (isGitRepository: boolean) => {
    const calls: Array<[string, unknown]> = []
    return {
      calls,
      call: async <T>(method: string, params?: Record<string, unknown>): Promise<T> => {
        calls.push([method, params])
        return { conversationId: 42 } as T
      },
      workspaces: {
        inspect: async (path: string) => {
          if (path === '/missing') throw new Error('/missing is not a usable directory.')
          return { path: `${path}/`, isGitRepository, branches: ['main'], currentBranch: 'main' }
        },
        changes: async () => {
          throw new Error('unused')
        },
        addWorktree: async (path: string, base: string, prompt: string) => {
          calls.push(['worktree', { path, base, prompt }])
          return { path: '/wt/fix', branch: 'droi/fix', repoRoot: path }
        },
      },
    }
  }

  test('creates the conversation in the folder as the computer resolves it', async () => {
    const c = connection(false)
    const opened = await openSession(c, '/w/app', {
      settings: { modelId: null, reasoningEffort: null, autonomyLevel: 'medium' },
      defaultAutonomy: 'medium',
    })
    expect(opened).toEqual({ sessionId: '42', worktree: null })
    expect(c.calls).toEqual([
      ['conversation.create', { cwd: '/w/app/', kind: 'chat', requestId: expect.any(String) }],
    ])
  })

  test('makes the worktree first, and sets a level other than the default as the conversation’s own', async () => {
    const c = connection(true)
    const opened = await openSession(c, '/w/app', {
      settings: { modelId: null, reasoningEffort: null, autonomyLevel: 'high' },
      worktree: { lifecycle: 'persistent', promptSlug: ' fix the bug ', baseBranch: 'dev' },
      defaultAutonomy: 'low',
    })
    expect(opened).toEqual({
      sessionId: '42',
      worktree: {
        path: '/wt/fix',
        branch: 'droi/fix',
        lifecycle: 'persistent',
        repoRoot: '/w/app/',
      },
    })
    expect(c.calls.map(([method]) => method)).toEqual([
      'worktree',
      'conversation.create',
      'policy.set',
    ])
    expect(c.calls[0]![1]).toEqual({ path: '/w/app/', base: 'dev', prompt: 'fix the bug' })
    expect(c.calls[2]![1]).toEqual({ id: 42, policy: { autonomy: 'all' } })
  })

  test('a Scratch Session is a scratch conversation, never in a worktree', async () => {
    const c = connection(true)
    await openSession(c, '/u/.droi/chats/x', {
      tags: [{ name: 'droi.scratch' }],
      worktree: { lifecycle: 'ephemeral' },
    })
    expect(c.calls).toEqual([
      [
        'conversation.create',
        { cwd: '/u/.droi/chats/x/', kind: 'scratch', requestId: expect.any(String) },
      ],
    ])
  })

  test('an unusable folder is the computer’s reason, and nothing is created', async () => {
    const c = connection(false)
    expect(await openSession(c, '/missing')).toEqual({
      error: '/missing is not a usable directory.',
    })
    expect(c.calls).toEqual([])
  })
})
