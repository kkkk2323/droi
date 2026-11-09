import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { mkdirSync, realpathSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { execFileSync } from 'node:child_process'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { forgetRepoRoots, repoRootOf } from '../src/workspace.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'
import { Indexer } from '../src/indexer/index.ts'

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
  test('an owned conversation is not a list row, and `parent` lists it', async () => {
    const cwd = workspace('repo', true)
    const parent = await create(cwd)
    const child = await create(cwd, 'subagent')
    // `index.update` is how an owner is recorded; a subagent sets it itself in M4, so the write stands in.
    await client.call('index.update', { id: child, ownerId: parent })

    const top = await client.call('conversation.list', {})
    expect((top['items'] as { id: number }[]).map((row) => row.id)).toEqual([parent])

    const children = await client.call('conversation.list', { parent })
    expect((children['items'] as { id: number }[]).map((row) => row.id)).toEqual([child])
    expect((children['items'] as { ownerId?: number }[])[0]?.ownerId).toBe(parent)

    const none = await client.call('conversation.list', { parent: 999 })
    expect(none['items']).toEqual([])
  })

  test('a conversation a person started carries neither an owner nor fork lineage', async () => {
    const cwd = workspace('solo', true)
    await create(cwd)
    const page = await client.call('conversation.list', {})
    const row = (page['items'] as { parentId?: number; ownerId?: number }[])[0]
    expect(row?.parentId).toBeUndefined()
    expect(row?.ownerId).toBeUndefined()
  })

  test('a fork is a row of its own: its parent is lineage, not an owner', async () => {
    const cwd = workspace('forked', true)
    const source = await create(cwd)
    // A fork needs an entry to fork at.
    await client.call('conversation.submit', { id: source, input: 'the first turn' })
    let anchor = 0
    for (let i = 0; i < 200; i++) {
      const page = await client.call('conversation.entries', {
        id: source,
        query: { order: 'ascending' },
        limit: 50,
      })
      const items = page['items'] as { id: number; kind: string }[]
      const user = items.find((entry) => entry.kind === 'pi.user')
      if (user !== undefined) {
        anchor = user.id
        break
      }
      await Bun.sleep(20)
    }
    if (anchor === 0) throw new Error('the source conversation never wrote the entry to fork at')
    const forked = await client.call('conversation.fork', { id: source, at: anchor })
    const forkId = forked['conversationId'] as number

    const page = await client.call('conversation.list', {})
    const rows = page['items'] as { id: number; parentId?: number; ownerId?: number }[]
    const fork = rows.find((row) => row.id === forkId)
    expect(fork).toBeDefined()
    expect(fork?.parentId).toBe(source)
    expect(fork?.ownerId).toBeUndefined()
  })

  test('an owner must exist, must not be the row itself, and must not make a cycle', async () => {
    const cwd = workspace('owners', true)
    const first = await create(cwd)
    const second = await create(cwd)

    const missing = await client.fails('index.update', { id: first, ownerId: 424242 })
    expect(missing.code).toBe(-32000)
    const self = await client.fails('index.update', { id: first, ownerId: first })
    expect(self.code).toBe(-32602)
    const bad = await client.fails('index.update', { id: first, ownerId: 'second' })
    expect(bad.code).toBe(-32602)

    await client.call('index.update', { id: second, ownerId: first })
    const cycle = await client.fails('index.update', { id: first, ownerId: second })
    expect(cycle.code).toBe(-32602)
    // The refusal left both rows as they were.
    const rows = (await client.call('conversation.list', {})).items as { id: number }[]
    expect(rows.map((row) => row.id)).toEqual([first])
    const owned = (await client.call('conversation.list', { parent: first })).items as {
      id: number
    }[]
    expect(owned.map((row) => row.id)).toEqual([second])
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
    const expected = await repoRootOf(plain)
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
    // A create is seen up to itself; opening the conversation writes the time it was opened.
    const fresh = await read()
    expect(fresh.viewedAt).toBe(fresh.lastActivityAt)
    await client.call('index.update', { id, viewedAt: null })
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

    // A refusal writes nothing, even when another field in the same call was valid: a caller that reads
    // -32602 as "nothing happened" must be right.
    const refused = await client.fails('index.update', {
      id,
      title: 'not stored',
      viewedAt: 'yesterday',
    })
    expect(refused.code).toBe(-32602)
    const list = await client.call('conversation.list', {})
    const row = (list['items'] as { id: number; title: string }[]).find((item) => item.id === id)
    expect(row?.title).not.toBe('not stored')
  })
})

