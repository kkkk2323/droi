import { afterEach, describe, expect, test } from 'bun:test'
import { execFileSync } from 'node:child_process'
import { chmodSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { forgetRepoRoots } from '../src/workspace.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'

/** What the sidebar groups and names by, at the edges: forks, cleared titles, a cleared cwd, a slow git. */

type Row = {
  id: number
  kind?: string
  title?: string
  titleSource?: string
  workspace?: string
  repoRoot?: string
  parentId?: number
}

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
  forgetRepoRoots()
})

async function boot(dir: string, faux: Faux): Promise<{ kernel: KernelHandle; client: Client }> {
  const kernel = await start({ dir, faux })
  const client = new Client(kernel.port)
  let stopped = false
  const stop = async (): Promise<void> => {
    if (stopped) return
    stopped = true
    client.close()
    await kernel.stop()
  }
  cleanups.push(stop)
  return { kernel, client }
}

function repo(parent: string, name: string): string {
  const dir = join(parent, name)
  mkdirSync(dir, { recursive: true })
  execFileSync('git', ['init', '-q'], { cwd: dir, env: process.env })
  return dir
}

async function rows(client: Client, params: Record<string, unknown> = {}): Promise<Row[]> {
  return (await client.call('conversation.list', params))['items'] as Row[]
}

async function rowOf(client: Client, id: number): Promise<Row | undefined> {
  return (await rows(client)).find((row) => row.id === id)
}

async function firstUserEntry(client: Client, id: number): Promise<number> {
  for (let i = 0; i < 250; i++) {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 50,
    })
    const user = (page['items'] as { id: number; kind: string }[]).find(
      (entry) => entry.kind === 'pi.user',
    )
    if (user !== undefined) return user.id
    await Bun.sleep(20)
  }
  throw new Error('no user entry')
}

/** A `git` that does `body` first, put in front of PATH for the rest of the test. */
function shimGit(body: string): () => void {
  const real = execFileSync('/bin/sh', ['-c', 'command -v git'], { encoding: 'utf8' }).trim()
  const dir = mkdtempSync(join(tmpdir(), 'git-shim-'))
  writeFileSync(join(dir, 'git'), `#!/bin/sh\n${body.replaceAll('$REAL', real)}\n`)
  chmodSync(join(dir, 'git'), 0o755)
  const original = process.env['PATH'] ?? ''
  process.env['PATH'] = dir + ':' + original
  let undone = false
  const undo = (): void => {
    if (undone) return
    undone = true
    process.env['PATH'] = original
    rmSync(dir, { recursive: true, force: true })
  }
  cleanups.push(undo)
  return undo
}

describe('a fork', () => {
  test('is a row in its source repository, of its kind, with its name, before and after a restart', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    let faux = fauxCatalog()
    let { kernel, client } = await boot(data.dir, faux)
    const cwd = repo(data.dir, 'forked')
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd },
      kind: 'chat',
    })
    const source = created['conversationId'] as number
    faux.faux.appendResponses([fauxAssistantMessage('ok')])
    await client.call('conversation.submit', { id: source, input: 'the turn to fork at' })
    const at = await firstUserEntry(client, source)
    for (let i = 0; i < 100 && (await rowOf(client, source))?.title === undefined; i++) {
      await Bun.sleep(20)
    }
    const forked = await client.call('conversation.fork', { id: source, at })
    const forkId = forked['conversationId'] as number

    const expectRow = (row: Row | undefined): void => {
      expect(row?.workspace).toBe(cwd)
      expect(row?.repoRoot).toBe(realpathSync(cwd))
      expect(row?.kind).toBe('chat')
      expect(row?.parentId).toBe(source)
      expect(row?.title).toBe('the turn to fork at')
      expect(row?.titleSource).toBe('auto')
    }
    expectRow(await rowOf(client, forkId))
    expect((await rows(client, { workspace: cwd })).map((row) => row.id)).toContain(forkId)
    expect((await rows(client, { repoRoot: realpathSync(cwd) })).map((row) => row.id)).toContain(
      forkId,
    )

    // A rebuilt row comes from the fork's own droi.meta, which must hold the same facts.
    await kernel.stop()
    client.close()
    const db = new BunSqliteDatabase(join(data.dir, 'kernel.sqlite'))
    await db.run('DELETE FROM droi_conversations WHERE id = ?', forkId)
    await db.close()
    forgetRepoRoots()
    faux = fauxCatalog()
    ;({ kernel, client } = await boot(data.dir, faux))
    let rebuilt: Row | undefined
    for (let i = 0; i < 100; i++) {
      rebuilt = await rowOf(client, forkId)
      if (rebuilt?.repoRoot !== undefined) break
      await Bun.sleep(20)
    }
    expect(rebuilt?.workspace).toBe(cwd)
    expect(rebuilt?.repoRoot).toBe(realpathSync(cwd))
    expect(rebuilt?.kind).toBe('chat')
  }, 60_000)
})

