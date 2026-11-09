import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { spawn } from 'node:child_process'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { allEntries, Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import { fingerprintOf } from '../src/session/service.ts'
import { parseArgs } from '../src/config.ts'
import type { KernelHandle } from '../src/kernel.ts'

let data: ReturnType<typeof tempData>
let faux: Faux
let kernel: KernelHandle | undefined
let client: Client | undefined

beforeEach(() => {
  data = tempData()
  faux = fauxCatalog()
})

afterEach(async () => {
  client?.close()
  client = undefined
  await kernel?.stop()
  kernel = undefined
  data.cleanup()
})

function currentPort(): number {
  if (kernel === undefined) throw new Error('no kernel yet: call open() first')
  return kernel.port
}

async function open(overrides: Parameters<typeof start>[0]['overrides'] = {}): Promise<Client> {
  kernel ??= await start({ dir: data.dir, faux, overrides })
  client ??= new Client(currentPort())
  return client
}

// 1 — a crash between Pi's commit and the index flush must not lose the conversation.
describe('the index survives a crash', () => {
  test('SIGKILL between the commit and the index flush keeps the conversation in the list and search', async () => {
    // A real process, killed without warning: a graceful stop flushes the index first and would pass even
    // if the recovery did nothing.
    const child = spawn(
      process.execPath,
      [new URL('../src/main.ts', import.meta.url).pathname, '--data', data.dir],
      { env: { ...process.env, DROI_KERNEL_TOKEN: 'crash-index-test' } },
    )
    let log = ''
    child.stderr.on('data', (chunk: Buffer) => {
      log += chunk.toString()
    })
    const exited = new Promise<number | null>((resolve) =>
      child.on('exit', (code) => resolve(code)),
    )
    try {
      const port = await new Promise<number>((resolve, reject) => {
        let out = ''
        const timer = setTimeout(() => reject(new Error(`no handshake: ${log}`)), 20_000)
        child.stdout.on('data', (chunk: Buffer) => {
          out += chunk.toString()
          const line = out.split('\n').find((candidate) => candidate.startsWith('{'))
          if (line === undefined) return
          clearTimeout(timer)
          resolve((JSON.parse(line) as { port: number }).port)
        })
      })
      const live = new Client(port, 'crash-index-test')
      const created = await live.call('conversation.create', {
        requestId: 'index-crash-key',
        kind: 'chat',
        cwd: '/tmp/index-crash-cwd',
      })
      const id = created['conversationId'] as number
      // The reply means Pi stored the conversation and its `droi.meta` entry. The index flush is behind a
      // 150 ms timer, so a kill right here is exactly the window.
      child.kill('SIGKILL')
      await exited
      live.close()

      kernel = await start({ dir: data.dir, faux })
      client = new Client(currentPort())
      const list = await client.call('conversation.list', { limit: 50 })
      const items = list['items'] as { id: number; kind: string; workspace: string }[]
      expect(items.map((item) => item.id)).toContain(id)
      // The row is rebuilt from the conversation's own `droi.meta` entry, so what it says about the
      // conversation survives too.
      const row = items.find((item) => item.id === id)!
      expect({ kind: row.kind, workspace: row.workspace }).toEqual({
        kind: 'chat',
        workspace: '/tmp/index-crash-cwd',
      })
      const found = await client.call('conversation.list', { query: 'index-crash' })
      expect(Array.isArray(found['items'])).toBe(true)
      // And a retry of the same create answers the same conversation instead of making another.
      const retry = await client.call('conversation.create', {
        requestId: 'index-crash-key',
        kind: 'chat',
        cwd: '/tmp/index-crash-cwd',
      })
      expect(retry['conversationId']).toBe(id)
    } finally {
      child.kill('SIGKILL')
    }
  }, 60_000)

  test('a restart re-reads only what that conversation’s watermark did not cover', async () => {
    kernel = await start({ dir: data.dir, faux })
    const live = new Client(currentPort())
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    faux.faux.setResponses([fauxAssistantMessage('indexed once')])
    await live.call('conversation.submit', { id, input: 'first message' })
    await Bun.sleep(300)
    // Per conversation, not one global number: the watermark is what this conversation's index covers.
    const watermark = kernel.indexer.watermark(id)
    expect(watermark).toBeGreaterThan(0)
    live.close()
    await kernel.stop()
    kernel = undefined

    kernel = await start({ dir: data.dir, faux })
    // Everything before the watermark is already indexed, so the catch-up re-reads nothing for it.
    expect(kernel.indexer.watermark(id)).toBe(watermark)
  }, 60_000)
})

// 2 — --read-only
describe('a read-only kernel', () => {
  test('does not resume an unfinished run, and refuses index.update', async () => {
    // A conversation with a live task, left behind by a kernel that ended mid-run.
    kernel = await start({ dir: data.dir, faux })
    const live = new Client(currentPort())
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    let calls = 0
    faux.faux.setResponses([
      async (_context: unknown, options?: { signal?: AbortSignal }) => {
        calls++
        // Hangs like a slow provider, and gives up when the run is aborted or the kernel closes — a
        // promise that never settles would keep the test's own shutdown waiting.
        return new Promise<never>((_, reject) => {
          const timer = setTimeout(() => reject(new Error('the run was left running')), 20_000)
          options?.signal?.addEventListener('abort', () => {
            clearTimeout(timer)
            reject(new Error('aborted'))
          })
        })
      },
    ])
    await live.call('conversation.submit', { id, input: 'hold the floor' })
    await live.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] != null,
      10_000,
    )
    live.close()
    await kernel.stop()
    kernel = undefined
    const before = calls

    kernel = await start({ dir: data.dir, faux, overrides: { readOnly: true } })
    const viewer = new Client(currentPort())
    expect((await viewer.call('harness.info'))['version']).toBe('kernel-1.5.0-1')
    await Bun.sleep(500)
    // Not resumed: the provider was never asked again.
    expect(calls).toBe(before)
    const refused = await viewer.fails('index.update', { id, title: 'renamed by a viewer' })
    expect(refused.code).toBe(-32008)
    const rename = await viewer.fails('conversation.create', {})
    expect(rename.code).toBe(-32008)
    viewer.close()
  }, 60_000)
})

