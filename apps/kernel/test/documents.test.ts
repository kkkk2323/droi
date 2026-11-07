import { describe, expect, test } from 'bun:test'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { defineExtension, defineTool } from '@earendil-works/pi-durable'
import type { Extension, ToolExecutionApi } from '@earendil-works/pi-durable'
import { obj, str } from '@earendil-works/pi-schema'
import { Client, config, fauxCatalog, start, tempData } from './helpers.ts'
import { startKernel } from '../src/kernel.ts'
import { BACKGROUND_CONTEXT } from '@earendil-works/chord/context'

/**
 * The documents `protocol/README.md` says a client may read, pinned as they come off a real Kernel: `pi.inbox`
 * while a message waits, `pi.live` while a tool runs, and the compaction status. These are Pi's shapes, so a
 * field that changes upstream would otherwise reach M2 silently.
 */

/** The docs of one conversation, read through `conversation.open`. */
async function docs(client: Client, id: number): Promise<Record<string, Record<string, unknown>>> {
  const opened = (await client.call('conversation.open', {
    id,
    window: { min: 20, max: 100 },
  })) as {
    view: { docs: Record<string, Record<string, unknown>> }
  }
  return opened.view.docs
}

async function until(what: string, check: () => Promise<boolean>, ms = 60_000): Promise<void> {
  for (const end = Date.now() + ms; Date.now() < end;) {
    if (await check()) return
    await Bun.sleep(25)
  }
  throw new Error(`timed out waiting for ${what}`)
}

