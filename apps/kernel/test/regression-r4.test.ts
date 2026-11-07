import { expect, test } from 'bun:test'
import { readFileSync, writeFileSync } from 'node:fs'
import { Database } from 'bun:sqlite'
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

type Outcome = {
  port?: number
  code: number | null
  signal: NodeJS.Signals | null
  stderr: string
}
type KernelProcess = {
  child: ReturnType<typeof spawn>
  /** The handshake port, or — when it was refused — the exit code and what it said. */
  outcome: Promise<Outcome>
  kill: (signal: NodeJS.Signals) => Promise<void>
}

/** What a group of simultaneous starts showed about mutual exclusion. */
type StartVerdict =
  | { kind: 'ok'; owners: number }
  /** A real failure: the caller's assertions would fail, and a retry would only hide it. */
  | { kind: 'violation'; reason: string }
  /** No verdict at all: a child died to a signal before it could answer or be refused. */
  | { kind: 'inconclusive'; reason: string }

/**
 * Judge one group of simultaneous starts.
 *
 * The order of the checks is the whole point. **More than one handshake is a violation, whatever else the
 * group did**, and it is returned before anything about signals is considered: a group in which a kernel
 * was killed *and* two kernels started is not an interrupted run, it is proof that the lock failed, and a
 * retry would replace that proof with the next attempt's cleaner result. Only a group that kept the
 * invariant and lost a child to a signal is retried — and that is not a judgement about the lock at all: a
 * child with no verdict was reaped, usually by the test runner.
 */