describe('a cleared title', () => {
  test('is the first message again, as auto, and a later message does not rename it', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const faux = fauxCatalog()
    const { client } = await boot(data.dir, faux)
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: data.dir },
      kind: 'chat',
    })
    const id = created['conversationId'] as number
    faux.faux.appendResponses([fauxAssistantMessage('one')])
    await client.call('conversation.submit', { id, input: 'the first user message' })
    for (let i = 0; i < 200 && (await rowOf(client, id))?.title === undefined; i++) {
      await Bun.sleep(20)
    }
    await client.call('index.update', { id, title: 'a name I chose' })
    await client.call('index.update', { id, title: null })
    const cleared = await rowOf(client, id)
    expect(cleared?.title).toBe('the first user message')
    expect(cleared?.titleSource).toBe('auto')

    faux.faux.appendResponses([fauxAssistantMessage('two')])
    await client.call('conversation.submit', { id, input: 'a much later message' })
    await client.waitFor(
      (note) => note.method === 'conversation.busy' && note.params['run'] === null,
      20_000,
    )
    await Bun.sleep(200)
    expect((await rowOf(client, id))?.title).toBe('the first user message')
  }, 60_000)

  test('and a rename are announced with where the name came from', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const faux = fauxCatalog()
    const { client } = await boot(data.dir, faux)
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: data.dir },
      kind: 'chat',
    })
    const id = created['conversationId'] as number
    client.notifications.length = 0
    await client.call('index.update', { id, title: 'generated', titleSource: 'auto' })
    const auto = await client.waitFor(
      (note) => note.method === 'index.changed' && note.params['title'] === 'generated',
    )
    expect(auto.params['titleSource']).toBe('auto')
    await client.call('index.update', { id, title: 'typed' })
    const chosen = await client.waitFor(
      (note) => note.method === 'index.changed' && note.params['title'] === 'typed',
    )
    expect(chosen.params['titleSource']).toBe('chosen')
  }, 60_000)
})

describe('a cleared cwd', () => {
  test('clears the row workspace and repository', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const faux = fauxCatalog()
    const { client } = await boot(data.dir, faux)
    const first = repo(data.dir, 'first')
    const second = repo(data.dir, 'second')
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: first },
      kind: 'chat',
    })
    const id = created['conversationId'] as number
    await client.call('conversation.configure', { id, change: { cwd: second } })
    expect((await rowOf(client, id))?.workspace).toBe(second)
    await client.call('conversation.configure', { id, change: { cwd: null } })
    const cleared = await rowOf(client, id)
    expect(cleared?.workspace).toBeNull()
    expect(cleared?.repoRoot).toBeUndefined()
  }, 60_000)
})

describe.skipIf(process.platform === 'win32')('git', () => {
  test('a start that cannot ask git leaves the repository unknown, and the next start asks again', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const dir = repo(data.dir, 'repo')
    const first = await boot(data.dir, fauxCatalog())
    const created = await first.client.call('conversation.create', {
      agent: { model: fauxCatalog().model, cwd: dir },
      kind: 'chat',
    })
    const id = created['conversationId'] as number
    await first.client.close()
    await first.kernel.stop()
    // An earlier build's row: a workspace, no repository.
    const db = new BunSqliteDatabase(join(data.dir, 'kernel.sqlite'))
    await db.run('UPDATE droi_conversations SET repo_root = NULL WHERE id = ?', id)
    await db.close()

    forgetRepoRoots()
    const undo = shimGit('echo "fatal: cannot run" >&2\nexit 1')
    const broken = await boot(data.dir, fauxCatalog())
    await Bun.sleep(200)
    expect((await rowOf(broken.client, id))?.repoRoot).toBeUndefined()
    broken.client.close()
    await broken.kernel.stop()

    // The shim goes; the next start asks the real git.
    undo()
    forgetRepoRoots()
    const good = await boot(data.dir, fauxCatalog())
    let answered: Row | undefined
    for (let i = 0; i < 100; i++) {
      answered = await rowOf(good.client, id)
      if (answered?.repoRoot !== undefined) break
      await Bun.sleep(20)
    }
    expect(answered?.repoRoot).toBe(realpathSync(dir))
  }, 60_000)

  test('a slow git does not hold up another connection', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const faux = fauxCatalog()
    const { kernel, client } = await boot(data.dir, faux)
    const other = new Client(kernel.port)
    cleanups.push(() => other.close())
    const dir = repo(data.dir, 'slow')
    shimGit('sleep 1\nexec $REAL "$@"')
    const started = performance.now()
    const created = client.call('conversation.create', {
      agent: { model: faux.model, cwd: dir },
      kind: 'chat',
    })
    let slowest = 0
    while (performance.now() - started < 900) {
      const t = performance.now()
      await other.call('harness.info', {})
      slowest = Math.max(slowest, performance.now() - t)
      await Bun.sleep(20)
    }
    await created
    expect(slowest).toBeLessThan(300)
  }, 60_000)
})
