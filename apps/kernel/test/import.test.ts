import { describe, expect, test } from 'bun:test'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'

/**
 * `conversation.import`: a transcript that already exists, written as if the kernel had written it — the shape
 * M2's history constructor and M5's droid importer both need (shared/interfaces/conversation-import.md).
 */

const user = (text: string, at: number) => ({
  kind: 'pi.user',
  model: [{ role: 'user', content: text, timestamp: at }],
})
const assistant = (
  text: string,
  at: number,
  usage?: Record<string, unknown>,
  toolCall?: unknown,
) => ({
  kind: 'pi.assistant',
  model: [
    {
      role: 'assistant',
      content:
        toolCall === undefined ? [{ type: 'text', text }] : [{ type: 'text', text }, toolCall],
      provider: 'fake',
      model: 'fake-1',
      ...(usage === undefined ? {} : { usage }),
      timestamp: at,
    },
  ],
})

async function kernelForTest() {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  return { kernel, client: new Client(kernel.port), data }
}

type Imported = { conversationId: number; entryIds: number[]; replayed?: boolean }

describe('conversation.import', () => {
  test('writes a transcript with its own timestamps, title, kind and usage', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const created = (await client.call('conversation.import', {
        key: 'droid-import:fixture:0',
        agent: { model: { provider: 'fake', modelId: 'fake-1' }, thinkingLevel: 'off' },
        cwd: '/Users/dev/acme-web',
        kind: 'chat',
        title: 'Fix the login race',
        createdAt: 1762480000000,
        entries: [
          user('the login race', 1762480000000),
          assistant('let me look', 1762480001000, {
            input: 1200,
            output: 180,
            cacheRead: 0,
            cacheWrite: 0,
            totalTokens: 1380,
            cost: { input: 0.0036, output: 0.0027, cacheRead: 0, cacheWrite: 0, total: 0.0063 },
          }),
          {
            kind: 'pi.tool-result',
            data: { diagnostics: [] },
            model: [
              {
                role: 'toolResult',
                toolCallId: 'call_1',
                toolName: 'read',
                content: [{ type: 'text', text: 'the file' }],
                isError: false,
                timestamp: 1762480002000,
              },
            ],
          },
        ],
      })) as Imported

      expect(created.entryIds).toHaveLength(3)
      expect(created.replayed).toBeUndefined()
      // Consecutive, so `entryIds` and `entries` line up.
      expect(created.entryIds[1]).toBe(created.entryIds[0]! + 1)
      expect(created.entryIds[2]).toBe(created.entryIds[1]! + 1)

      const page = (await client.call('conversation.entries', {
        id: created.conversationId,
        query: { order: 'ascending' },
        limit: 100,
      })) as { items: { id: number; kind: string; model?: { timestamp?: number }[] }[] }
      const written = page.items.filter((entry) => created.entryIds.includes(entry.id))
      expect(written.map((entry) => entry.id)).toEqual(created.entryIds)
      expect(written.map((entry) => entry.kind)).toEqual([
        'pi.user',
        'pi.assistant',
        'pi.tool-result',
      ])
      expect(written.map((entry) => entry.model?.[0]?.timestamp)).toEqual([
        1762480000000, 1762480001000, 1762480002000,
      ])

      const listed = (await client.call('conversation.list', { limit: 20 })) as {
        items: {
          id: number
          title?: string
          kind?: string
          workspace?: string
          lastActivityAt: number
        }[]
      }
      const row = listed.items.find((item) => item.id === created.conversationId)
      expect(row?.title).toBe('Fix the login race')
      expect(row?.kind).toBe('chat')
      expect(row?.workspace).toBe('/Users/dev/acme-web')
      // The newest message's time, not the import's.
      expect(row?.lastActivityAt).toBe(1762480002000)

      const usage = (await client.call('conversation.usage', { id: created.conversationId })) as {
        models: Record<string, { input: number; output: number; totalTokens: number }>
      }
      expect(usage.models['fake/fake-1']).toMatchObject({
        input: 1200,
        output: 180,
        totalTokens: 1380,
      })
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('appends a batch to an existing conversation, and each batch is one commit', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('first', 1762480000000)],
      })) as Imported
      const appended = (await client.call('conversation.import', {
        id: created.conversationId,
        entries: [assistant('second', 1762480005000), user('third', 1762480006000)],
      })) as Imported
      expect(appended.conversationId).toBe(created.conversationId)
      expect(appended.entryIds).toHaveLength(2)
      expect(appended.entryIds[0]).toBeGreaterThan(created.entryIds[0]!)

      const page = (await client.call('conversation.entries', {
        id: created.conversationId,
        query: { order: 'ascending' },
        limit: 100,
      })) as { items: { id: number; kind: string }[] }
      expect(
        page.items.filter((entry) => !entry.kind.startsWith('droi.')).map((entry) => entry.kind),
      ).toEqual(['pi.user', 'pi.assistant', 'pi.user'])

      // No run, no submission: an import is history, not a turn.
      const opened = (await client.call('conversation.open', {
        id: created.conversationId,
        window: { min: 20, max: 100 },
      })) as { view: { docs: Record<string, { run?: unknown }> } }
      expect(opened.view.docs['pi.live']?.run).toBeUndefined()
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('replaying a key writes nothing and answers what it wrote the first time', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const params = {
        key: 'droid-import:fixture:7',
        entries: [user('once', 1762480000000)],
      }
      const first = (await client.call('conversation.import', params)) as Imported
      const again = (await client.call('conversation.import', params)) as Imported
      expect(again.conversationId).toBe(first.conversationId)
      expect(again.entryIds).toEqual(first.entryIds)
      expect(again.replayed).toBe(true)

      const page = (await client.call('conversation.entries', {
        id: first.conversationId,
        query: { order: 'ascending' },
        limit: 100,
      })) as { items: { kind: string }[] }
      // One user entry and the kernel's own memo; the second call added nothing.
      expect(page.items.filter((entry) => entry.kind === 'pi.user')).toHaveLength(1)

      const appended = (await client.call('conversation.import', {
        id: first.conversationId,
        key: 'droid-import:fixture:7',
        entries: [user('twice', 1762480009000)],
      })) as Imported
      expect(appended.entryIds).toEqual(first.entryIds)
      expect(appended.replayed).toBe(true)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('refuses what it cannot write', async () => {
    const { kernel, client, data } = await kernelForTest()
    const fails = (method: string, params: Record<string, unknown>) =>
      client.fails(method, params) as Promise<{
        code: number
        data?: { issues?: { path: string }[] }
      }>
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('one', 1762480000000)],
      })) as Imported

      // A create-only field on an append: the conversation's identity is already decided.
      const identity = await fails('conversation.import', {
        id: created.conversationId,
        title: 'too late',
        entries: [],
      })
      expect(identity.code).toBe(-32602)
      expect(identity.data?.issues?.[0]?.path).toBe('title')

      // A message without a timestamp is not a message Pi can store.
      const stamp = await fails('conversation.import', {
        id: created.conversationId,
        entries: [{ kind: 'pi.user', model: [{ role: 'user', content: 'no time' }] }],
      })
      expect(stamp.code).toBe(-32602)
      expect(stamp.data?.issues?.[0]?.path).toBe('entries[0].model[0].timestamp')

      // The kernel's own bookkeeping kinds belong to the kernel.
      const meta = await fails('conversation.import', {
        id: created.conversationId,
        entries: [{ kind: 'droi.meta', data: { createdAt: 1 } }],
      })
      expect(meta.code).toBe(-32602)
      expect(meta.data?.issues?.[0]?.path).toBe('entries[0].kind')

      // A batch larger than the published cap is refused, not silently truncated.
      const tooMany = await fails('conversation.import', {
        id: created.conversationId,
        entries: Array.from({ length: 1001 }, (_, index) =>
          user(`m${index}`, 1762480000000 + index),
        ),
      })
      expect(tooMany.code).toBe(-32602)

      const missing = await fails('conversation.import', { id: 99999, entries: [] })
      expect(missing.code).toBe(-32000)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('a key reused for a different conversation is a conflict, not a silent answer', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      await client.call('conversation.import', {
        key: 'droid-import:same',
        kind: 'chat',
        entries: [user('one', 1762480000000)],
      })
      const clash = (await client.fails('conversation.import', {
        key: 'droid-import:same',
        kind: 'automation',
        entries: [user('other', 1762480000000)],
      })) as { code: number }
      expect(clash.code).toBe(-32013)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)
})
