import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'

/**
 * The two questions the sidebar asked about what the kernel already sends: what `preview` holds, and
 * whether `conversation.busy` reaches a connection that never opened the conversation.
 */

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

async function create(): Promise<number> {
  const answer = await client.call('conversation.create', {
    agent: { model: faux.model, cwd: process.cwd() },
  })
  return answer['conversationId'] as number
}

async function preview(id: number): Promise<string | null | undefined> {
  const page = await client.call('conversation.list', {})
  const row = (page['items'] as { id: number; preview: string | null }[]).find(
    (item) => item.id === id,
  )
  return row?.preview
}

describe('preview', () => {
  test("is the newest message's first line, the answer's once there is one", async () => {
    const id = await create()
    faux.faux.setResponses([
      fauxAssistantMessage('The answer\nwith a second line that is not shown'),
    ])
    await client.call('conversation.submit', { id, input: 'the question' })
    for (let i = 0; i < 200 && (await preview(id)) !== 'The answer'; i++) await Bun.sleep(20)
    expect(await preview(id)).toBe('The answer')
  })

  test('is the question while the answer is still being written, then the answer', async () => {
    const id = await create()
    // A slow answer: the transcript holds the question and, for a moment, nothing after it.
    faux.faux.setResponses([
      async () => {
        await Bun.sleep(1500)
        return fauxAssistantMessage('the answer')
      },
    ])
    await client.call('conversation.submit', { id, input: 'a question still being answered' })
    for (let i = 0; i < 200 && (await preview(id)) !== 'a question still being answered'; i++) {
      await Bun.sleep(20)
    }
    expect(await preview(id)).toBe('a question still being answered')
    for (let i = 0; i < 300 && (await preview(id)) !== 'the answer'; i++) await Bun.sleep(20)
    expect(await preview(id)).toBe('the answer')
  })

  test('is cut to one bounded line', async () => {
    const id = await create()
    const long = `${'x'.repeat(400)}\ntail`
    faux.faux.setResponses([fauxAssistantMessage(long)])
    await client.call('conversation.submit', { id, input: 'long answer please' })
    for (let i = 0; i < 200 && !(await preview(id))?.endsWith('…'); i++) await Bun.sleep(20)
    const shown = await preview(id)
    expect(shown).toBeDefined()
    expect(shown!.length).toBeLessThanOrEqual(200)
    expect(shown!.endsWith('…')).toBe(true)
    expect(shown).not.toContain('tail')
  })
})

describe('conversation.busy', () => {
  test('a round on a conversation nobody opened still tells every connection', async () => {
    const id = await create()
    const watcher = new Client(kernel.port)
    await watcher.opened
    faux.faux.setResponses([fauxAssistantMessage('done')])
    await client.call('conversation.submit', { id, input: 'go' })

    // No `conversation.open` anywhere: the busy notice is broadcast, not subscribed.
    await watcher.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] === null,
    )
    const busy = watcher.notifications.filter(
      (note) => note.method === 'conversation.busy' && note.params['id'] === id,
    )
    // It went busy first: a `run` object, then `null`. Never the other way round.
    expect(busy.some((note) => note.params['run'] !== null)).toBe(true)
    expect(busy.at(-1)?.params['run']).toBeNull()
    watcher.close()
  })
})
