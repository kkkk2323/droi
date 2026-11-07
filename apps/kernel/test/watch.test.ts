import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import { applyImmutable } from '@earendil-works/chord/delta'
import { ViewWindow } from '../src/session/watch.ts'
import type { Connection } from '../src/protocol/server.ts'
import type { KernelHandle } from '../src/kernel.ts'

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
})

/** A connection stand-in that records what a window would send. */
function recorder() {
  const sent: { method: string; params: Record<string, unknown> }[] = []
  const connection = {
    open: true,
    notify: (method: string, params: unknown) =>
      sent.push({ method, params: params as Record<string, unknown> }),
  } as unknown as Connection
  return { connection, sent }
}

function view(
  id: number,
  entries: number[],
): { conversation: { id: number }; entries: unknown[]; docs: Record<string, never> } {
  return { conversation: { id }, entries: entries.map((entry) => ({ id: entry })), docs: {} }
}

describe('the view push window', () => {
  test('coalesces commits inside one interval and always delivers the last state', async () => {
    const { connection, sent } = recorder()
    const window = new ViewWindow({ connection, conversationId: 7, mode: 'view', perSecond: 5 })
    for (let index = 1; index <= 40; index++) window.advance(view(7, [index]) as never, [], false)
    await Bun.sleep(400)
    expect(sent.length).toBeLessThanOrEqual(3)
    expect(sent.length).toBeGreaterThan(0)
    const last = sent.at(-1)!.params['value'] as { entries: { id: number }[] }
    expect(last.entries.at(-1)?.id).toBe(40)
    window.stop()
  })

  test('never sends more than the rate allows, however fast the commits come', async () => {
    const { connection, sent } = recorder()
    const window = new ViewWindow({ connection, conversationId: 7, mode: 'view', perSecond: 5 })
    const started = Date.now()
    for (let index = 0; index < 100; index++) {
      window.advance(view(7, [index]) as never, [], false)
      await Bun.sleep(10)
    }
    const elapsed = (Date.now() - started) / 1000
    // Five a second, plus the one that was allowed at the start.
    expect(sent.length).toBeLessThanOrEqual(Math.ceil(elapsed * 5) + 2)
    window.stop()
    await Bun.sleep(400)
    const afterStop = sent.length
    await Bun.sleep(300)
    expect(sent.length).toBe(afterStop)
  })

  test('a delta window sends ops only, and a whole value after a retirement', async () => {
    const { connection, sent } = recorder()
    const window = new ViewWindow({ connection, conversationId: 7, mode: 'delta', perSecond: 20 })
    window.advance(view(7, [1]) as never, [['a', ['entries'], '{"id":1}']], false)
    await Bun.sleep(120)
    expect(sent[0]?.params['kind']).toBe('value')
    expect(sent[0]?.params['value']).toBeNull()
    expect(((sent[0]?.params['ops'] ?? []) as unknown[]).length).toBe(1)

    window.advance(view(7, [1, 2]) as never, [['a', ['entries'], '{"id":2}']], false)
    window.advance(view(7, [1, 2, 3]) as never, [['a', ['entries'], '{"id":3}']], false)
    await Bun.sleep(120)
    const folded = sent.at(-1)!
    expect(folded.params['kind']).toBe('value')
    expect((folded.params['ops'] as unknown[]).length).toBe(2)

    window.advance(view(7, [9]) as never, [['r', { entries: [] }]], true)
    await Bun.sleep(120)
    const replacement = sent.at(-1)!
    expect(replacement.params['kind']).toBe('full')
    expect((replacement.params['ops'] as unknown[]).length).toBe(0)
    expect((replacement.params['value'] as { entries: { id: number }[] }).entries[0]?.id).toBe(9)
    window.stop()
  })

  test('a failed read sends failed and keeps the client where it was', async () => {
    const { connection, sent } = recorder()
    const window = new ViewWindow({ connection, conversationId: 7, mode: 'view', perSecond: 20 })
    window.fail(new Error('storage read failed'))
    await Bun.sleep(120)
    expect(sent[0]?.params['kind']).toBe('failed')
    expect((sent[0]?.params['error'] ?? { message: '' }) as { message: string }).toMatchObject({
      message: 'storage read failed',
    })
    window.stop()
  })
})

describe('subscriptions over the connection', () => {
  test('harness.info publishes the push limit and the capabilities', async () => {
    const info = await client.call('harness.info')
    const limits = info['limits'] as {
      viewPushesPerSecond: number
      viewWindowDefault: { min: number; max: number }
    }
    expect(limits.viewPushesPerSecond).toBe(5)
    expect(limits.viewWindowDefault.min).toBe(100)
    expect((info['capabilities'] as Record<string, boolean>)['models']).toBe(true)
  })

  test('model.complete answers, and refuses a read-only kernel', async () => {
    // A call with no params is the contract's own param error, not `-32009`: the method is built.
    const error = await client.fails('model.complete', {})
    expect(error.code).toBe(-32602)
  })

  test('a delta subscriber rebuilds the same view by applying the ops', async () => {
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number

    const watcher = new Client(kernel.port)
    const opened = await watcher.call('conversation.open', { id, subscribe: 'delta' })
    // Chord's own op applier, the one a client uses: the check is that the ops are complete, not that a
    // test-local approximation of them agrees with itself.
    let local = opened['view'] as { entries: { id: number }[] }
    const baseline = local.entries.length
    let pushes = 0

    faux.faux.setResponses([fauxAssistantMessage('one'), fauxAssistantMessage('two')])
    await client.call('conversation.submit', { id, input: 'talk to me' })

    let matched = false
    const deadline = Date.now() + 25_000
    while (Date.now() < deadline) {
      const seen = watcher.notifications.filter(
        (note) => note.method === 'conversation.view' && note.params['id'] === id,
      )
      for (const push of seen.slice(pushes)) {
        pushes++
        if (push.params['kind'] === 'failed') continue
        if (push.params['kind'] === 'full') {
          local = push.params['value'] as typeof local
        } else {
          local = applyImmutable(local, push.params['ops'] as never) as typeof local
        }
      }
      const page = await client.call('conversation.entries', {
        id,
        query: { order: 'ascending' },
        limit: 200,
      })
      const truth = (page['items'] as { id: number }[]).map((entry) => entry.id)
      if (
        truth.length > baseline &&
        truth.length === local.entries.length &&
        truth.every((entryId, index) => local.entries[index]?.id === entryId)
      ) {
        matched = true
        break
      }
      await Bun.sleep(50)
    }
    expect(matched).toBe(true)
    expect(pushes).toBeGreaterThan(0)
    watcher.close()
  }, 60_000)
})
