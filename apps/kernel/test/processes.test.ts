import { afterEach, describe, expect, test } from 'bun:test'
import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { Database } from 'bun:sqlite'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'

/** `bash_background`, `process.list`, `process.stop` and `process.changed` (design/m3-kernel.md §7). */

const posix = process.platform !== 'win32'
const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

type Booted = { kernel: KernelHandle; client: Client; faux: Faux }

async function boot(dir: string): Promise<Booted> {
  const faux = fauxCatalog()
  const kernel = await start({ dir, faux })
  const client = new Client(kernel.port)
  let stopped = false
  cleanups.push(async () => {
    if (stopped) return
    stopped = true
    client.close()
    await kernel.stop()
  })
  return { kernel, client, faux }
}

function data(): string {
  const made = tempData()
  cleanups.push(() => made.cleanup())
  return made.dir
}

async function conversation(b: Booted): Promise<number> {
  const created = await b.client.call('conversation.create', {
    agent: { model: b.faux.model, cwd: process.cwd() },
  })
  return created['conversationId'] as number
}

type Change = { id: number; processId: string; status: string; exitCode?: number; tail?: string }
type Listed = {
  id: string
  pid: number
  command: string
  logPath: string
  status: string
  exitCode?: number
}

/** Have the model start `command` in the background; answers the first `process.changed` it sent. */
async function startInBackground(b: Booted, id: number, command: string): Promise<Change> {
  b.faux.faux.setResponses([
    fauxAssistantMessage(fauxToolCall('bash_background', { command }), { stopReason: 'toolUse' }),
    fauxAssistantMessage('started'),
  ])
  const seen = new Set(
    b.client.notifications
      .filter((n) => n.method === 'process.changed')
      .map((n) => n.params['processId']),
  )
  await b.client.call('conversation.submit', { id, input: 'start it' })
  const note = await b.client.waitFor(
    (n) =>
      n.method === 'process.changed' &&
      n.params['id'] === id &&
      n.params['status'] === 'running' &&
      !seen.has(n.params['processId']),
  )
  return note.params as unknown as Change
}

async function ended(client: Client, processId: string): Promise<Change> {
  const note = await client.waitFor(
    (n) =>
      n.method === 'process.changed' &&
      n.params['processId'] === processId &&
      n.params['status'] !== 'running',
  )
  return note.params as unknown as Change
}

async function listed(client: Client, id: number): Promise<Listed[]> {
  return (await client.call('process.list', { id }))['processes'] as Listed[]
}

async function records(client: Client, id: number): Promise<Record<string, unknown>[]> {
  const page = await client.call('conversation.entries', {
    id,
    query: { order: 'ascending' },
    limit: 200,
  })
  return (page['items'] as { kind: string; data?: Record<string, unknown> }[])
    .filter((entry) => entry.kind === 'droi.process')
    .map((entry) => entry.data!)
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

async function gone(pid: number, timeoutMs = 10_000): Promise<boolean> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    if (!alive(pid)) return true
    await Bun.sleep(25)
  }
  return false
}