describe('model.complete on a kernel that may not spend a key', () => {
  test('a read-only kernel refuses it with -32008', async () => {
    client.close()
    await kernel.stop()
    kernel = await start({ dir: data.dir, faux, overrides: { readOnly: true } })
    client = new Client(kernel.port)
    const failure = await client.fails('model.complete', {
      model: faux.model,
      input: 'name this conversation',
    })
    expect(failure.code).toBe(-32008)
  })
})

describe('a start fills in what a build before it could not', () => {
  test('a row whose repoRoot is missing gets one, and a resolved row is left alone', async () => {
    const dir = tempData()
    const db = new BunSqliteDatabase(join(dir.dir, 'probe.sqlite'))
    try {
      const indexer = new Indexer(db, {} as never, { flushIntervalMs: 0 })
      await indexer.open()
      // Two rows an earlier build left: one with a workspace and no repository, one already resolved.
      await db.run(
        `INSERT INTO droi_conversations (id, created_at, last_activity_at, workspace)
         VALUES (1, 1, 1, ?), (2, 1, 1, ?)`,
        join(dir.dir, 'repo', 'nested'),
        join(dir.dir, 'kept'),
      )
      await db.run('UPDATE droi_conversations SET repo_root = ? WHERE id = 2', '/somewhere/main')

      const asked: string[] = []
      await indexer.backfillRepoRoots(async (directory) => {
        asked.push(directory)
        return '/the/repository'
      })

      expect(asked).toEqual([join(dir.dir, 'repo', 'nested')])
      const rows = await db.all<{ id: number; repo_root: string | null }>(
        'SELECT id, repo_root FROM droi_conversations ORDER BY id',
      )
      expect(rows.map((row) => row.repo_root)).toEqual(['/the/repository', '/somewhere/main'])
      // Nothing is left to do, so a second start asks nothing.
      asked.length = 0
      await indexer.backfillRepoRoots(async (directory) => {
        asked.push(directory)
        return '/the/repository'
      })
      expect(asked).toEqual([])
      await indexer.close()
    } finally {
      await db.close()
      dir.cleanup()
    }
  })
})

describe('titleSource', () => {
  test('the kernel names a conversation and says the name is its own', async () => {
    const root = workspace('titles', true)
    const id = await create(root)
    let row: { id: number; title?: string; titleSource?: string } | undefined
    for (let i = 0; i < 100; i++) {
      await client.call('conversation.submit', { id, input: 'the first user message' })
      for (let n = 0; n < 100; n++) {
        const page = await client.call('conversation.list', {})
        row = (page['items'] as { id: number; title?: string; titleSource?: string }[]).find(
          (item) => item.id === id,
        )
        if (row?.title !== undefined) break
        await Bun.sleep(20)
      }
      break
    }
    expect(row?.title).toBe('the first user message')
    expect(row?.titleSource).toBe('auto')
  })

  test('index.update makes it a choice, and an automatic one can be replaced again', async () => {
    const root = workspace('renamed', true)
    const id = await create(root)
    await client.call('conversation.submit', { id, input: 'the first user message' })
    const read = async (): Promise<{ title?: string; titleSource?: string }> => {
      const page = await client.call('conversation.list', {})
      const row = (page['items'] as { id: number; title?: string; titleSource?: string }[]).find(
        (item) => item.id === id,
      )
      if (row === undefined) throw new Error('the conversation is not in the list')
      return row
    }
    for (let i = 0; i < 200 && (await read()).title === undefined; i++) await Bun.sleep(20)

    // A person names it: chosen, and the kernel's next automatic guess must not overwrite it.
    await client.call('index.update', { id, title: 'My name for it' })
    expect(await read()).toMatchObject({ title: 'My name for it', titleSource: 'chosen' })
    await client.call('conversation.submit', { id, input: 'a later message' })
    await Bun.sleep(200)
    expect((await read()).title).toBe('My name for it')

    // A generator names it: still auto, so the next generator may replace it.
    await client.call('index.update', { id, title: 'A generated name', titleSource: 'auto' })
    expect(await read()).toMatchObject({ title: 'A generated name', titleSource: 'auto' })

    // Clearing it goes back to the kernel's own name, the first message, which a generator may replace.
    await client.call('index.update', { id, title: null })
    expect(await read()).toMatchObject({ title: 'the first user message', titleSource: 'auto' })

    const bad = await client.fails('index.update', { id, title: 'x', titleSource: 'guessed' })
    expect(bad.code).toBe(-32602)
    const alone = await client.fails('index.update', { id, titleSource: 'auto' })
    expect(alone.code).toBe(-32602)
  })
})