function judgeStartGroup(outcomes: readonly Outcome[]): StartVerdict {
  const owners = outcomes.filter((outcome) => outcome.port !== undefined)
  if (owners.length > 1) {
    return {
      kind: 'violation',
      reason: `${owners.length} kernels reached the handshake on one data directory (ports ${owners
        .map((owner) => owner.port)
        .join(', ')})`,
    }
  }
  // A start that neither answered nor was refused did not test the lock: it is a broken start, and
  // reporting it as a refusal would pass the "exactly one owner" assertion for the wrong reason.
  const broken = outcomes.filter(
    (outcome) => outcome.port === undefined && outcome.signal === null && outcome.code !== 2,
  )
  if (broken.length > 0) {
    return {
      kind: 'violation',
      reason: `a start failed for another reason: ${broken
        .map((outcome) => `exit ${outcome.code} ${outcome.stderr}`)
        .join(' | ')}`,
    }
  }
  // Every start was refused and nothing was killed: each one was told somebody else held the lock, and
  // nobody did. That is not a missing verdict, it is a group of kernels that could not start — a worse
  // failure than two owners, because the data directory is left with nothing running in it at all.
  if (owners.length === 0 && outcomes.every((outcome) => outcome.signal === null)) {
    return {
      kind: 'violation',
      reason: `every start was refused (exit 2) and none reached the handshake: ${outcomes
        .map((outcome) => outcome.stderr.trim().slice(0, 200))
        .join(' | ')}`,
    }
  }
  // A child killed before it could answer or be refused tested nothing: that is the one case that is
  // started again, and it includes the case where a real owner answered and another was reaped.
  const interrupted = outcomes.filter((outcome) => outcome.signal !== null)
  if (interrupted.length > 0) {
    return {
      kind: 'inconclusive',
      reason: `${interrupted.length} of ${outcomes.length} starts were killed by ${interrupted
        .map((outcome) => outcome.signal)
        .join(', ')}`,
    }
  }
  return { kind: 'ok', owners: owners.length }
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
  const outcome = new Promise<Outcome>((resolve) => {
    const settled = { done: false }
    const settle = (code: number | null, signal: NodeJS.Signals | null) => {
      if (settled.done) return
      settled.done = true
      clearInterval(timer)
      resolve({
        ...(out.includes('\n') ? { port: JSON.parse(out.split('\n')[0]!).port as number } : {}),
        code,
        signal,
        stderr,
      })
    }
    // Whichever comes first: the handshake line, or the process leaving.
    const timer = setInterval(() => {
      if (!out.includes('\n')) return
      setTimeout(() => settle(null, null), 100)
    }, 20)
    child.once('exit', (code, signal) => settle(code, signal))
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

/**
 * Start `count` kernels at the same instant, and report what each one did.
 *
 * Only an inconclusive group is launched again; a group that showed a violation is returned as it was, so
 * the failure is the one that actually happened.
 */
async function starters(dir: string, count: number, attempts = 3): Promise<Outcome[]> {
  let last: StartVerdict | undefined
  for (let attempt = 0; attempt < attempts; attempt++) {
    const group = Array.from({ length: count }, () => startProcess(dir))
    const outcomes = await Promise.all(group.map((starter) => starter.outcome))
    await Promise.all(group.map((starter) => starter.kill('SIGKILL')))
    const verdict = judgeStartGroup(outcomes)
    if (verdict.kind === 'ok') return outcomes
    if (verdict.kind === 'violation') throw new Error(verdict.reason)
    // A retry is itself a fact worth having in the log: with no reason printed, a flake looks like a pass.
    console.error(
      `start group ${attempt + 1} was inconclusive, starting it again: ${verdict.reason}`,
    )
    last = verdict
  }
  throw new Error(
    `every attempt to start ${count} kernels was inconclusive: ${last?.kind === 'inconclusive' ? last.reason : ''}`,
  )
}

test('R4 a group with two handshakes is a violation, never a retry', () => {
  // The classification is the guard against masking: one killed child must not launder a group that
  // started two kernels.
  const owner = (port: number): Outcome => ({ port, code: null, signal: null, stderr: '' })
  const refused = (): Outcome => ({
    code: 2,
    signal: null,
    stderr: 'another kernel (pid 1) is using this data directory',
  })
  const killed = (): Outcome => ({ code: null, signal: 'SIGKILL', stderr: '' })

  const masked = judgeStartGroup([owner(1001), owner(1002), killed()])
  expect(masked.kind).toBe('violation')
  expect(masked.kind === 'violation' ? masked.reason : '').toContain(
    '2 kernels reached the handshake',
  )

  // A single owner and a killed child is the one case worth starting again: nobody saw a verdict.
  expect(judgeStartGroup([owner(1003), refused(), killed()]).kind).toBe('inconclusive')
  expect(judgeStartGroup([owner(1004), refused(), refused()]).kind).toBe('ok')
  // A start that neither answered nor was refused is broken, not a refusal.
  const broken = judgeStartGroup([refused(), { code: 1, signal: null, stderr: 'boom' }])
  expect(broken.kind).toBe('violation')
  // Nobody started and nobody was killed: every start was told somebody else held the lock, and the data
  // directory is left with no kernel at all. Retrying this would hide a directory that cannot be started.
  const nobodyStarted = judgeStartGroup([refused(), refused(), refused()])
  expect(nobodyStarted.kind).toBe('violation')
})

test('R4 a lock database left in WAL mode is normalized, and still excludes', async () => {
  const data = tempData()
  const path = join(data.dir, 'kernel.lock.db')
  // A lock database an earlier build wrote in WAL mode. The lock must not depend on which mode it finds.
  const prepared = new Database(path, { create: true })
  prepared.exec('PRAGMA journal_mode = WAL')
  prepared.exec('CREATE TABLE IF NOT EXISTS droi_lock (owner INTEGER)')
  prepared.exec('INSERT INTO droi_lock (owner) VALUES (0)')
  prepared.close()
  const owner = startProcess(data.dir)
  try {
    expect((await owner.outcome).port).toBeGreaterThan(0)
    const rival = await startProcess(data.dir).outcome
    expect(rival.port).toBeUndefined()
    expect(rival.code).toBe(2)
    await owner.kill('SIGKILL')
    const inspect = new Database(path)
    try {
      expect(inspect.query('PRAGMA journal_mode').get()).toEqual({ journal_mode: 'delete' })
    } finally {
      inspect.close()
    }
  } finally {
    await owner.kill('SIGKILL')
    data.cleanup()
  }
}, 40_000)

test('R4 a lock database that cannot be opened fails the start with exit 1', async () => {
  const data = tempData()
  // Not a database at all: no retry fixes this, so it must not be reported as "another kernel is using
  // this data directory" (exit 2, which a Supervisor retries) — the operator has a file to deal with.
  writeFileSync(join(data.dir, 'kernel.lock.db'), 'this is not a SQLite database, it is a sentence')
  const started = startProcess(data.dir)
  try {
    const outcome = await started.outcome
    expect(outcome.port).toBeUndefined()
    expect(outcome.code, outcome.stderr).toBe(1)
    expect(outcome.stderr).toContain('cannot be used')
    expect(outcome.stderr).not.toContain('another kernel')
  } finally {
    await started.kill('SIGKILL')
    data.cleanup()
  }
}, 40_000)

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
  try {
    const outcomes = await starters(data.dir, 3)
    const owners = outcomes.filter((outcome) => outcome.port !== undefined)
    const refused = outcomes.filter((outcome) => outcome.port === undefined)
    expect(owners).toHaveLength(1)
    expect(refused).toHaveLength(2)
    for (const loser of refused) {
      expect(loser.code, loser.stderr).toBe(2)
      expect(loser.stderr).toContain('another kernel')
    }
  } finally {
    data.cleanup()
  }
}, 40_000)

test('R4 five starts at once, repeated, always leave exactly one kernel', async () => {
  // The all-refused failure this guards against is rare and load-dependent: measured on the previous
  // head, one round in forty of three simultaneous starts ended with every start refused (exit 2) and no
  // kernel running, because their setup statements collided on a zero busy timeout. Four rounds here,
  // five at a time, is the suite-sized version of the twenty-round run this was found with.
  const data = tempData()
  try {
    for (let round = 1; round <= 4; round++) {
      const outcomes = await starters(data.dir, 5)
      const owners = outcomes.filter((outcome) => outcome.port !== undefined)
      const refused = outcomes.filter((outcome) => outcome.port === undefined)
      expect(
        owners,
        `round ${round}: ${refused.map((entry) => entry.stderr).join(' | ')}`,
      ).toHaveLength(1)
      expect(refused).toHaveLength(4)
      for (const loser of refused) {
        expect(loser.code, `round ${round}: ${loser.stderr}`).toBe(2)
        expect(loser.stderr).toContain('another kernel')
      }
    }
  } finally {
    data.cleanup()
  }
}, 60_000)

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
