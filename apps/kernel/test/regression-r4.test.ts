import { expect, test } from 'bun:test'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { Client, TOKEN, allEntries, fauxCatalog, start, tempData } from './helpers.ts'
import { context } from '../src/session/service.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'
import { Indexer } from '../src/indexer/index.ts'
import { takeLock } from '../src/lock.ts'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'

test('R4 an entry newer than its create wins inside the first index batch', async () => {
  const data = tempData()
  const db = new BunSqliteDatabase(join(data.dir, 'index.sqlite'))
  const index = new Indexer(db, {} as never, { flushIntervalMs: 60_000 })
  try {
    await index.open()
    // Admission at 100, then the run's first output at 200 — both still queued, so both land in one row.
    await index.touch(2, 100, { createdAt: 100 })
    index.entry({
      id: 3,
      conversationId: 2,
      kind: 'pi.assistant',
      model: [{ ...fauxAssistantMessage('new response'), timestamp: 200 }],
    } as never)
    expect((await index.conversation(2))!.last_activity_at).toBe(200)
  } finally {
    await index.close()
    await db.close()
    data.cleanup()
  }
})

test('R4 a first answer in the first index batch advances the listed activity', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    let timestamp = 0
    faux.faux.setResponses([
      async () => {
        await Bun.sleep(20)
        timestamp = Date.now()
        return fauxAssistantMessage('first output', { timestamp })
      },
    ])
    await client.call('conversation.submit', { id, input: 'a first ordinary prompt' })
    await (await kernel.harness.conversation(id as never, context))!.waitForIdle(context)
    const row = (
      (await client.call('conversation.list')).items as { id: number; lastActivityAt: number }[]
    ).find((item) => item.id === id)!
    expect(row.lastActivityAt).toBe(timestamp)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R4 a stale-guard remover never unlinks a live guard', () => {
  const data = tempData()
  const path = join(data.dir, 'kernel.lock')
  // A lock and a guard whose owners are gone, as after a SIGKILL during a takeover.
  writeFileSync(path, '99999999')
  writeFileSync(`${path}.guard`, '88888888')
  const live = 4242
  const alive = (pid: number) => {
    if (pid === 88888888) {
      // The exact window R4-2 describes: another starter clears the dead guard and installs its own
      // live one between our staleness check and our removal. An observation is not authorization.
      writeFileSync(`${path}.guard`, String(live))
      return false
    }
    return pid === live
  }
  expect(() => takeLock(path, { isAlive: alive })).toThrow('another kernel')
  // The other starter's guard is untouched, and nobody took the lock over behind its back.
  expect(readFileSync(`${path}.guard`, 'utf8').trim()).toBe(String(live))
  expect(readFileSync(path, 'utf8').trim()).toBe('99999999')
  data.cleanup()
})

test('R4 a second starter cannot clear the guard a live one is replacing', async () => {
  const data = tempData()
  const path = join(data.dir, 'kernel.lock')
  // A lock and a guard whose owners are both gone: the state after a SIGKILL during a takeover.
  writeFileSync(path, '99999999')
  writeFileSync(`${path}.guard`, '99999998')
  const launch = (role: string) => {
    const child = spawn(process.execPath, [
      new URL('./r4-lock-race-child.ts', import.meta.url).pathname,
      data.dir,
      role,
    ])
    const output = { log: '' }
    child.stdout.on('data', (chunk) => (output.log += chunk))
    child.stderr.on('data', (chunk) => (output.log += chunk))
    const exited = new Promise<number | null>((resolve) => child.once('exit', resolve))
    return { child, output, exited }
  }
  const waitFor = async (name: string): Promise<void> => {
    const deadline = Date.now() + 5000
    while (!existsSync(join(data.dir, name))) {
      if (Date.now() > deadline) throw new Error(`no ${name}`)
      await Bun.sleep(5)
    }
  }
  const first = launch('A')
  try {
    // A is inside the takeover: it holds the fence, has judged the guard dead, and has not unlinked it.
    await waitFor('A-before-guard-unlink.paused')
    const second = launch('B')
    await second.exited
    // B saw a live fence: it may not touch the guard, so it refuses instead of starting a second kernel.
    expect(existsSync(join(data.dir, 'B.won'))).toBe(false)
    expect(readFileSync(join(data.dir, 'B.refused'), 'utf8')).toContain('another kernel')
    writeFileSync(join(data.dir, 'A-before-guard-unlink.go'), '')
    await first.exited
    const winner = Number(readFileSync(join(data.dir, 'A.won'), 'utf8'))
    expect(winner).toBeGreaterThan(0)
    expect(Number(readFileSync(path, 'utf8').trim())).toBe(winner)
    expect(existsSync(`${path}.guard`)).toBe(false)
    expect(existsSync(`${path}.guard.break`)).toBe(false)
  } finally {
    first.child.kill('SIGKILL')
    await first.exited
    data.cleanup()
  }
}, 20_000)