// 4 — create with a top-level cwd/instructions
describe('conversation.create', () => {
  test('applies a top-level cwd and instructions beside the agent', async () => {
    const live = await open()
    const created = await live.call('conversation.create', {
      agent: { model: faux.model },
      cwd: '/tmp/droi-create-cwd',
      instructions: 'Answer in one line.',
      kind: 'chat',
    })
    const view = created['view'] as {
      docs: Record<string, { cwd?: string; instructions?: string; model?: unknown }>
    }
    const agent = view.docs['pi.agent']!
    expect(agent.cwd).toBe('/tmp/droi-create-cwd')
    expect(agent.instructions).toBe('Answer in one line.')
    expect(agent.model).toBeDefined()
    // The index records the same cwd as the workspace, so filtering by it agrees with where tools run.
    const listed = await live.call('conversation.list', { workspace: '/tmp/droi-create-cwd' })
    expect((listed['items'] as { id: number }[]).map((item) => item.id)).toContain(
      created['conversationId'] as number,
    )
  })

  test('uses the config default agent when the request names no model', async () => {
    kernel = await start({
      dir: data.dir,
      faux,
      overrides: { agent: { provider: faux.model.provider, modelId: faux.model.modelId } },
    })
    client = new Client(currentPort())
    const created = await client.call('conversation.create', {})
    const view = created['view'] as { docs: Record<string, { model?: { modelId?: string } }> }
    expect(view.docs['pi.agent']?.model?.modelId).toBe(faux.model.modelId)
    // And a run works without a model in the request: a missing default is `no_model`, no provider call.
    faux.faux.setResponses([fauxAssistantMessage('the default model answered')])
    await client.call('conversation.submit', {
      id: created['conversationId'] as number,
      input: 'hello',
    })
    const entries = await allEntries(client, created['conversationId'] as number)
    const answer = entries.filter((entry) => entry.kind === 'pi.assistant').at(-1)
    expect(JSON.stringify(answer?.model)).toContain('the default model answered')
  })

  test('a request agent overrides the config default, field by field', async () => {
    kernel = await start({
      dir: data.dir,
      faux,
      overrides: {
        agent: {
          provider: faux.model.provider,
          modelId: faux.model.modelId,
          instructions: 'from config',
        },
      },
    })
    client = new Client(currentPort())
    const created = await client.call('conversation.create', { instructions: 'from the request' })
    const view = created['view'] as { docs: Record<string, { instructions?: string }> }
    expect(view.docs['pi.agent']?.instructions).toBe('from the request')
  })
})

