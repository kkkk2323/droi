import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { mkdirSync, realpathSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { execFileSync } from 'node:child_process'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { forgetRepoRoots, repoRootOf } from '../src/workspace.ts'

/**
 * What the sidebar reads: which conversations are top-level rows, which repository each belongs to,
 * and when its unread dot goes away.
 */

let data: ReturnType<typeof tempData>
let faux: Faux
let kernel: KernelHandle
let client: Client

beforeEach(async () => {
  data = tempData()
  faux = fauxCatalog()
  kernel = await start({ dir: data.dir, faux })
  client = new Client(kernel.port)
})

afterEach(async () => {
  client.close()
  await kernel.stop()
  data.cleanup()
  forgetRepoRoots()
})

async function create(cwd: string, kind = 'chat'): Promise<number> {
  const answer = await client.call('conversation.create', {
    agent: { model: faux.model, cwd },
    kind,
  })
  return answer['conversationId'] as number
}

/** A directory inside the throwaway data dir with its own `git init`, or none. */
function workspace(name: string, git: boolean): string {
  const dir = join(data.dir, name)
  mkdirSync(dir, { recursive: true })
  if (git) execFileSync('git', ['init', '-q'], { cwd: dir, env: process.env })
  return dir
}

describe('conversation.list', () => {
  test('a child conversation is not a list row, and `parent` lists it', async () => {
    const cwd = workspace('repo', true)
    const parent = await create(cwd)
    const child = await create(cwd, 'subagent')
    // `index.update` is how a child records its owner; a subagent sets it in M4, so the write stands in.
    await client.call('index.update', { id: child, parentId: parent })

    const top = await client.call('conversation.list', {})
    expect((top['items'] as { id: number }[]).map((row) => row.id)).toEqual([parent])

    const children = await client.call('conversation.list', { parent })
    expect((children['items'] as { id: number }[]).map((row) => row.id)).toEqual([child])
    expect((children['items'] as { parentId?: number }[])[0]?.parentId).toBe(parent)

    const none = await client.call('conversation.list', { parent: 999 })
    expect(none['items']).toEqual([])
  })

  test('a top-level row carries no parentId', async () => {
    const cwd = workspace('solo', true)
    await create(cwd)
    const page = await client.call('conversation.list', {})
    const row = (page['items'] as { parentId?: number }[])[0]
    expect(row?.parentId).toBeUndefined()
  })

  test('repoRoot is the repository the cwd sits in, and workspace stays the cwd', async () => {
    const root = workspace('main', true)
    // A subdirectory is a different cwd in the same repository: one group, two workspaces.
    const nested = join(root, 'packages', 'app')
    mkdirSync(nested, { recursive: true })
    writeFileSync(join(nested, 'package.json'), '{}')
    const inside = await create(nested)
    const atRoot = await create(root)

    const page = await client.call('conversation.list', {})
    const rows = page['items'] as { id: number; repoRoot?: string; workspace?: string }[]
    const expected = realpathSync(root)
    for (const row of rows) {
      expect(row.repoRoot).toBe(expected)
    }
    expect(rows.find((row) => row.id === inside)?.workspace).toBe(nested)
    expect(rows.find((row) => row.id === atRoot)?.workspace).toBe(root)

    // Filtering by repository finds both; filtering by workspace finds one.
    const byRepo = await client.call('conversation.list', { repoRoot: expected })
    expect((byRepo['items'] as { id: number }[]).length).toBe(2)
    const byWorkspace = await client.call('conversation.list', { workspace: nested })
    expect((byWorkspace['items'] as { id: number }[]).map((row) => row.id)).toEqual([inside])
  })

  test('a directory outside any repository is its own group', async () => {
    const plain = workspace('plain', false)
    await create(plain)
    const page = await client.call('conversation.list', {})
    const row = (page['items'] as { repoRoot?: string; workspace?: string }[])[0]
    // `git rev-parse` walks up, and the temp dir may itself sit in a repository (a worktree, or a
    // checkout under $HOME). Either answer is the rule: the repository root when there is one, the
    // directory when there is not.
    const expected = repoRootOf(plain)
    expect(row?.repoRoot).toBe(expected)
    expect(row?.workspace).toBe(plain)
  })

  test('a cwd change moves the conversation to the new repository', async () => {
    const first = workspace('first', true)
    const second = workspace('second', true)
    const id = await create(first)
    await client.call('conversation.configure', { id, change: { cwd: second } })
    const page = await client.call('conversation.list', {})
    const row = (page['items'] as { id: number; repoRoot?: string; workspace?: string }[])[0]
    expect(row?.workspace).toBe(second)
    expect(row?.repoRoot).toBe(realpathSync(second))
  })

  test('the repository is stored, so a restart does not lose it', async () => {
    const root = workspace('kept', true)
    const id = await create(root)
    const before = (await client.call('conversation.list', {}))['items'] as {
      id: number
      repoRoot?: string
    }[]
    expect(before[0]?.repoRoot).toBe(realpathSync(root))

    client.close()
    await kernel.stop()
    // Forget the memo, so a stale cache cannot be what answers.
    forgetRepoRoots()
    kernel = await start({ dir: data.dir, faux })
    client = new Client(kernel.port)
    const after = (await client.call('conversation.list', {}))['items'] as {
      id: number
      repoRoot?: string
    }[]
    expect(after.find((row) => row.id === id)?.repoRoot).toBe(realpathSync(root))
  })
})

describe('index.update viewedAt', () => {
  test('a time clears the unread dot, null brings it back, absent leaves it', async () => {
    const cwd = workspace('unread', true)
    const id = await create(cwd)
    const read = async (): Promise<{ viewedAt: number | null; lastActivityAt: number }> => {
      const page = await client.call('conversation.list', {})
      const row = (
        page['items'] as { id: number; viewedAt: number | null; lastActivityAt: number }[]
      ).find((item) => item.id === id)
      if (row === undefined) throw new Error('the conversation is not in the list')
      return row
    }
    // A create writes no "seen" time; opening the conversation does.
    expect((await read()).viewedAt).toBeNull()
    await client.call('conversation.open', { id })
    expect((await read()).viewedAt).not.toBeNull()

    await client.call('index.update', { id, viewedAt: 1 })
    expect((await read()).viewedAt).toBe(1)

    await client.call('index.update', { id, viewedAt: null })
    expect((await read()).viewedAt).toBeNull()

    const cleared = (await read()).viewedAt
    await client.call('index.update', { id, title: 'renamed' })
    expect((await read()).viewedAt).toBe(cleared)

    const failure = await client.fails('index.update', { id, viewedAt: 'yesterday' })
    expect(failure.code).toBe(-32602)
  })
})
