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

describe('archiving a conversation', () => {
  const recorder = () => {
    const calls: string[] = []
    return {
      calls,
      connection: {
        controller: {
          archiveSession: async (id: string) => {
            calls.push(`archive ${id}`)
            return { success: true, archivedAt: 'now' }
          },
          unarchiveSession: async (id: string) => {
            calls.push(`unarchive ${id}`)
            return { success: true }
          },
        },
        scratch: {
          trash: async (path: string) => void calls.push(`trash ${path}`),
          restore: async (path: string) => void calls.push(`restore ${path}`),
        },
      },
    }
  }
  const scratch = [{ name: SCRATCH_TAG }]
  const folder = '/u/.droi/chats/2026-09-26-abcdef'
  const first = summary({ sessionId: 'first', cwd: folder, tags: scratch })
  const second = summary({ sessionId: 'second', cwd: folder, tags: scratch, parentId: 'first' })
  const latest = summary({ sessionId: 'latest', cwd: folder, tags: scratch, parentId: 'second' })

  test('a Workspace Session is archived on its own, and its folder is left alone', async () => {
    const { calls, connection } = recorder()
    const session = summary({ sessionId: 'p', cwd: '/w/app' })
    await archiveConversation(connection, [session], session)
    await unarchiveConversation(connection, [session], session)
    expect(calls).toEqual(['archive p', 'unarchive p'])
  })

  test('a Scratch Session takes its whole compaction chain along, then its folder goes to the Trash', async () => {
    const { calls, connection } = recorder()
    await archiveConversation(connection, [first, second, latest], latest)
    expect(calls).toEqual(['archive latest', 'archive second', 'archive first', `trash ${folder}`])
  })

  test('unarchiving recreates the folder before the Sessions come back', async () => {
    const { calls, connection } = recorder()
    await unarchiveConversation(connection, [first, second, latest], latest)
    expect(calls).toEqual([
      `restore ${folder}`,
      'unarchive latest',
      'unarchive second',
      'unarchive first',
    ])
  })

  test('a refused archive keeps the folder', async () => {
    const { calls, connection } = recorder()
    connection.controller.archiveSession = async () => {
      throw new Error('CONFLICT')
    }
    await expect(archiveConversation(connection, [latest], latest)).rejects.toThrow('CONFLICT')
    expect(calls).toEqual([])
  })
})