// 5 — requestId fingerprints
describe('requestId', () => {
  test('a repeat with the same params returns the first result, a repeat with others is -32013', async () => {
    const live = await open()
    const first = await live.call('conversation.create', {
      requestId: 'create-key',
      kind: 'chat',
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const again = await live.call('conversation.create', {
      requestId: 'create-key',
      kind: 'chat',
      agent: { cwd: process.cwd(), model: faux.model },
    })
    expect(again['conversationId']).toBe(first['conversationId'])
    const conflictCreate = await live.fails('conversation.create', {
      requestId: 'create-key',
      kind: 'memory',
      agent: { model: faux.model, cwd: process.cwd() },
    })
    expect(conflictCreate.code).toBe(-32013)

    const id = first['conversationId'] as number
    faux.faux.setResponses([fauxAssistantMessage('one')])
    const submit = await live.call('conversation.submit', {
      id,
      input: 'hello',
      requestId: 'submit-key',
    })
    const same = await live.call('conversation.submit', {
      id,
      input: 'hello',
      requestId: 'submit-key',
    })
    expect(same['submissionId']).toBe(submit['submissionId'])
    const conflictSubmit = await live.fails('conversation.submit', {
      id,
      input: 'something else',
      requestId: 'submit-key',
    })
    expect(conflictSubmit.code).toBe(-32013)
  })

  test('a create conflict compares the cwd and the tools, not just the kind', async () => {
    const live = await open()
    await live.call('conversation.create', {
      requestId: 'cwd-conflict-key',
      kind: 'chat',
      agent: { model: faux.model, cwd: '/tmp/one' },
    })
    // Same kind, different working directory: a different conversation, so a conflict.
    const byCwd = await live.fails('conversation.create', {
      requestId: 'cwd-conflict-key',
      kind: 'chat',
      agent: { model: faux.model, cwd: '/tmp/two' },
    })
    expect(byCwd.code).toBe(-32013)
    await live.call('conversation.create', {
      requestId: 'tools-conflict-key',
      agent: { model: faux.model, modelTools: [] },
    })
    const byTools = await live.fails('conversation.create', {
      requestId: 'tools-conflict-key',
      agent: { model: faux.model, modelTools: ['read'] },
    })
    expect(byTools.code).toBe(-32013)
  })

  test('the fingerprint ignores key order', () => {
    expect(fingerprintOf({ a: 1, b: [2, 3] })).toBe(fingerprintOf({ b: [2, 3], a: 1 }))
    expect(fingerprintOf({ a: 1 })).not.toBe(fingerprintOf({ a: 2 }))
  })
})

// 6 — unwatch must not stop the busy broadcast
describe('unwatch', () => {
  test('unwatching DURING a run still lets another connection hear that it ended', async () => {
    const live = await open()
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    const watcher = new Client(currentPort())
    await watcher.opened
    await live.call('conversation.open', { id })

    // The run must be in flight when the unwatch happens: unwatching before it starts would pass even if
    // the unwatch destroyed the observation, because a new run would create one again.
    faux.faux.setResponses([
      async () => {
        await Bun.sleep(400)
        return fauxAssistantMessage('a slow answer')
      },
    ])
    await live.call('conversation.submit', { id, input: 'take your time' })
    await watcher.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] != null,
      15_000,
    )
    // Forget everything so far: a `run: null` from before this run started would satisfy the wait below
    // without the unwatch having any effect at all, which is how the first version of this test passed.
    watcher.notifications.length = 0
    await live.call('conversation.unwatch', { id })
    const idle = await watcher.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] === null,
      15_000,
    )
    expect(idle.params['run']).toBeNull()
    watcher.close()
  }, 60_000)

  test('an idle conversation nobody watches gives its observation back', async () => {
    const live = await open()
    const ids: number[] = []
    for (let index = 0; index < 5; index++) {
      const created = await live.call('conversation.create', {
        agent: { model: faux.model, cwd: process.cwd() },
      })
      ids.push(created['conversationId'] as number)
    }
    for (const id of ids) await live.call('conversation.open', { id })
    expect(kernel?.kernel.liveObservations).toBe(ids.length)
    for (const id of ids) await live.call('conversation.unwatch', { id })
    // Every one is idle and unwatched, so nothing is observed any more.
    expect(kernel?.kernel.liveObservations).toBe(0)
  })

  test('one of two viewers unwatching keeps the observation the other still needs', async () => {
    const live = await open()
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    const other = new Client(currentPort())
    await other.call('conversation.open', { id })
    await live.call('conversation.open', { id })
    await live.call('conversation.unwatch', { id })
    // The other connection still shows it, so its observation must stay.
    expect(kernel?.kernel.liveObservations).toBe(1)
    await other.call('conversation.unwatch', { id })
    expect(kernel?.kernel.liveObservations).toBe(0)
    other.close()
  })

  test('opening twice on one connection is one viewer, not two', async () => {
    const live = await open()
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    await live.call('conversation.open', { id })
    await live.call('conversation.open', { id, window: { min: 50, max: 100 } })
    expect(kernel?.kernel.liveObservations).toBe(1)
    await live.call('conversation.unwatch', { id })
    expect(kernel?.kernel.liveObservations).toBe(0)
  })
})

