import { describe, expect, test } from 'bun:test'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'

/**
 * `conversation.entries` with `query.kinds`: the entries of some kinds only, filtered here so a client that wants a
 * long conversation's questions (the turn rail) does not carry every answer and tool output over the wire.
 */

const user = (text: string, at: number) => ({
  kind: 'pi.user',
  model: [{ role: 'user', content: text, timestamp: at }],
})
const assistant = (text: string, at: number) => ({
  kind: 'pi.assistant',
  model: [
    {
      role: 'assistant',
      content: [{ type: 'text', text }],
      provider: 'fake',
      model: 'fake-1',
      timestamp: at,
    },
  ],
})

type Entry = { id: number; kind: string; model?: { content?: unknown }[] }

describe('conversation.entries with kinds', () => {
  test("pages through one kind by entry id, past more than one of Pi's pages", async () => {
    const data = tempData()
    const kernel = await start({ dir: data.dir, faux: fauxCatalog() })
    const client = new Client(kernel.port)
    try {
      // 300 turns: 600 entries, three of Pi's 200-entry pages, so the filter has to read on past the first.
      const entries = []
      for (let i = 0; i < 300; i++) {
        entries.push(
          user(`question ${i}`, 1762480000000 + 2 * i),
          assistant(`answer ${i}`, 1762480000001 + 2 * i),
        )
      }
      const created = (await client.call('conversation.import', { entries, title: 'Long' })) as {
        conversationId: number
        entryIds: number[]
      }
      const id = created.conversationId

      const questions: string[] = []
      let max: number | undefined
      for (let pages = 0; pages < 10; pages++) {
        const page = (await client.call('conversation.entries', {
          id,
          query: {
            order: 'descending',
            kinds: ['pi.user'],
            ...(max === undefined ? {} : { maxEntryId: max }),
          },
          limit: 120,
        })) as { items: Entry[]; cursor?: unknown }
        expect(page.cursor).toBeUndefined()
        expect(page.items.every((entry) => entry.kind === 'pi.user')).toBe(true)
        for (const entry of page.items) questions.push(entry.model?.[0]?.content as string)
        if (page.items.length < 120) break
        max = page.items[page.items.length - 1]!.id - 1
      }
      expect(questions).toHaveLength(300)
      expect(questions[0]).toBe('question 299')
      expect(questions[299]).toBe('question 0')

      // Bounded like any other query: only the questions at or below the bound.
      const below = (await client.call('conversation.entries', {
        id,
        query: { order: 'descending', kinds: ['pi.user'], maxEntryId: created.entryIds[9] },
        limit: 50,
      })) as { items: Entry[] }
      expect(below.items.map((entry) => entry.model?.[0]?.content)).toEqual([
        'question 4',
        'question 3',
        'question 2',
        'question 1',
        'question 0',
      ])
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  })

  test('refuses kinds that are not a list of names, and a cursor beside them', async () => {
    const data = tempData()
    const kernel = await start({ dir: data.dir, faux: fauxCatalog() })
    const client = new Client(kernel.port)
    try {
      const created = (await client.call('conversation.import', {
        entries: [user('hi', 1762480000000)],
      })) as {
        conversationId: number
      }
      const id = created.conversationId
      for (const kinds of [[], 'pi.user', [3]]) {
        const failed = await client.fails('conversation.entries', { id, query: { kinds } })
        expect(failed.code).toBe(-32602)
        expect(JSON.stringify(failed.data)).toContain('query.kinds')
      }
      const failed = await client.fails('conversation.entries', {
        id,
        query: { kinds: ['pi.user'] },
        cursor: { after: 1 },
      })
      expect(failed.code).toBe(-32602)
      expect(JSON.stringify(failed.data)).toContain('cursor')
    } finally {
      client.close()
      await kernel.stop()
      data.cleanup()
    }
  })
})
