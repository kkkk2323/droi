import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { allEntries, Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
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

/**
 * A long conversation, which is where a page cap that nobody published once stalled a run: the entries were
 * all there, the client asked for 5000, and the kernel answered the newest 200 in ascending order — so the
 * wait for the round's own entries never ended, and it looked like a deadlock at the 49th round (200 entries
 * in). These two tests pin both halves: a hundred tool rounds complete, and `entries` says where its page ends.
 */
describe('a long conversation', () => {
  test('a hundred tool rounds finish, and the transcript holds every one', async () => {
    const file = join(data.dir, 'sample.txt')
    writeFileSync(file, Array.from({ length: 400 }, (_, line) => `line ${line}`).join('\n'))
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: data.dir },
    })
    const id = created['conversationId'] as number

    for (let round = 0; round < 100; round++) {
      const before = new Set((await allEntries(client, id)).map((entry) => entry.id))
      faux.faux.setResponses([
        fauxAssistantMessage(
          fauxToolCall('read', { path: file, offset: (round % 20) + 1, limit: 5 }),
          {
            stopReason: 'toolUse',
          },
        ),
        fauxAssistantMessage(`round ${round} done`),
      ])
      await client.call('conversation.submit', { id, input: `round ${round}` })
      const deadline = Date.now() + 30_000
      let done = false
      while (Date.now() < deadline) {
        const fresh = (await allEntries(client, id)).filter((entry) => !before.has(entry.id))
        if (
          fresh.filter((entry) => entry.kind === 'pi.tool-result').length >= 1 &&
          fresh.filter((entry) => entry.kind === 'pi.assistant').length >= 2
        ) {
          done = true
          break
        }
        await Bun.sleep(5)
      }
      expect({ round, done }).toEqual({ round, done: true })
    }

    const entries = await allEntries(client, id)
    expect(entries.filter((entry) => entry.kind === 'pi.tool-result').length).toBe(100)
    expect(entries.filter((entry) => entry.kind === 'pi.assistant').length).toBe(200)
    // Nothing is left running, and the conversation still takes work.
    const inspect = await kernel.harness.inspect({ signal: new AbortController().signal } as never)
    expect(
      inspect.tasks.filter((task) => task.record.conversationId === (id as never)).length,
    ).toBe(0)
  }, 180_000)

  test('a page larger than the cap comes back at the cap, with a cursor and the limit published', async () => {
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    await kernel.harness.commit(
      async (tx) => {
        for (let index = 0; index < 250; index++) {
          await tx.appendEntry(id as never, { kind: 'droi.note', data: { text: `note ${index}` } })
        }
      },
      { signal: new AbortController().signal } as never,
    )

    const info = await client.call('harness.info')
    const limits = info['limits'] as { maxEntriesPerPage: number; maxConversationsPerPage: number }
    expect(limits.maxEntriesPerPage).toBe(200)
    expect(limits.maxConversationsPerPage).toBe(200)

    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 1000,
    })
    expect((page['items'] as unknown[]).length).toBe(limits.maxEntriesPerPage)
    // The cursor is what tells a client the page is not the whole transcript.
    expect(page['cursor']).toBeDefined()
    const all = await allEntries(client, id)
    // 250 notes, plus the `droi.meta` entry the creating commit writes.
    expect(all.filter((entry) => entry.kind === 'droi.note').length).toBe(250)
  })

  test('the list also caps its page and publishes the same limit', async () => {
    const page = await client.call('conversation.list', { limit: 1000 })
    const limits = (
      (await client.call('harness.info'))['limits'] as { maxConversationsPerPage: number }
    ).maxConversationsPerPage
    expect(limits).toBe(200)
    expect(Array.isArray(page['items'])).toBe(true)
  })
})