// 7 — models match the fixture
describe('harness.info', () => {
  test('publishes the model fields the fixture names', async () => {
    kernel = await start({
      dir: data.dir,
      faux,
      overrides: {
        providers: [
          {
            id: 'fake',
            api: 'openai-completions',
            baseUrl: 'http://127.0.0.1:1/v1',
            models: [
              {
                id: 'fake-1',
                contextWindow: 200_000,
                maxTokens: 8_000,
                input: ['text', 'image'],
                thinking: ['off', 'low', 'high'],
              },
            ],
          },
        ],
      },
    })
    client = new Client(currentPort())
    const info = await client.call('harness.info')
    const models = info['models'] as Record<string, unknown>[]
    expect(models[0]).toMatchObject({
      provider: 'fake',
      id: 'fake-1',
      contextWindow: 200_000,
      maxOutput: 8_000,
      input: ['text', 'image'],
      thinking: ['off', 'low', 'high'],
    })
    // The fixture is the contract: the live answer has exactly its keys.
    const fixture = (await Bun.file(
      new URL('../protocol/fixtures/methods/harness.info.json', import.meta.url).pathname,
    ).json()) as { response: { result: { models: Record<string, unknown>[] } } }
    expect(Object.keys(models[0]!).sort()).toEqual(
      Object.keys(fixture.response.result.models[0]!).sort(),
    )
  })
})