test('R4 two starters over a dead lock leave exactly one owner and exit 2', async () => {
  const data = tempData()
  writeFileSync(join(data.dir, 'kernel.lock'), '99999999')
  writeFileSync(join(data.dir, 'kernel.lock.guard'), '99999998')
  const main = new URL('../src/main.ts', import.meta.url).pathname
  type Outcome = { handshake?: number; code: number | null; stderr: string }
  const launch = (): { kill: () => void; done: Promise<Outcome> } => {
    const child = spawn(process.execPath, [main, '--data', data.dir], {
      env: { ...process.env, DROI_KERNEL_TOKEN: TOKEN },
    })
    let out = ''
    let stderr = ''
    child.stdout.on('data', (chunk) => (out += chunk))
    child.stderr.on('data', (chunk) => (stderr += chunk))
    const done = new Promise<Outcome>((resolve) => {
      // Whichever comes first: the handshake, or an exit. Both starters are racing, and only one of
      // them may reach the handshake.
      const finish = (code: number | null) =>
        resolve({
          handshake: out.includes('\n')
            ? (JSON.parse(out.split('\n')[0]!).port as number)
            : undefined,
          code,
          stderr,
        })
      child.once('exit', (code) => finish(code))
      const timer = setInterval(() => {
        if (!out.includes('\n')) return
        clearInterval(timer)
        setTimeout(() => finish(null), 100)
      }, 20)
    })
    return { kill: () => child.kill('SIGTERM'), done }
  }
  const first = launch()
  const second = launch()
  try {
    const outcomes = await Promise.all([first.done, second.done])
    const owners = outcomes.filter((outcome) => outcome.handshake !== undefined)
    const losers = outcomes.filter((outcome) => outcome.handshake === undefined)
    expect(owners).toHaveLength(1)
    expect(losers).toHaveLength(1)
    // A refused start is its own status, not a generic crash: the Host must not read it as a bug.
    expect(losers[0]!.code).toBe(2)
    expect(losers[0]!.stderr).toContain('another kernel')
    const owner = owners[0]!.handshake!
    expect(Number(readFileSync(join(data.dir, 'kernel.lock'), 'utf8').trim())).toBeGreaterThan(0)
    void owner
  } finally {
    first.kill()
    second.kill()
    await Promise.all([first.done, second.done]).catch(() => undefined)
    data.cleanup()
  }
}, 30_000)

test('R4 the request which first hits failed storage returns kernel_failed', async () => {
  const data = tempData()
  const child = spawn(process.execPath, [
    new URL('./r4-failing-request-child.ts', import.meta.url).pathname,
    data.dir,
  ])
  let stderr = ''
  child.stderr.on('data', (chunk) => (stderr += chunk))
  const exited = new Promise<number | null>((resolve) => child.once('exit', resolve))
  let client: Client | undefined
  try {
    const port = await new Promise<number>((resolve, reject) => {
      let out = ''
      const timer = setTimeout(() => reject(new Error(stderr)), 10_000)
      child.stdout.on('data', (chunk) => {
        out += chunk
        if (!out.includes('\n')) return
        clearTimeout(timer)
        resolve(JSON.parse(out.split('\n')[0]!).port as number)
      })
    })
    client = new Client(port)
    const result = await client.call('conversation.create').then(
      (value) => ({ value, code: 0 }),
      (error: { code: number }) => ({ code: error.code }),
    )
    // The write failed because the storage did: the caller is told the kernel failed, not that it
    // sent something malformed.
    expect(result.code).toBe(-32010)
    expect(await exited).toBe(3)
    expect(client.notifications.some((note) => note.params['kind'] === 'kernel_failed')).toBe(true)
  } finally {
    child.kill('SIGKILL')
    client?.close()
    data.cleanup()
  }
}, 30_000)

