import { describe, expect, test } from 'vitest'
import { archiveConversation, unarchiveConversation } from './archive'
import { createScratchWorkspaces } from './scratch-workspaces'
import { SCRATCH_TAG, type SessionSummary } from './sessions'

describe('createScratchWorkspaces', () => {
  const calls: Array<{ url: string; method: string | undefined }> = []
  const answer = (status: number, body?: unknown) =>
    new Response(body === undefined ? null : JSON.stringify(body), { status })
  const scratchWith = (reply: () => Response) =>
    createScratchWorkspaces(
      { gatewayUrl: 'http://laptop.local:41417', pairingToken: 'tok' },
      async (input, init) => {
        calls.push({ url: String(input), method: init?.method })
        return reply()
      },
    )

  test('asks the Gateway for a folder and answers its path', async () => {
    calls.length = 0
    const scratch = scratchWith(() => answer(201, { path: '/u/.droi/chats/2026-09-26-abcdef' }))
    expect(await scratch.create()).toBe('/u/.droi/chats/2026-09-26-abcdef')
    expect(calls).toEqual([
      { url: 'http://laptop.local:41417/scratch-workspaces?token=tok', method: 'POST' },
    ])
  })

  test('names the folder to trash or restore in the query', async () => {
    calls.length = 0
    const scratch = scratchWith(() => answer(204))
    await scratch.trash('/u/.droi/chats/2026-09-26-abcdef')
    await scratch.restore('/u/.droi/chats/2026-09-26-abcdef')
    expect(
      calls.map((c) => [new URL(c.url).pathname, new URL(c.url).searchParams.get('path')]),
    ).toEqual([
      ['/scratch-workspaces/trash', '/u/.droi/chats/2026-09-26-abcdef'],
      ['/scratch-workspaces/restore', '/u/.droi/chats/2026-09-26-abcdef'],
    ])
  })

  test('a refusal rejects with the Gateway’s reason, or says what failed', async () => {
    await expect(
      scratchWith(() => answer(400, { error: '/etc is not a Scratch Workspace' })).trash('/etc'),
    ).rejects.toThrow('/etc is not a Scratch Workspace')
    await expect(scratchWith(() => answer(404)).create()).rejects.toThrow(
      /could not make a folder.*404/i,
    )
  })
})

function summary(overrides: Partial<SessionSummary>): SessionSummary {
  return {
    sessionId: 'x',
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
  }
}

describe('archiving a conversation', () => {
  const recorder = () => {
    const calls: string[] = []
    let refuse = false
    return {
      calls,
      refuse: () => void (refuse = true),
      connection: {
        call: async <T>(method: string, params?: Record<string, unknown>): Promise<T> => {
          if (refuse) throw new Error('CONFLICT')
          calls.push(`${method} ${params?.['id']} ${params?.['archived']}`)
          return {} as T
        },
        scratch: {
          trash: async (path: string) => void calls.push(`trash ${path}`),
          restore: async (path: string) => void calls.push(`restore ${path}`),
        },
      },
    }
  }
  const folder = '/u/.droi/chats/2026-09-26-abcdef'
  const scratch = summary({ sessionId: '7', cwd: folder, tags: [{ name: SCRATCH_TAG }] })

  test('a Workspace Session is archived on its own, and its folder is left alone', async () => {
    const { calls, connection } = recorder()
    const session = summary({ sessionId: '3', cwd: '/w/app' })
    await archiveConversation(connection, session)
    await unarchiveConversation(connection, session)
    expect(calls).toEqual(['index.update 3 true', 'index.update 3 false'])
  })

  test('a Scratch Session’s folder goes to the Trash after it, and comes back before it', async () => {
    const { calls, connection } = recorder()
    await archiveConversation(connection, scratch)
    await unarchiveConversation(connection, scratch)
    expect(calls).toEqual([
      'index.update 7 true',
      `trash ${folder}`,
      `restore ${folder}`,
      'index.update 7 false',
    ])
  })

  test('a refused archive keeps the folder', async () => {
    const { calls, connection, refuse } = recorder()
    refuse()
    await expect(archiveConversation(connection, scratch)).rejects.toThrow('CONFLICT')
    expect(calls).toEqual([])
  })
})