// 8 — task.changed and index.changed happen on their own
describe('notifications that arise from work', () => {
  test('a new conversation, its title, and its compaction each send one', async () => {
    const live = await open()
    const watcher = new Client(currentPort())
    await watcher.opened
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    faux.faux.setResponses([
      fauxAssistantMessage('first'),
      fauxAssistantMessage('second'),
      fauxAssistantMessage('the compaction summary'),
    ])
    await live.call('conversation.submit', { id, input: 'name this conversation please' })
    await live.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] === null,
      15_000,
    )
    const changed = await watcher.waitFor(
      (note) => note.method === 'index.changed' && note.params['id'] === id,
      15_000,
    )
    expect(String(changed.params['title'] ?? '')).toContain('name this conversation')

    faux.faux.appendResponses([fauxAssistantMessage('the compaction summary')])
    await live.call('conversation.compact', { id })
    // A compaction is a task of its own: it appears when it starts and ends when it finishes.
    const task = await watcher.waitFor(
      (note) => note.method === 'task.changed' && note.params['kind'] === 'compaction',
      20_000,
    )
    expect(task.params['conversationId']).toBe(id)
    const done = await watcher.waitFor(
      (note) =>
        note.method === 'task.changed' &&
        note.params['kind'] === 'compaction' &&
        note.params['status'] === 'terminal',
      20_000,
    )
    expect(done.params['taskId']).toBe(task.params['taskId'])
    watcher.close()
  }, 60_000)
})

// 9 — the list cursor
describe('conversation.list paging', () => {
  test('pages with {"before": id} and returns every conversation once', async () => {
    const live = await open()
    for (let index = 0; index < 5; index++) {
      await live.call('conversation.create', {
        agent: { model: faux.model, cwd: process.cwd() },
        requestId: `page-${index}`,
      })
    }
    const seen: number[] = []
    let cursor: unknown = undefined
    for (let page = 0; page < 10; page++) {
      const params: Record<string, unknown> = { limit: 2 }
      if (cursor !== undefined) params['cursor'] = cursor
      const answer = await live.call('conversation.list', params)
      seen.push(...(answer['items'] as { id: number }[]).map((item) => item.id))
      cursor = answer['cursor']
      if (cursor === undefined) break
      expect(cursor).toEqual({ before: seen.at(-1) })
    }
    expect(seen.length).toBe(5)
    expect(new Set(seen).size).toBe(5)
    // A cursor that is not the contract's shape is an invalid request, not an internal error.
    const bad = await live.fails('conversation.list', { cursor: 'nope' })
    expect(bad.code).toBe(-32602)
  })
})

// 10 — validation reaches the client as -32602
describe('params are validated before Pi sees them', () => {
  test('fork without at is -32602 on its own, and so is a window of zero', async () => {
    const live = await open()
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    // One case per request: a combined loop that stops at the first failure cannot say which check failed.
    const missingAt = await live.fails('conversation.fork', { id })
    expect(missingAt.code).toBe(-32602)
    expect((missingAt.data as { issues: { path: string }[] }).issues[0]?.path).toBe('at')
    const zeroWindow = await live.fails('conversation.open', { id, window: { min: 0 } })
    expect(zeroWindow.code).toBe(-32602)
    expect((zeroWindow.data as { issues: { path: string }[] }).issues[0]?.path).toBe('window.min')
    const badOrder = await live.fails('conversation.entries', { id, query: { order: 'sideways' } })
    expect(badOrder.code).toBe(-32602)
    const noInput = await live.fails('conversation.submit', { id })
    expect(noInput.code).toBe(-32602)
    const badBusy = await live.fails('conversation.submit', {
      id,
      input: 'hi',
      whenBusy: 'whenever',
    })
    expect(badBusy.code).toBe(-32602)
  })

  test('every named parameter check reports its own path', async () => {
    const live = await open()
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    for (const [method, params, path] of [
      ['conversation.fork', { id }, 'at'],
      ['conversation.open', { id, window: { min: 0 } }, 'window.min'],
      ['conversation.entries', { id, query: { order: 'sideways' } }, 'query.order'],
      ['conversation.submit', { id }, 'input'],
      ['conversation.submit', { id, input: 'hi', whenBusy: 'whenever' }, 'whenBusy'],
    ] as const) {
      const error = await live.fails(method, params as never)
      expect({ method, code: error.code }).toEqual({ method, code: -32602 })
      const body = error.data as { issues: { path: string }[] }
      expect(body.issues.map((issue) => issue.path)).toContain(path)
    }
  })

  test('fork at an entry of another conversation is -32602', async () => {
    const live = await open()
    const a = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const b = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    faux.faux.setResponses([fauxAssistantMessage('in a')])
    await live.call('conversation.submit', { id: a['conversationId'] as number, input: 'hello' })
    const entries = await allEntries(live, a['conversationId'] as number)
    const error = await live.fails('conversation.fork', {
      id: b['conversationId'] as number,
      at: entries[0]!.id,
    })
    expect(error.code).toBe(-32602)
  })

  test('submission.abort names settled and placed states', async () => {
    const live = await open()
    const created = await live.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    faux.faux.setResponses([fauxAssistantMessage('answered')])
    const submitted = await live.call('conversation.submit', { id, input: 'hello' })
    await live.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] === null,
      15_000,
    )
    const settled = await live.fails('submission.abort', {
      id,
      submissionId: submitted['submissionId'] as number,
    })
    expect(settled.code).toBe(-32004)
    const unknown = await live.fails('submission.abort', { id, submissionId: 999_999 })
    expect(unknown.code).toBe(-32003)

    // A submission that is in the transcript but still running is "already placed", its own code.
    const slow = new Client(currentPort())
    faux.faux.setResponses([
      async () => {
        await Bun.sleep(500)
        return fauxAssistantMessage('still running')
      },
    ])
    const running = await slow.call('conversation.submit', { id, input: 'a second turn' })
    const placed = await slow.fails('submission.abort', {
      id,
      submissionId: running['submissionId'] as number,
    })
    expect(placed.code).toBe(-32005)
    slow.close()
  }, 60_000)
})

