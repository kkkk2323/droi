import { expect, test } from 'bun:test'
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { Client, TOKEN, allEntries, fauxCatalog, start, tempData } from './helpers.ts'
import { context } from '../src/session/service.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'
import { Indexer } from '../src/indexer/index.ts'
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

type KernelProcess = {
  child: ReturnType<typeof spawn>
  /** The handshake port, or — when it was refused — the exit code and what it said. */
  outcome: Promise<{ port?: number; code: number | null; stderr: string }>
  kill: (signal: NodeJS.Signals) => Promise<void>
}

function startProcess(dir: string): KernelProcess {
  const child = spawn(
    process.execPath,
    [new URL('../src/main.ts', import.meta.url).pathname, '--data', dir],
    { env: { ...process.env, DROI_KERNEL_TOKEN: TOKEN } },
  )
  let out = ''
  let stderr = ''
  child.stdout.on('data', (chunk) => (out += chunk))
  child.stderr.on('data', (chunk) => (stderr += chunk))
  const outcome = new Promise<{ port?: number; code: number | null; stderr: string }>((resolve) => {
    const settle = (code: number | null) =>
      resolve({
        ...(out.includes('\n') ? { port: JSON.parse(out.split('\n')[0]!).port as number } : {}),
        code,
        stderr,
      })
    // Whichever comes first: the handshake line, or the process leaving.
    const timer = setInterval(() => {
      if (!out.includes('\n')) return
      clearInterval(timer)
      setTimeout(() => settle(null), 100)
    }, 20)
    child.once('exit', (code) => {
      clearInterval(timer)
      settle(code)
    })
  })
  return {
    child,
    outcome,
    kill: async (signal) => {
      if (child.exitCode !== null || child.signalCode !== null) return
      const gone = new Promise<void>((resolve) => child.once('exit', () => resolve()))
      child.kill(signal)
      await gone
    },
  }
}

test('R4 a second start is refused while the first runs, and starts the moment it dies', async () => {
  const data = tempData()
  const owner = startProcess(data.dir)
  try {
    const first = await owner.outcome
    expect(first.port).toBeGreaterThan(0)
    // The PID file is diagnosis only, but a refusal should be able to name the kernel in the way.
    expect(Number(readFileSync(join(data.dir, 'kernel.pid'), 'utf8').trim())).toBe(owner.child.pid!)

    const rival = startProcess(data.dir)
    const refused = await rival.outcome
    expect(refused.port).toBeUndefined()
    expect(refused.code).toBe(2)
    expect(refused.stderr).toContain(`another kernel (pid ${owner.child.pid})`)

    await owner.kill('SIGKILL')
    // No waiting, no staleness check, no repair: the operating system released the lock with the process.
    const started = Date.now()
    const next = startProcess(data.dir)
    const third = await next.outcome
    await next.kill('SIGTERM')
    expect(third.port).toBeGreaterThan(0)
    expect(Date.now() - started).toBeLessThan(5000)
  } finally {
    await owner.kill('SIGKILL')
    data.cleanup()
  }
}, 40_000)

test('R4 three starts at once leave exactly one kernel', async () => {
  const data = tempData()
  const starters = [startProcess(data.dir), startProcess(data.dir), startProcess(data.dir)]
  try {
    const outcomes = await Promise.all(starters.map((starter) => starter.outcome))
    const owners = outcomes.filter((outcome) => outcome.port !== undefined)
    const refused = outcomes.filter((outcome) => outcome.port === undefined)
    expect(owners).toHaveLength(1)
    expect(refused).toHaveLength(2)
    for (const loser of refused) expect(loser.code).toBe(2)
  } finally {
    await Promise.all(starters.map((starter) => starter.kill('SIGKILL')))
    data.cleanup()
  }
}, 40_000)

test('R4 a lock left by a crash never blocks the next start', async () => {
  // R4b-2: a SIGKILL between creating a claim file and writing its owner refused every later start for
  // ever. There is no such window now — the file is the database — and all a killed kernel leaves is a hot
  // journal, which SQLite rolls back for the next starter.
  const data = tempData()
  const holder = startProcess(data.dir)
  try {
    expect((await holder.outcome).port).toBeGreaterThan(0)
    await holder.kill('SIGKILL')
    // The old algorithm's residue, in both shapes: an empty main lock, and a PID text file.
    writeFileSync(join(data.dir, 'kernel.lock'), '')
    const started = Date.now()
    const next = startProcess(data.dir)
    const outcome = await next.outcome
    await next.kill('SIGKILL')
    expect(outcome.port).toBeGreaterThan(0)
    expect(Date.now() - started).toBeLessThan(5000)
  } finally {
    await holder.kill('SIGKILL')
    data.cleanup()
  }
}, 40_000)
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