describe('the documents a client reads', () => {
  test('pi.inbox lists what waits for the run, with the submission id submit answered', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    faux.faux.setResponses([
      async () => {
        release()
        await Bun.sleep(4000)
        return fauxAssistantMessage('a slow answer')
      },
      fauxAssistantMessage('then this'),
    ])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const { conversationId: id } = (await client.call('conversation.create', {
        agent: { model: faux.model },
      })) as { conversationId: number }
      await client.call('conversation.submit', { id, input: 'first' })
      await held
      const queued = (await client.call('conversation.submit', {
        id,
        input: 'queued one',
        whenBusy: 'followUp',
      })) as { submissionId: number }
      const steer = (await client.call('conversation.submit', {
        id,
        input: 'steer one',
        whenBusy: 'steer',
      })) as { submissionId: number }

      const inbox = (await docs(client, id))['pi.inbox'] as {
        items: { id: number; mode: string; content: unknown }[]
      }
      expect(inbox.items.map((item) => [item.id, item.mode, item.content])).toEqual([
        [queued.submissionId, 'followUp', 'queued one'],
        [steer.submissionId, 'steer', 'steer one'],
      ])

      // Idle: the inbox is empty rather than absent.
      await until('both turns to finish', async () => {
        const now = (await docs(client, id))['pi.inbox'] as { items: unknown[] }
        const live = (await docs(client, id))['pi.live'] as { run?: unknown }
        return now.items.length === 0 && live?.run === undefined
      })
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('pi.live carries a running tool output, and drops it once the call settles', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    faux.faux.setResponses([
      fauxAssistantMessage(
        fauxToolCall('bash', { command: 'for i in 1 2 3 4 5 6; do echo line$i; sleep 0.4; done' }),
        { stopReason: 'toolUse' },
      ),
      fauxAssistantMessage('after the tool'),
    ])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const { conversationId: id } = (await client.call('conversation.create', {
        agent: { model: faux.model },
      })) as { conversationId: number }
      await client.call('conversation.submit', { id, input: 'run a loud tool' })

      let slot: Record<string, unknown> | undefined
      await until('a tool slot with output', async () => {
        const live = (await docs(client, id))['pi.live'] as {
          tools?: Record<string, unknown>[]
        }
        slot = live?.tools?.find(
          (each) => typeof each['output'] === 'string' && each['output'] !== '',
        )
        return slot !== undefined
      })
      expect(slot?.['status']).toBe('running')
      expect(String(slot?.['output'])).toContain('line1')
      expect(slot?.['entry']).toBeUndefined()

      // Once it settles the round moves on: the result entry is the record, the slot's output is gone.
      await until('the answer and an empty round', async () => {
        const page = (await client.call('conversation.entries', {
          id,
          query: { order: 'ascending' },
          limit: 100,
        })) as { items: { kind: string; model?: unknown }[] }
        const answered = page.items.some(
          (entry) =>
            entry.kind === 'pi.assistant' && JSON.stringify(entry.model).includes('after the tool'),
        )
        const live = (await docs(client, id))['pi.live'] as { tools?: unknown[] }
        return answered && (live?.tools ?? []).length === 0
      })
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('pi.live.compactions names the running compaction', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    const long = (n: number) => `answer ${n}: ` + 'lorem ipsum dolor sit amet '.repeat(2500)
    faux.faux.setResponses([
      fauxAssistantMessage(long(1)),
      fauxAssistantMessage(long(2)),
      fauxAssistantMessage(long(3)),
      async () => {
        await held
        return fauxAssistantMessage('the summary')
      },
    ])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const { conversationId: id } = (await client.call('conversation.create', {
        agent: { model: faux.model },
      })) as { conversationId: number }
      for (let turn = 1; turn <= 3; turn++) {
        await client.call('conversation.submit', { id, input: `turn ${turn}` })
        await Bun.sleep(300)
      }
      const compacted = (await client.call('conversation.compact', {
        id,
        instructions: 'keep paths',
      })) as {
        taskId: number
      }
      let statuses:
        | { taskId: number; trigger: string; blocking: boolean; attempt: number }[]
        | undefined
      await until('a compaction status', async () => {
        const live = (await docs(client, id))['pi.live'] as {
          compactions?: { taskId: number; trigger: string; blocking: boolean; attempt: number }[]
        }
        statuses = live?.compactions
        return (statuses ?? []).length > 0
      })
      expect(
        statuses?.map(({ taskId, trigger, blocking, attempt }) => ({
          taskId,
          trigger,
          blocking,
          attempt,
        })),
      ).toEqual([{ taskId: compacted.taskId, trigger: 'manual', blocking: false, attempt: 1 }])
      release()
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('conversation.create stores kind, and the agent change beats the top-level field', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const created = (await client.call('conversation.create', {
        kind: 'automation',
        instructions: 'from the top level',
        agent: { model: faux.model },
      })) as { conversationId: number }
      const agent = (await docs(client, created.conversationId))['pi.agent'] as {
        instructions?: string
      }
      expect(agent.instructions).toBe('from the top level')

      // `agent` wins over the top level, so a Host's default can be overridden field by field.
      const nested = (await client.call('conversation.create', {
        instructions: 'from the top level',
        agent: { model: faux.model, instructions: 'from the agent' },
      })) as { conversationId: number }
      const overridden = (await docs(client, nested.conversationId))['pi.agent'] as {
        instructions?: string
      }
      expect(overridden.instructions).toBe('from the agent')

      const listed = (await client.call('conversation.list', {
        kind: 'automation',
        limit: 20,
      })) as {
        items: { id: number; kind?: string }[]
      }
      expect(listed.items.map((item) => item.id)).toEqual([created.conversationId])
      expect(listed.items[0]?.kind).toBe('automation')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)
})

/**
 * A tool that calls another tool through `executeTool()`, which is the only thing that puts an entry in
 * `pi.live.nestedTools`. Its inner call pauses, so the slot is observable rather than gone before the first poll.
 */
function nestedTool(): Extension {
  return defineExtension({
    name: 'nested-probe',
    tools: [
      defineTool({
        name: 'nested',
        description: 'Echo a string through a nested call.',
        parameters: obj({ text: str() }, { open: true }),
        async execute(args: { text: string }, api: ToolExecutionApi) {
          const inner = await api.executeTool('slow-echo', { text: args.text }, BACKGROUND_CONTEXT)
          return {
            output: [{ type: 'text' as const, text: String(inner.structuredOutput ?? args.text) }],
          }
        },
      }),
      defineTool({
        name: 'slow-echo',
        description: 'Echo a string after a pause.',
        parameters: obj({ text: str() }, { open: true }),
        structuredOutputSchema: obj({ text: str() }, { open: true }),
        async execute(args: { text: string }) {
          await Bun.sleep(1500)
          return {
            output: [{ type: 'text' as const, text: args.text }],
            structuredOutput: { text: args.text },
          }
        },
      }),
    ],
  })
}
describe('the documents, continued', () => {
  test('a kind inside agent is not stored: kind belongs to the conversation', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      // A nested kind on its own is ignored, not applied.
      const nestedOnly = (await client.call('conversation.create', {
        agent: { model: faux.model, kind: 'automation' },
      })) as { conversationId: number }
      const automations = (await client.call('conversation.list', {
        kind: 'automation',
        limit: 50,
      })) as {
        items: { id: number }[]
      }
      expect(automations.items.map((item) => item.id)).not.toContain(nestedOnly.conversationId)

      // And it does not override a top-level kind.
      const both = (await client.call('conversation.create', {
        kind: 'chat',
        agent: { model: faux.model, kind: 'automation' },
      })) as { conversationId: number }
      const chats = (await client.call('conversation.list', { kind: 'chat', limit: 50 })) as {
        items: { id: number; kind?: string }[]
      }
      expect(chats.items.find((item) => item.id === both.conversationId)?.kind).toBe('chat')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('pi.live.nestedTools lists a call a running tool made, and drops it with that call', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('nested', { text: 'from the tool' }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('after the nested call'),
    ])
    const kernel = await start({ dir: data.dir, faux, extensions: [nestedTool()] })
    const client = new Client(kernel.port)
    try {
      const { conversationId: id } = (await client.call('conversation.create', {
        agent: { model: faux.model },
      })) as { conversationId: number }
      await client.call('conversation.submit', { id, input: 'make a nested call' })

      let slots: Record<string, unknown>[] | undefined
      await until('a nested slot', async () => {
        const live = (await docs(client, id))['pi.live'] as {
          nestedTools?: Record<string, unknown>[]
        }
        slots = live?.nestedTools
        return (slots ?? []).length > 0
      })
      const slot = slots![0]!
      // The fields the README promises, and the one that ties it to the call that made it.
      expect(Object.keys(slot)).toEqual(
        expect.arrayContaining([
          'callId',
          'name',
          'parentCallId',
          'parentTaskId',
          'taskId',
          'status',
        ]),
      )
      expect(slot['name']).toBe('slow-echo')
      expect(slot['status']).toBe('running')
      // The id is `<parentCallId>/<key>`: the call that made it, then the key the tool chose.
      expect(String(slot['callId']).startsWith(String(slot['parentCallId']) + '/')).toBe(true)
      expect(slot['taskId']).not.toBe(slot['parentTaskId'])

      await until('the answer', async () => {
        const page = (await client.call('conversation.entries', {
          id,
          query: { order: 'ascending' },
          limit: 100,
        })) as { items: { kind: string; model?: unknown }[] }
        return page.items.some(
          (entry) =>
            entry.kind === 'pi.assistant' &&
            JSON.stringify(entry.model).includes('after the nested call'),
        )
      })
      // Gone with the call that made it, not left behind as history.
      const live = (await docs(client, id))['pi.live'] as { nestedTools?: unknown[] }
      expect(live?.nestedTools ?? []).toEqual([])
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('a slot never reports done while it still holds output', async () => {
    // `clearProgress` runs as the slot is marked done, so the two never appear together: a client can trust
    // "output" to mean "still running".
    const data = tempData()
    const faux = fauxCatalog()
    faux.faux.setResponses([
      fauxAssistantMessage(
        fauxToolCall('bash', {
          command: 'for i in 1 2 3 4 5 6 7 8; do echo row$i; sleep 0.25; done',
        }),
        { stopReason: 'toolUse' },
      ),
      fauxAssistantMessage('after the rows'),
    ])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const { conversationId: id } = (await client.call('conversation.create', {
        agent: { model: faux.model },
      })) as { conversationId: number }
      await client.call('conversation.submit', { id, input: 'print rows' })
      let sawRunning = false
      let sawDone = false
      for (const end = Date.now() + 60_000; Date.now() < end;) {
        const live = (await docs(client, id))['pi.live'] as {
          tools?: { status?: string; output?: string }[]
        }
        for (const slot of live?.tools ?? []) {
          if (slot.status === 'running') {
            expect(slot.output === undefined || typeof slot.output === 'string').toBe(true)
            if (slot.output !== undefined) sawRunning = true
          }
          if (slot.status === 'done') {
            sawDone = true
            expect(slot.output).toBeUndefined()
          }
        }
        const page = (await client.call('conversation.entries', {
          id,
          query: { order: 'ascending' },
          limit: 100,
        })) as { items: { kind: string; model?: unknown }[] }
        if (
          page.items.some(
            (entry) =>
              entry.kind === 'pi.assistant' &&
              JSON.stringify(entry.model).includes('after the rows'),
          )
        ) {
          break
        }
      }
      expect(sawRunning).toBe(true)
      // The slot may be gone before a poll sees it; what may never happen is `done` with output, checked above.
      void sawDone
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 90_000)

  test('pi.live has no compactions while the conversation is idle', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    faux.faux.setResponses([fauxAssistantMessage('a short answer')])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const { conversationId: id } = (await client.call('conversation.create', {
        agent: { model: faux.model },
      })) as { conversationId: number }
      await client.call('conversation.submit', { id, input: 'one turn' })
      await until('the answer', async () => {
        const page = (await client.call('conversation.entries', {
          id,
          query: { order: 'ascending' },
          limit: 100,
        })) as { items: { kind: string }[] }
        return page.items.some((entry) => entry.kind === 'pi.assistant')
      })
      const live = (await docs(client, id))['pi.live'] as Record<string, unknown> | undefined
      // Absent, not an empty list: the README says so, and a client that tests `length` would see neither.
      expect(live === undefined || !('compactions' in live)).toBe(true)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)
})

describe('pi.live.generation.retry', () => {
  test('a failed attempt publishes its backoff, then the next attempt', async () => {
    // The one field that needs a provider that fails: `retry` is `{at, error}` while Pi waits to try again.
    let calls = 0
    const provider = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      async fetch(request) {
        if (!new URL(request.url).pathname.endsWith('/chat/completions')) {
          return new Response('no', { status: 404 })
        }
        calls++
        if (calls <= 2) {
          return Response.json(
            { error: { message: 'upstream exploded' } },
            { status: 500, headers: { 'retry-after-ms': '1200' } },
          )
        }
        const chunk = (delta: unknown, finish: string | null) =>
          `data: ${JSON.stringify({
            id: 'chatcmpl-retry',
            object: 'chat.completion.chunk',
            created: 0,
            model: 'retry-1',
            choices: [{ index: 0, delta, finish_reason: finish }],
          })}\n\n`
        return new Response(
          chunk({ role: 'assistant' }, null) +
            chunk({ content: 'an answer at last' }, null) +
            chunk({}, 'stop') +
            'data: [DONE]\n\n',
          { headers: { 'content-type': 'text/event-stream' } },
        )
      },
    })
    const data = tempData()
    const previous = process.env['FAKE_PROVIDER_KEY']
    process.env['FAKE_PROVIDER_KEY'] = 'fake-key'
    const kernel = await startKernel(
      config(data.dir, {
        providers: [
          {
            id: 'fake',
            baseUrl: `http://127.0.0.1:${provider.port}/v1`,
            api: 'openai-completions',
            apiKeyEnv: 'FAKE_PROVIDER_KEY',
            models: [{ id: 'retry-1', contextWindow: 200_000, maxTokens: 8_000, input: ['text'] }],
          },
        ],
      }),
      { token: 'test-token', noLock: true },
    )
    const client = new Client(kernel.port)
    try {
      const { conversationId: id } = (await client.call('conversation.create', {
        agent: { model: { provider: 'fake', modelId: 'retry-1' } },
      })) as { conversationId: number }
      await client.call('conversation.submit', { id, input: 'say hi' })
      const seen: { attempt: number; retry: { at: number; error: string } }[] = []
      for (const end = Date.now() + 30_000; Date.now() < end && seen.length < 2;) {
        const live = (await docs(client, id))['pi.live'] as {
          generation?: { attempt?: number; retry?: { at: number; error: string } }
        }
        const generation = live?.generation
        if (generation?.retry !== undefined && typeof generation.attempt === 'number') {
          const last = seen.at(-1)
          if (last === undefined || last.attempt !== generation.attempt) {
            seen.push({ attempt: generation.attempt, retry: generation.retry })
          }
        }
        await Bun.sleep(20)
      }
      expect(seen.map((entry) => entry.attempt)).toEqual([1, 2])
      for (const entry of seen) {
        expect(entry.retry.at).toBeGreaterThan(0)
        expect(entry.retry.error).toContain('500')
      }
      client.close()
    } finally {
      client.close()
      await kernel.stop()
      provider.stop(true)
      data.cleanup()
      if (previous === undefined) delete process.env['FAKE_PROVIDER_KEY']
      else process.env['FAKE_PROVIDER_KEY'] = previous
    }
  }, 60_000)
})
