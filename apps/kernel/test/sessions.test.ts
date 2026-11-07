import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { clientRounds } from './rounds.ts'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'

/** A scripted answer that takes a while, so a test can look at a conversation while it runs. */
const slowAnswer =
  (text: string, ms = 1500) =>
  async () => {
    await Bun.sleep(ms)
    return fauxAssistantMessage(text)
  }
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

describe('conversations', () => {
  test('three conversations, two rounds each, keep their own answers and entries', async () => {
    const answers = await clientRounds(client, faux, {
      conversations: [
        { rounds: ['alpha one', 'alpha two'] },
        { rounds: ['beta one', 'beta two'] },
        { rounds: ['gamma one', 'gamma two'] },
      ],
    })
    for (const [index, result] of answers.entries()) {
      const texts = result.answerTexts
      expect(texts).toEqual(
        [
          ['alpha one', 'alpha two'],
          ['beta one', 'beta two'],
          ['gamma one', 'gamma two'],
        ][index]!,
      )
      // Each conversation holds only its own user input, so nothing bled across.
      expect(result.inputs).toEqual(['round 0', 'round 1'])
    }
    const ids = new Set(answers.map((answer) => answer.conversationId))
    expect(ids.size).toBe(3)
  })

  test('five conversations run at once without mixing their answers', async () => {
    const plan = [0, 1, 2, 3, 4].map((n) => ({
      rounds: [`parallel ${n} first`, `parallel ${n} second`],
    }))
    const answers = await clientRounds(client, faux, { conversations: plan, concurrent: true })
    for (const [index, result] of answers.entries()) {
      expect(result.answerTexts).toEqual([`parallel ${index} first`, `parallel ${index} second`])
    }
  })

  test('the list shows each conversation once, newest first, with a title from its first message', async () => {
    await clientRounds(client, faux, {
      conversations: [{ rounds: ['first answer'], inputs: ['first words of the chat'] }],
    })
    await clientRounds(client, faux, {
      conversations: [{ rounds: ['second answer'], inputs: ['second chat begins'] }],
    })
    const page = await client.call('conversation.list', { limit: 10 })
    const items = page['items'] as { id: number; title: string }[]
    expect(items.length).toBe(2)
    expect(items[0]!.id).toBeGreaterThan(items[1]!.id)
    expect(items.map((item) => item.title)).toContain('first words of the chat')
  })

  test('search finds a conversation by a word inside an answer', async () => {
    await clientRounds(client, faux, {
      conversations: [{ rounds: ['tell me about the parapet'] }, { rounds: ['unrelated chatter'] }],
    })
    const page = await client.call('conversation.list', { query: 'parapet' })
    const items = page['items'] as { id: number }[]
    expect(items.length).toBe(1)
  })

  test('index.update renames and archives, and archive keeps it out of the default list', async () => {
    const [only] = await clientRounds(client, faux, { conversations: [{ rounds: ['rename me'] }] })
    const id = only!.conversationId
    await client.call('index.update', { id, title: 'A chosen name' })
    const renamed = (await client.call('conversation.list', {}))['items'] as { title: string }[]
    expect(renamed[0]?.title).toBe('A chosen name')
    await client.call('index.update', { id, archived: true })
    expect(((await client.call('conversation.list', {}))['items'] as unknown[]).length).toBe(0)
    expect(
      ((await client.call('conversation.list', { archived: true }))['items'] as unknown[]).length,
    ).toBe(1)
  })

  test('conversation.open answers a view and then sends conversation.view for new entries', async () => {
    const [only] = await clientRounds(client, faux, { conversations: [{ rounds: ['watched'] }] })
    const id = only!.conversationId
    const opened = await client.call('conversation.open', { id })
    const view = opened['view'] as { entries: unknown[]; docs: Record<string, unknown> }
    expect(view.entries.length).toBeGreaterThan(1)
    expect(view.docs['pi.agent']).toBeDefined()

    faux.faux.appendResponses([fauxAssistantMessage('another answer')])
    await client.call('conversation.submit', { id, input: 'one more' })
    const note = await client.waitFor(
      (candidate) =>
        candidate.method === 'conversation.view' &&
        candidate.params['id'] === id &&
        (candidate.params['value'] as { entries: { kind: string }[] }).entries.some(
          (entry) => entry.kind === 'pi.user',
        ),
      10_000,
    )
    const value = note.params['value'] as { entries: unknown[]; docs: Record<string, unknown> }
    expect(value.entries.length).toBeGreaterThan(0)
    expect(typeof note.params['ops']).toBe('object')
  })

  test('conversation.unwatch stops the views for that connection only', async () => {
    const [only] = await clientRounds(client, faux, {
      conversations: [{ rounds: ['watched twice'] }],
    })
    const id = only!.conversationId
    await client.call('conversation.open', { id })
    const other = new Client(kernel.port)
    await other.call('conversation.open', { id })
    await client.call('conversation.unwatch', { id })

    const before = client.notifications.length
    faux.faux.appendResponses([fauxAssistantMessage('another answer')])
    await client.call('conversation.submit', { id, input: 'after unwatch' })
    await other.waitFor((n) => n.method === 'conversation.view', 5_000)
    await Bun.sleep(100)
    const views = client.notifications
      .slice(before)
      .filter((note) => note.method === 'conversation.view')
    expect(views.length).toBe(0)
    other.close()
  })

  test('conversation.busy is broadcast to a connection that never opened it', async () => {
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    const watcher = new Client(kernel.port)
    // A connection must exist before the run starts, or it cannot be told about it.
    await watcher.opened
    faux.faux.setResponses([slowAnswer('hello answer')])
    await client.call('conversation.submit', { id, input: 'hello there' })
    const busy = await watcher.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] !== null,
      10_000,
    )
    expect(busy.params['run']).not.toBeNull()
    watcher.close()
  })

  test('a second submit while busy queues, and abort ends the run', async () => {
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    faux.faux.setResponses([slowAnswer('slow')])
    await client.call('conversation.submit', { id, input: 'first' })
    const second = await client.call('conversation.submit', {
      id,
      input: 'second',
      whenBusy: 'followUp',
    })
    expect(typeof second['submissionId']).toBe('number')
    await client.call('conversation.abort', { id })
    const idle = await client.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] === null,
      10_000,
    )
    expect(idle.params['run']).toBeNull()
  })

  test('reject-while-busy is -32001 and changing nothing', async () => {
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    faux.faux.setResponses([slowAnswer('slow')])
    client.notifications.length = 0
    await client.call('conversation.submit', { id, input: 'busy now' })
    // Waits for the run to be admitted: an admission is what makes the next submit a busy one.
    await client.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === id &&
        note.params['run'] !== null,
      10_000,
    )
    const error = await client.fails('conversation.submit', {
      id,
      input: 'nope',
      whenBusy: 'reject',
    })
    expect(error.code).toBe(-32001)
    await client.call('conversation.abort', { id })
  })

  test('a repeated requestId returns the same conversation; a different one is -32013', async () => {
    const first = await client.call('conversation.create', { requestId: 'same-key', kind: 'chat' })
    const again = await client.call('conversation.create', { requestId: 'same-key', kind: 'chat' })
    expect(again['conversationId']).toBe(first['conversationId'])
    const error = await client.fails('conversation.create', {
      requestId: 'same-key',
      kind: 'memory',
    })
    expect(error.code).toBe(-32013)
    const created = await client.call('conversation.create', { kind: 'memory' })
    void created
  })

  test('conversation.usage reports the model spend and a context estimate', async () => {
    const [only] = await clientRounds(client, faux, {
      conversations: [{ rounds: ['count my tokens'] }],
    })
    const usage = await client.call('conversation.usage', { id: only!.conversationId })
    const models = usage['models'] as Record<string, { totalTokens: number }>
    const keys = Object.keys(models)
    expect(keys.length).toBe(1)
    expect(models[keys[0]!]!.totalTokens).toBeGreaterThan(0)
    const context = usage['context'] as { used: number; limit: number; entries: number }
    expect(context.entries).toBeGreaterThan(0)
    expect(context.used).toBeGreaterThan(0)
    expect(context.limit).toBeGreaterThan(0)
  })

  test('conversation.configure resolves names back to the registry', async () => {
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const id = created['conversationId'] as number
    const configured = await client.call('conversation.configure', {
      id,
      change: { instructions: 'Be brief.', modelTools: { remove: [] }, maxTurns: 5 },
    })
    const agent = configured['agent'] as { instructions: string; maxTurns: number; tools: string[] }
    expect(agent.instructions).toBe('Be brief.')
    expect(agent.maxTurns).toBe(5)
    expect(agent.tools).toContain('read')
    const error = await client.fails('conversation.configure', {
      id,
      change: { modelTools: { add: ['no-such-tool'] } },
    })
    expect(error.code).toBe(-32602)
  })

  test('conversation.fork starts a conversation at the chosen entry', async () => {
    const [only] = await clientRounds(client, faux, { conversations: [{ rounds: ['fork me'] }] })
    const page = await client.call('conversation.entries', {
      id: only!.conversationId,
      query: { order: 'ascending' },
    })
    const entries = page['items'] as { id: number; kind: string }[]
    const firstUser = entries.find((entry) => entry.kind === 'pi.user')!
    const forked = await client.call('conversation.fork', {
      id: only!.conversationId,
      at: firstUser.id,
    })
    expect(forked['conversationId']).not.toBe(only!.conversationId)
    const view = forked['view'] as {
      entries: { id: number; kind: string }[]
      conversation: { parent?: unknown }
    }
    expect(view.conversation.parent).toBeDefined()
    // The fork writes a `droi.meta` of its own as its first entry — its identity, not the parent's — so the
    // last entry is that record and the transcript stops at the fork point.
    const transcript = view.entries.filter((entry) => entry.kind !== 'droi.meta')
    expect(transcript.at(-1)?.id).toBe(firstUser.id)
  })
})
