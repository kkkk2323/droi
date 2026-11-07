import { describe, expect, test } from 'bun:test'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'

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