describe.skipIf(!posix)('bash_background', () => {
  test('answers at once, is listed while it runs, and process.stop ends its whole group', async () => {
    const b = await boot(data())
    const id = await conversation(b)
    const started = await startInBackground(b, id, 'sleep 30 & sleep 30; echo never')
    expect(started).toMatchObject({ id, status: 'running', tail: '' })

    const result = await waitForToolResult(b.client, id)
    expect(result).toContain(started.processId)

    const [one] = await listed(b.client, id)
    expect(one).toMatchObject({
      id: started.processId,
      status: 'running',
      command: 'sleep 30 & sleep 30; echo never',
    })
    expect(typeof (one as unknown as { callId?: unknown }).callId).toBe('string')
    expect(alive(one!.pid)).toBe(true)
    expect(existsSync(one!.logPath)).toBe(true)

    expect(await b.client.call('process.stop', { id, processId: started.processId })).toEqual({
      status: 'stopped',
    })
    expect(await gone(one!.pid)).toBe(true)
    // The `sleep` the shell put in the background is in the same group, and ends with it.
    expect(await groupGone(one!.pid)).toBe(true)
    expect((await ended(b.client, started.processId)).status).toBe('stopped')
    expect((await listed(b.client, id))[0]).toMatchObject({ status: 'stopped' })
    expect((await records(b.client, id)).map((r) => r['status'])).toEqual(['running', 'stopped'])
    // Stopped already: nothing left to signal.
    expect(await b.client.call('process.stop', { id, processId: started.processId })).toEqual({
      status: 'gone',
    })
  }, 60_000)

  test('a command that ends says how, with the end of its output', async () => {
    const b = await boot(data())
    const id = await conversation(b)
    const ok = await startInBackground(b, id, 'echo built')
    const okEnd = await ended(b.client, ok.processId)
    expect(okEnd).toMatchObject({ status: 'exited', exitCode: 0 })
    expect(okEnd.tail).toContain('built')
    await waitForToolResult(b.client, id)

    const bad = await startInBackground(b, id, 'echo broken; exit 3')
    const badEnd = await ended(b.client, bad.processId)
    expect(badEnd).toMatchObject({ status: 'failed', exitCode: 3 })
    expect(badEnd.tail).toContain('broken')

    const byId = new Map((await listed(b.client, id)).map((p) => [p.id, p]))
    expect(byId.get(ok.processId)).toMatchObject({ status: 'exited', exitCode: 0 })
    expect(byId.get(bad.processId)).toMatchObject({ status: 'failed', exitCode: 3 })
    // The start is recorded before the end, however fast the command was.
    const statuses = (await records(b.client, id)).map((r) => `${r['processId']}:${r['status']}`)
    expect(statuses).toEqual([
      `${ok.processId}:running`,
      `${ok.processId}:done`,
      `${bad.processId}:running`,
      `${bad.processId}:failed`,
    ])
  }, 60_000)

  test("a relative cwd is the conversation working directory's", async () => {
    const b = await boot(data())
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash_background', { command: 'pwd', cwd: 'test' }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('started'),
    ])
    await b.client.call('conversation.submit', { id, input: 'start it' })
    const note = await b.client.waitFor(
      (n) => n.method === 'process.changed' && n.params['status'] === 'exited',
    )
    expect(String(note.params['tail']).trim()).toBe(`${process.cwd()}/test`)
  }, 60_000)

  test('unknown ids', async () => {
    const b = await boot(data())
    const id = await conversation(b)
    expect((await b.client.fails('process.stop', { id, processId: 'p-none' })).code).toBe(-32011)
    expect((await b.client.fails('process.list', { id: 9999 })).code).toBe(-32000)
    expect(await listed(b.client, id)).toEqual([])
  }, 60_000)

  test('a kernel that stops ends its background processes, and says so', async () => {
    const dir = data()
    const first = await boot(dir)
    const id = await conversation(first)
    const started = await startInBackground(first, id, 'sleep 30')
    const [one] = await listed(first.client, id)
    first.client.close()
    await first.kernel.stop()
    expect(await gone(one!.pid)).toBe(true)

    const second = await boot(dir)
    expect((await listed(second.client, id))[0]).toMatchObject({
      id: started.processId,
      status: 'stopped',
    })
    expect((await records(second.client, id)).map((r) => r['status'])).toEqual([
      'running',
      'stopped',
    ])
  }, 60_000)
})

describe.skipIf(!posix)('after a crash', () => {
  test('a process the dead kernel started is still listed, and still stopped by process.stop', async () => {
    const dir = data()
    const child = await startChild(dir, 'sleep 30')
    child.process.kill(9)
    await child.process.exited
    expect(alive(child.pid)).toBe(true)
    cleanups.push(() => {
      try {
        process.kill(-child.pid, 'SIGKILL')
      } catch {
        // Already gone.
      }
    })

    const b = await boot(dir)
    expect((await listed(b.client, child.conversationId))[0]).toMatchObject({
      id: child.processId,
      status: 'running',
    })
    expect(
      await b.client.call('process.stop', { id: child.conversationId, processId: child.processId }),
    ).toEqual({
      status: 'stopped',
    })
    expect(await gone(child.pid)).toBe(true)
    expect((await listed(b.client, child.conversationId))[0]).toMatchObject({ status: 'stopped' })
  }, 60_000)

  test('one that ended while no kernel ran reads as stopped', async () => {
    const dir = data()
    const child = await startChild(dir, 'sleep 30')
    child.process.kill(9)
    await child.process.exited
    process.kill(-child.pid, 'SIGKILL')
    expect(await gone(child.pid)).toBe(true)

    const b = await boot(dir)
    expect((await listed(b.client, child.conversationId))[0]).toMatchObject({ status: 'stopped' })
    expect(
      await b.client.call('process.stop', { id: child.conversationId, processId: child.processId }),
    ).toEqual({
      status: 'gone',
    })
  }, 60_000)
})