test('R4 nested forks isolate both directions of request keys across restart', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  let kernel = await start({ dir: data.dir, faux })
  let client = new Client(kernel.port)
  const submit = async (id: unknown, requestId: string, input: string) => {
    faux.faux.setResponses([fauxAssistantMessage(`${input}-answer`)])
    const result = await client.call('conversation.submit', { id, requestId, input })
    await (await kernel.harness.conversation(id as never, context))!.waitForIdle(context)
    return result['submissionId']
  }
  try {
    const { conversationId: parent } = await client.call('conversation.create', {
      requestId: 'parent-create',
      agent: { model: faux.model },
    })
    const parentKey = await submit(parent, 'ancestor-key', 'parent prompt')
    const anchor = (await allEntries(client, parent as number)).at(-1)!.id
    const { conversationId: fork } = await client.call('conversation.fork', {
      id: parent,
      at: anchor,
    })
    const forkKey = await submit(fork, 'ancestor-key', 'fork prompt different from parent')
    expect(forkKey).not.toBe(parentKey)
    const forkOnly = await submit(fork, 'fork-first-key', 'fork first')
    const parentLater = await submit(parent, 'fork-first-key', 'parent later')
    expect(parentLater).not.toBe(forkOnly)
    const forkAnchor = (await allEntries(client, fork as number)).at(-1)!.id
    const { conversationId: nested } = await client.call('conversation.fork', {
      id: fork,
      at: forkAnchor,
    })
    const nestedKey = await submit(nested, 'ancestor-key', 'nested prompt')
    expect([parentKey, forkKey]).not.toContain(nestedKey)
    await kernel.indexer.flush()
    client.close()
    await kernel.stop()
    kernel = await start({ dir: data.dir, faux })
    client = new Client(kernel.port)
    const rows = (await client.call('conversation.list')).items as {
      id: number
      parentId?: number
    }[]
    expect(rows.find((row) => row.id === fork)!.parentId).toBe(parent as number)
    expect(rows.find((row) => row.id === nested)!.parentId).toBe(fork as number)
    const retries: [unknown, string, unknown][] = [
      [parent, 'parent prompt', parentKey],
      [fork, 'fork prompt different from parent', forkKey],
      [nested, 'nested prompt', nestedKey],
    ]
    for (const [target, prompt, submission] of retries) {
      const result = await client.call('conversation.submit', {
        id: target,
        input: prompt,
        requestId: 'ancestor-key',
      })
      expect(result['submissionId']).toBe(submission)
    }
    // A parent's own text is the parent's: its forks carry it in their transcript, not in the index.
    const found = await client.call('conversation.list', { query: 'parent prompt-answer' })
    expect((found['items'] as { id: number }[]).map((row) => row.id)).toEqual([parent as number])
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
}, 30_000)

test('R4 a fork before the parent metadata still gets its own metadata in one commit', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const legacy = await kernel.harness.createConversation(
      {
        ownership: { kind: 'ownerless' },
        agent: { model: faux.model } as never,
        init: async (tx, id) => {
          await tx.appendEntry(id, { kind: 'droi.note', data: { text: 'before metadata' } })
          await tx.appendEntry(id, {
            kind: 'droi.meta',
            data: { createdAt: 123, requestId: 'legacy-parent' },
          })
        },
      },
      context,
    )
    const parent = legacy.id as unknown as number
    const entries = await allEntries(client, parent)
    const publications: { changes: { type: string; value: Record<string, unknown> }[] }[] = []
    kernel.harness.observeCommits({
      advance: (publication) => publications.push(publication as never),
      end: () => undefined,
    })
    const { conversationId: fork } = await client.call('conversation.fork', {
      id: parent,
      at: entries[0]!.id,
    })
    const own = (await allEntries(client, fork as number)) as unknown as {
      kind: string
      data: { parentId?: number }
    }[]
    const metas = own.filter((entry) => entry.kind === 'droi.meta')
    expect(metas).toHaveLength(1)
    expect(metas[0]!.data.parentId).toBe(parent)
    // The fork's identity is in the same publication as the fork itself: it cannot be lost between
    // Pi storing the conversation and the kernel writing the record.
    const publication = publications.find((candidate) =>
      candidate.changes.some(
        (change) => change.type === 'conversation' && change.value['id'] === fork,
      ),
    )
    expect(
      publication!.changes.some(
        (change) =>
          change.type === 'entry' &&
          change.value['conversationId'] === fork &&
          change.value['kind'] === 'droi.meta',
      ),
    ).toBe(true)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R4 a permanently failing index warns once per batch and retains it', async () => {
  const data = tempData()
  const db = new BunSqliteDatabase(join(data.dir, 'index.sqlite'))
  let warnings = 0
  let attempts = 0
  const index = new Indexer(db, {} as never, { flushIntervalMs: 5, onError: () => warnings++ })
  const original = db.transaction.bind(db)
  try {
    await index.open()
    db.transaction = async () => {
      attempts++
      throw new Error('permanent index failure')
    }
    await index.touch(2, 1, { createdAt: 1 })
    await Bun.sleep(30)
    const first = attempts
    await Bun.sleep(30)
    // No self-retry spin: a broken index does not burn the CPU it is already failing to use.
    expect(attempts).toBe(first)
    expect(warnings).toBeGreaterThan(0)
    for (let id = 3; id < 103; id++) {
      index.entry({
        id,
        conversationId: 2,
        kind: 'droi.note',
        data: { text: `retained-${id}` },
      } as never)
    }
    await Bun.sleep(30)
    db.transaction = original
    await index.flush()
    expect(await db.get('SELECT COUNT(*) AS n FROM droi_entries_fts')).toEqual({ n: 100 })
  } finally {
    db.transaction = original
    await index.close()
    await db.close()
    data.cleanup()
  }
})