// 17 — loopback only
describe('the interface', () => {
  test('refuses a host that is not loopback', () => {
    expect(() => parseArgs(['--data', '/tmp/x', '--host', '0.0.0.0'])).toThrow('loopback')
    expect(() => parseArgs(['--data', '/tmp/x', '--host', '192.168.1.10'])).toThrow('loopback')
    expect(parseArgs(['--data', '/tmp/x', '--host', '127.0.0.1']).host).toBe('127.0.0.1')
    expect(parseArgs(['--data', '/tmp/x']).host).toBe('127.0.0.1')
  })
})

// 3 — a failed Session
describe('a failed Session', () => {
  test('warns kernel_failed, answers -32010 to every method, and exits exactly 3', async () => {
    const child = spawn(process.execPath, [
      new URL('./failed-child.ts', import.meta.url).pathname,
      data.dir,
    ])
    let log = ''
    child.stderr.on('data', (chunk: Buffer) => {
      log += chunk.toString()
    })
    const exited = new Promise<number | null>((resolve) =>
      child.on('exit', (code) => resolve(code)),
    )
    try {
      const handshake = await new Promise<{ port: number }>((resolve, reject) => {
        let buffer = ''
        const timer = setTimeout(() => reject(new Error(`no handshake: ${log}`)), 30_000)
        child.stdout.on('data', (chunk: Buffer) => {
          buffer += chunk.toString()
          const line = buffer.split('\n').find((candidate) => candidate.startsWith('{'))
          if (line === undefined) return
          clearTimeout(timer)
          resolve(JSON.parse(line) as { port: number })
        })
      })
      const watcher = new Client(handshake.port)
      await watcher.opened
      // The child breaks its own storage once it is running.
      await watcher.waitFor(
        (note) => note.method === 'warning' && note.params['kind'] === 'kernel_failed',
        30_000,
      )
      // Every method answers -32010 afterwards, reads included: a kernel that cannot write is not healthy.
      for (const method of ['harness.info', 'conversation.list', 'conversation.create']) {
        const refused = await watcher.fails(
          method,
          method === 'conversation.create' ? {} : undefined,
        )
        expect({ method, code: refused.code }).toEqual({ method, code: -32010 })
        expect((refused.data as { reason?: string })?.reason).toBeDefined()
      }
      watcher.close()
      // Exactly 3, the status the Supervisor reads to replace the process.
      expect(await exited).toBe(3)
    } finally {
      child.kill('SIGKILL')
    }
  }, 90_000)
})