describe.skipIf(!posix)('whose process it is', () => {
  test("a fork does not take its parent's processes as its own", async () => {
    const b = await boot(data())
    const id = await conversation(b)
    const started = await startInBackground(b, id, 'sleep 30')
    await waitForToolResult(b.client, id)
    const page = await b.client.call('conversation.entries', {
      id,
      query: { order: 'descending' },
      limit: 1,
    })
    const last = (page['items'] as { id: number }[])[0]!.id
    const fork = (await b.client.call('conversation.fork', { id, at: last }))[
      'conversationId'
    ] as number
    expect(await listed(b.client, fork)).toEqual([])
    expect(
      (await b.client.fails('process.stop', { id: fork, processId: started.processId })).code,
    ).toBe(-32011)
    expect((await listed(b.client, id))[0]).toMatchObject({
      id: started.processId,
      status: 'running',
    })
  }, 60_000)

  test('a recorded pid that now names another program is never signalled', async () => {
    const dir = data()
    const first = await boot(dir)
    const id = await conversation(first)
    first.client.close()
    await first.kernel.stop()

    // A program of someone else's, in a group of its own, that the record claims as a process started before it.
    const stranger = spawn('sleep', ['30'], { detached: true, stdio: 'ignore' })
    const pid = stranger.pid!
    cleanups.push(() => {
      try {
        process.kill(-pid, 'SIGKILL')
      } catch {
        // Already gone.
      }
    })
    const db = new Database(join(dir, 'kernel.sqlite'))
    const record = {
      id: 'p-stranger',
      pid,
      command: 'pnpm dev',
      logPath: join(dir, 'p-stranger.log'),
      startedAt: Date.now() - 60_000,
      status: 'running',
    }
    db.run('INSERT INTO droi_processes (process_id, conversation_id, record) VALUES (?, ?, ?)', [
      record.id,
      id,
      JSON.stringify(record),
    ])
    db.close()

    const second = await boot(dir)
    expect(alive(pid)).toBe(true)
    expect((await records(second.client, id)).map((r) => r['status'])).toEqual(['stopped'])
    expect(await second.client.call('process.stop', { id, processId: 'p-stranger' })).toEqual({
      status: 'gone',
    })
    second.client.close()
    await second.kernel.stop()
    expect(alive(pid)).toBe(true)
  }, 60_000)
})
async function groupGone(pgid: number, timeoutMs = 10_000): Promise<boolean> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      process.kill(-pgid, 0)
    } catch {
      return true
    }
    await Bun.sleep(25)
  }
  return false
}

async function waitForToolResult(client: Client, id: number): Promise<string> {
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    const results = (page['items'] as { kind: string; model?: { content: unknown }[] }[]).filter(
      (entry) => entry.kind === 'pi.tool-result',
    )
    const last = results.at(-1)
    if (last !== undefined) return JSON.stringify(last.model)
    await Bun.sleep(20)
  }
  throw new Error('no tool result')
}

const childPath = new URL('./processes-crash-child.ts', import.meta.url).pathname

type Child = {
  process: ReturnType<typeof Bun.spawn>
  conversationId: number
  processId: string
  pid: number
}

async function startChild(dir: string, command: string): Promise<Child> {
  const child = Bun.spawn([process.execPath, 'run', childPath, dir, command], {
    stdout: 'pipe',
    stderr: 'pipe',
  })
  const reader = child.stdout.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value)
    const line = buffer.split('\n').find((candidate) => candidate.startsWith('{"port"'))
    if (line !== undefined)
      return { process: child, ...(JSON.parse(line) as Omit<Child, 'process'>) }
  }
  child.kill(9)
  throw new Error(
    `the child kernel never reported: ${buffer}\n${await new Response(child.stderr).text()}`,
  )
}
