import { describe, expect, test } from 'bun:test'
import { Database } from 'bun:sqlite'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
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

async function until(what: string, check: () => Promise<boolean>, ms = 60_000): Promise<void> {
  for (const end = Date.now() + ms; Date.now() < end;) {
    if (await check()) return
    await Bun.sleep(25)
  }
  throw new Error(`timed out waiting for ${what}`)
}

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

      // The same key on an append is a different request, not a replay of the create: it is refused rather
      // than answered with the create's ids (the round-1 rule; the append path has its own test).
      const reused = (await client.fails('conversation.import', {
        id: first.conversationId,
        key: 'droid-import:fixture:7',
        entries: [user('twice', 1762480009000)],
      })) as { code: number }
      expect(reused.code).toBe(-32013)
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

describe('conversation.import, round 1', () => {
  test('a create key survives a crash before its cache, and a replay adds nothing', async () => {
    // The identity travels in the same commit as Pi's conversation — the `droi.import` memo — and the table is
    // a cache rebuilt from the transcripts. So a retry after a crash converges instead of importing a copy.
    const data = tempData()
    const params = {
      key: 'crash-window',
      createdAt: 100,
      entries: [user('crash-window-needle', 1000)],
    }
    let kernel = await start({ dir: data.dir, faux: fauxCatalog() })
    let client = new Client(kernel.port)
    try {
      const first = (await client.call('conversation.import', params)) as Imported
      client.close()
      await kernel.stop()

      // What a crash before the cache is written leaves: a table missing the row the memo records.
      const db = new Database(join(data.dir, 'kernel.sqlite'))
      db.run('DELETE FROM droi_import_keys')
      db.close()

      kernel = await start({ dir: data.dir, faux: fauxCatalog() })
      client = new Client(kernel.port)
      const replay = (await client.call('conversation.import', params)) as Imported
      expect(replay.conversationId).toBe(first.conversationId)
      expect(replay.entryIds).toEqual(first.entryIds)
      expect(replay.replayed).toBe(true)
      const listed = (await client.call('conversation.list', { query: 'crash-window-needle' })) as {
        items: { id: number; createdAt: number; lastActivityAt: number }[]
      }
      expect(listed.items.map((item) => item.id)).toEqual([first.conversationId])
      // The row the catch-up rebuilt keeps the history's own clock, not the rebuild's time.
      expect(listed.items[0]?.createdAt).toBe(100)
      expect(listed.items[0]?.lastActivityAt).toBe(1000)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('the same key with other entries is a conflict, on both paths', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const created = (await client.call('conversation.import', {
        key: 'payload-key',
        createdAt: 100,
        entries: [user('payload-one', 1000)],
      })) as Imported
      const replaced = (await client.fails('conversation.import', {
        key: 'payload-key',
        createdAt: 100,
        entries: [user('payload-REPLACED', 1000)],
      })) as { code: number }
      expect(replaced.code).toBe(-32013)

      const appended = (await client.call('conversation.import', {
        id: created.conversationId,
        key: 'append-key',
        entries: [user('append-one', 2000)],
      })) as Imported
      expect(appended.entryIds).toHaveLength(1)

      const changed = (await client.fails('conversation.import', {
        id: created.conversationId,
        key: 'append-key',
        entries: [user('append-REPLACED', 2000)],
      })) as { code: number }
      expect(changed.code).toBe(-32013)

      // The refused batch wrote nothing.
      const page = (await client.call('conversation.entries', {
        id: created.conversationId,
        query: { order: 'ascending' },
        limit: 100,
      })) as { items: unknown[] }
      expect(JSON.stringify(page.items)).not.toContain('append-REPLACED')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('an append is refused while a run is in flight', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    faux.faux.setResponses([
      async () => {
        await held
        return fauxAssistantMessage('the answer')
      },
    ])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('history', 1000)],
      })) as Imported
      // The imported conversation has no model of its own; the run needs one.
      await client.call('conversation.configure', {
        id: created.conversationId,
        change: { model: faux.model },
      })
      const submitted = (await client.call('conversation.submit', {
        id: created.conversationId,
        input: 'a live question',
      })) as { submissionId: number }
      await until('the run', async () => {
        const record = (await client.call('submission.get', {
          id: created.conversationId,
          submissionId: submitted.submissionId,
        })) as { status: string }
        return record.status === 'placed'
      })

      const refused = (await client.fails('conversation.import', {
        id: created.conversationId,
        entries: [user('INTERLEAVED-HISTORY', 2000)],
      })) as { code: number }
      expect(refused.code).toBe(-32001)

      release()
      await until('the answer', async () => {
        const page = (await client.call('conversation.entries', {
          id: created.conversationId,
          query: { order: 'ascending' },
          limit: 100,
        })) as { items: { kind: string; model?: unknown }[] }
        return page.items.some(
          (entry) =>
            entry.kind === 'pi.assistant' && JSON.stringify(entry.model).includes('the answer'),
        )
      })
      // And the question and its answer are still adjacent: nothing was spliced between them.
      const page = (await client.call('conversation.entries', {
        id: created.conversationId,
        query: { order: 'ascending' },
        limit: 100,
      })) as { items: { kind: string; model?: unknown }[] }
      expect(JSON.stringify(page.items)).not.toContain('INTERLEAVED-HISTORY')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test("a fork does not inherit its parent's import memos", async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const parent = (await client.call('conversation.import', {
        key: 'parent-key',
        entries: [user('parent-text', 1000)],
      })) as Imported
      const tail = (await client.call('conversation.entries', {
        id: parent.conversationId,
        query: { order: 'descending' },
        limit: 1,
      })) as { items: { id: number }[] }
      const forked = (await client.call('conversation.fork', {
        id: parent.conversationId,
        at: tail.items[0]!.id,
      })) as { conversationId: number }

      const own = (await client.call('conversation.import', {
        id: forked.conversationId,
        key: 'parent-key',
        entries: [user('fork-own-new-text', 2000)],
      })) as Imported
      expect(own.replayed).toBeUndefined()
      expect(own.entryIds).toHaveLength(1)

      const page = (await client.call('conversation.entries', {
        id: forked.conversationId,
        query: { order: 'ascending' },
        limit: 100,
      })) as { items: { conversationId: number; model?: unknown }[] }
      const written = page.items.filter((entry) => entry.conversationId === forked.conversationId)
      expect(JSON.stringify(written)).toContain('fork-own-new-text')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('edits and head are checked before they reach Pi', async () => {
    const { kernel, client, data } = await kernelForTest()
    const fails = (params: Record<string, unknown>) =>
      client.fails('conversation.import', params) as Promise<{
        code: number
        data?: { issues?: { path: string }[] }
      }>
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('one', 1000)],
      })) as Imported
      const edits = await fails({
        id: created.conversationId,
        entries: [{ kind: 'droi.note', data: { text: 'x' }, edits: 'not-an-array' }],
      })
      expect(edits.code).toBe(-32602)
      expect(edits.data?.issues?.[0]?.path).toBe('entries[0].edits')

      const head = await fails({
        id: created.conversationId,
        entries: [{ kind: 'pi.reset', head: 'not-an-id' }],
      })
      expect(head.code).toBe(-32602)
      expect(head.data?.issues?.[0]?.path).toBe('entries[0].head')

      const target = await fails({
        id: created.conversationId,
        entries: [{ kind: 'pi.user', edits: [{ target: 'x', action: 'omit' }] }],
      })
      expect(target.code).toBe(-32602)
      expect(target.data?.issues?.[0]?.path).toBe('entries[0].edits[0].target')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)
})

describe('conversation.import, activity and identity details', () => {
  test('a history is dated by its messages, and a timeless batch does not move activity', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      // No `createdAt`: the conversation's clock is its own messages, not the import's.
      const created = (await client.call('conversation.import', {
        entries: [user('old history', 1000)],
      })) as Imported
      const first = (await client.call('conversation.list', { limit: 20 })) as {
        items: { id: number; createdAt: number; lastActivityAt: number }[]
      }
      const row = first.items.find((item) => item.id === created.conversationId)
      expect(row?.lastActivityAt).toBe(1000)
      expect(row?.createdAt).toBe(1000)

      // A bookkeeping entry carries no time, so it must not push the conversation up the list.
      await client.call('conversation.import', {
        id: created.conversationId,
        entries: [{ kind: 'droi.note', data: { text: 'a timeless note' } }],
      })
      await client.call('conversation.import', { id: created.conversationId, entries: [] })
      const after = (await client.call('conversation.list', { limit: 20 })) as {
        items: { id: number; lastActivityAt: number }[]
      }
      expect(after.items.find((item) => item.id === created.conversationId)?.lastActivityAt).toBe(
        1000,
      )
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('the workspace is the resolved cwd, as conversation.create records it', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const created = (await client.call('conversation.import', {
        cwd: '/top',
        agent: { cwd: '/actual' },
        entries: [user('where do I run', 1000)],
      })) as Imported
      const listed = (await client.call('conversation.list', {
        workspace: '/actual',
        limit: 20,
      })) as {
        items: { id: number; workspace?: string }[]
      }
      expect(listed.items.find((item) => item.id === created.conversationId)?.workspace).toBe(
        '/actual',
      )
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('every usage counter Pi keeps survives an import', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const created = (await client.call('conversation.import', {
        entries: [
          {
            kind: 'pi.assistant',
            model: [
              {
                role: 'assistant',
                content: [{ type: 'text', text: 'answered' }],
                provider: 'fake',
                model: 'fake-1',
                usage: {
                  input: 10,
                  output: 20,
                  cacheRead: 30,
                  cacheWrite: 40,
                  cacheWrite1h: 5,
                  reasoning: 7,
                  totalTokens: 60,
                  cost: { input: 0.1, output: 0.2, cacheRead: 0, cacheWrite: 0, total: 0.3 },
                },
                timestamp: 1000,
              },
            ],
          },
        ],
      })) as Imported
      const usage = (await client.call('conversation.usage', { id: created.conversationId })) as {
        models: Record<string, { cacheWrite1h?: number; reasoning?: number; totalTokens: number }>
      }
      expect(usage.models['fake/fake-1']).toMatchObject({
        cacheWrite1h: 5,
        reasoning: 7,
        totalTokens: 60,
      })
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('four clients importing one key at once converge on one conversation', async () => {
    const { kernel, client, data } = await kernelForTest()
    const peers = Array.from({ length: 4 }, () => new Client(kernel.port))
    try {
      const params = { key: 'concurrent-create', createdAt: 100, entries: [user('one copy', 1000)] }
      const results = (await Promise.all(
        peers.map((peer) => peer.call('conversation.import', params)),
      )) as Imported[]
      const ids = new Set(results.map((result) => result.conversationId))
      expect(ids.size).toBe(1)
      const listed = (await client.call('conversation.list', { query: 'one copy' })) as {
        items: { id: number }[]
      }
      expect(listed.items).toHaveLength(1)
    } finally {
      peers.forEach((peer) => peer.close())
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)
})

describe('conversation.import, round 2', () => {
  test('a key with quotes, a backslash or a newline replays after its cache row is lost', async () => {
    // The cache is filled from the memos, not searched for: so an escaped key is found by exactly matching the
    // parsed one, and nothing about the key's text matters.
    const keys = ['quote"key', 'back\\slash', 'line\nbreak', 'percent%and_underscore', '中文键']
    const data = tempData()
    let kernel = await start({ dir: data.dir, faux: fauxCatalog() })
    let client = new Client(kernel.port)
    const imported: { key: string; conversationId: number; entryIds: number[] }[] = []
    try {
      for (const key of keys) {
        const created = (await client.call('conversation.import', {
          key,
          createdAt: 100,
          entries: [user(`payload ${key}`, 1000)],
        })) as Imported
        imported.push({ key, conversationId: created.conversationId, entryIds: created.entryIds })
      }
      client.close()
      await kernel.stop()

      // The cache is a cache: drop it, and the catch-up rebuilds it from the memos.
      const db = new Database(join(data.dir, 'kernel.sqlite'))
      db.run('DELETE FROM droi_import_keys')
      db.close()

      kernel = await start({ dir: data.dir, faux: fauxCatalog() })
      client = new Client(kernel.port)
      for (const entry of imported) {
        const replay = (await client.call('conversation.import', {
          key: entry.key,
          createdAt: 100,
          entries: [user(`payload ${entry.key}`, 1000)],
        })) as Imported
        expect(replay.conversationId).toBe(entry.conversationId)
        expect(replay.entryIds).toEqual(entry.entryIds)
        expect(replay.replayed).toBe(true)
      }
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('only a droi.import entry is a memo', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      // A payload that looks like a memo but is an ordinary note must not become one.
      const seeded = (await client.call('conversation.import', {
        entries: [
          {
            kind: 'droi.note',
            data: {
              marker: 'droi.import',
              key: 'forged',
              fingerprint: 'x',
              entryIds: [12345],
              scope: 'create',
            },
          },
        ],
      })) as Imported
      const created = (await client.call('conversation.import', {
        key: 'forged',
        entries: [user('the real history', 1000)],
      })) as Imported
      expect(created.replayed).toBeUndefined()
      expect(created.conversationId).not.toBe(seeded.conversationId)
      const page = (await client.call('conversation.entries', {
        id: created.conversationId,
        query: { order: 'ascending' },
        limit: 100,
      })) as { items: { model?: unknown }[] }
      expect(JSON.stringify(page.items)).toContain('the real history')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test("an append's key is local, so a create key keeps its own identity", async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      const params = { key: 'shared-key', entries: [user('parent history', 1000)] }
      const parent = (await client.call('conversation.import', params)) as Imported
      const tail = (await client.call('conversation.entries', {
        id: parent.conversationId,
        query: { order: 'descending' },
        limit: 1,
      })) as { items: { id: number }[] }
      const forked = (await client.call('conversation.fork', {
        id: parent.conversationId,
        at: tail.items[0]!.id,
      })) as { conversationId: number }

      // The fork appends under the same key: that is the fork's own batch, and must not touch the create's.
      const own = (await client.call('conversation.import', {
        id: forked.conversationId,
        key: 'shared-key',
        entries: [user('fork batch', 2000)],
      })) as Imported
      expect(own.replayed).toBeUndefined()

      // And retrying the create still answers the parent, not a conflict.
      const replay = (await client.call('conversation.import', params)) as Imported
      expect(replay.conversationId).toBe(parent.conversationId)
      expect(replay.replayed).toBe(true)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('replacement messages are validated one by one', async () => {
    const { kernel, client, data } = await kernelForTest()
    const fails = (params: Record<string, unknown>) =>
      client.fails('conversation.import', params) as Promise<{
        code: number
        data?: { issues?: { path: string }[] }
      }>
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('one', 1000)],
      })) as Imported
      const target = created.entryIds[0]!
      const nullMessage = await fails({
        id: created.conversationId,
        entries: [
          {
            kind: 'droi.note',
            data: { text: 'x' },
            edits: [{ target, action: 'replace', messages: [null] }],
          },
        ],
      })
      expect(nullMessage.code).toBe(-32602)
      expect(nullMessage.data?.issues?.[0]?.path).toBe('entries[0].edits[0].messages[0]')

      const stringContent = await fails({
        id: created.conversationId,
        entries: [
          {
            kind: 'droi.note',
            edits: [
              {
                target,
                action: 'replace',
                messages: [{ role: 'assistant', content: 'text', timestamp: 1000 }],
              },
            ],
          },
        ],
      })
      expect(stringContent.code).toBe(-32602)
      expect(stringContent.data?.issues?.[0]?.path).toBe('entries[0].edits[0].messages[0].content')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('a head must be a safe entry id this conversation holds', async () => {
    const { kernel, client, data } = await kernelForTest()
    const fails = (params: Record<string, unknown>) =>
      client.fails('conversation.import', params) as Promise<{
        code: number
        data?: { issues?: { path: string }[] }
      }>
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('one', 1000)],
      })) as Imported
      // 1e100 passes Number.isInteger and used to reach SQLite's INTEGER column, poisoning the Session.
      const huge = await fails({
        id: created.conversationId,
        entries: [{ kind: 'pi.reset', head: 1e100, model: [] }],
      })
      expect(huge.code).toBe(-32602)
      expect(huge.data?.issues?.[0]?.path).toBe('entries[0].head')

      const missing = await fails({
        id: created.conversationId,
        entries: [{ kind: 'pi.reset', head: 987654, model: [] }],
      })
      expect(missing.code).toBe(-32602)
      expect(missing.data?.issues?.[0]?.path).toBe('entries[0].head')

      // On a create nothing has an id yet, so only "self" can be a head.
      const onCreate = await fails({
        entries: [{ kind: 'pi.reset', head: 1, model: [] }],
      })
      expect(onCreate.code).toBe(-32602)
      expect(onCreate.data?.issues?.[0]?.path).toBe('entries[0].head')

      // The conversation still works after the refused requests.
      const appended = (await client.call('conversation.import', {
        id: created.conversationId,
        entries: [user('still fine', 2000)],
      })) as Imported
      expect(appended.entryIds).toHaveLength(1)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)

  test('an own __proto__ in data takes part in the fingerprint', async () => {
    const { kernel, client, data } = await kernelForTest()
    try {
      // `{"__proto__": {"value": 1}}` is an own field of the parsed data; a normalisation that assigns it
      // would set the prototype, lose it, and make two different payloads look like one request.
      const withProto = (value: number) => ({
        key: 'proto-key',
        createdAt: 100,
        entries: [
          {
            kind: 'droi.note',
            data: JSON.parse(`{"__proto__": {"value": ${value}}}`) as unknown,
          },
        ],
      })
      await client.call('conversation.import', withProto(1))
      const changed = (await client.fails('conversation.import', withProto(2))) as { code: number }
      expect(changed.code).toBe(-32013)
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)
})

describe('conversation.import, a history without usage', () => {
  test('an imported answer without usage does not stop the next turn', async () => {
    const data = tempData()
    const faux = fauxCatalog()
    faux.faux.setResponses([fauxAssistantMessage('the next answer')])
    const kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('an old question', 1000), assistant('an old answer', 2000)],
      })) as Imported
      await client.call('conversation.configure', {
        id: created.conversationId,
        change: { model: faux.model },
      })
      await client.call('conversation.submit', { id: created.conversationId, input: 'go on' })
      await until(
        'the answer',
        async () => {
          const page = (await client.call('conversation.entries', {
            id: created.conversationId,
            query: { order: 'ascending' },
            limit: 100,
          })) as { items: { kind: string; model?: unknown }[] }
          return page.items.some(
            (entry) =>
              entry.kind === 'pi.assistant' &&
              JSON.stringify(entry.model).includes('the next answer'),
          )
        },
        20_000,
      )
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  }, 60_000)
})
