import { afterEach, describe, expect, test } from 'bun:test'
import { createModels, fauxAssistantMessage, fauxProvider } from '@earendil-works/pi-ai'
import type { FauxResponseStep } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, waitIdle } from './helpers.ts'

/**
 * `conversation.compact {wait: true}`: the window's `/compact` waits for the compaction and says what it did, so a
 * compaction that found nothing to summarize is not silence.
 */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

async function boot(
  responses: Parameters<ReturnType<typeof fauxCatalog>['faux']['setResponses']>[0],
) {
  const data = tempData()
  const faux = fauxCatalog()
  faux.faux.setResponses(responses)
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  cleanups.push(() => data.cleanup())
  cleanups.push(async () => {
    client.close()
    await kernel.stop()
  })
  const { conversationId: id } = (await client.call('conversation.create', {
    agent: { model: faux.model },
  })) as { conversationId: number }
  return { client, id }
}

async function turn(client: Client, id: number, input: string): Promise<void> {
  const before = client.notifications.length
  await client.call('conversation.submit', { id, input })
  // waitIdle reads the newest busy push; drop the older ones so it waits for this run.
  client.notifications.splice(0, before)
  await waitIdle(client, id)
}

describe('conversation.compact with wait', () => {
  test('a short conversation answers nothing: it is within what a summary keeps', async () => {
    const { client, id } = await boot([fauxAssistantMessage('short answer')])
    await turn(client, id, 'hello')
    const answer = await client.call('conversation.compact', { id, wait: true })
    expect(answer['outcome']).toBe('nothing')
    expect(typeof answer['taskId']).toBe('number')
  }, 60_000)

  test('a long one answers compacted once the summary is written', async () => {
    const long = (n: number) => `answer ${n}: ` + 'lorem ipsum dolor sit amet '.repeat(2500)
    const { client, id } = await boot([
      fauxAssistantMessage(long(1)),
      fauxAssistantMessage(long(2)),
      fauxAssistantMessage(long(3)),
      fauxAssistantMessage('the summary'),
    ])
    for (let n = 1; n <= 3; n++) await turn(client, id, `turn ${n}`)
    const answer = await client.call('conversation.compact', { id, wait: true })
    expect(answer['outcome']).toBe('compacted')
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    expect(
      (page['items'] as { kind: string }[]).some((entry) => entry.kind === 'pi.compaction'),
    ).toBe(true)
  }, 60_000)

  test('without wait it answers the task at once, as before', async () => {
    const { client, id } = await boot([fauxAssistantMessage('short answer')])
    await turn(client, id, 'hello')
    const answer = await client.call('conversation.compact', { id })
    expect(Object.keys(answer)).toEqual(['taskId'])
  }, 60_000)
})

/** A long answer: three of them are more than a summary keeps verbatim. */
const long = (n: number) => `answer ${n}: ` + 'lorem ipsum dolor sit amet '.repeat(2500)

/** A kernel with two faux models, `main` for the conversation and `small` to compact with, and who answered what. */
async function bootTwo(compactionModel?: string) {
  const data = tempData()
  const faux = fauxProvider({ models: [{ id: 'main' }, { id: 'small' }] })
  const models = createModels()
  models.setProvider(faux.provider)
  const answered: { model: string; summary: boolean }[] = []
  const answer =
    (text: string): FauxResponseStep =>
    (context, _options, _state, model) => {
      const summary = JSON.stringify(context.messages).includes('<conversation>')
      answered.push({ model: model.id, summary })
      return fauxAssistantMessage(text)
    }
  faux.setResponses([answer(long(1)), answer(long(2)), answer(long(3)), answer('the summary')])
  const provider = faux.getModel().provider
  const kernel = await start({
    dir: data.dir,
    faux: { faux, models, model: { provider, modelId: 'main' } },
    ...(compactionModel === undefined
      ? {}
      : { overrides: { compactionModel: { provider, modelId: compactionModel } } }),
  })
  const client = new Client(kernel.port)
  cleanups.push(() => data.cleanup())
  cleanups.push(async () => {
    client.close()
    await kernel.stop()
  })
  const { conversationId: id } = (await client.call('conversation.create', {
    agent: { model: { provider, modelId: 'main' } },
  })) as { conversationId: number }
  for (let n = 1; n <= 3; n++) await turn(client, id, `turn ${n}`)
  return { client, id, answered, provider }
}

async function summaryText(client: Client, id: number): Promise<string> {
  const page = await client.call('conversation.entries', {
    id,
    query: { order: 'ascending' },
    limit: 200,
  })
  const entry = (page['items'] as { kind: string; model?: unknown }[]).find(
    (item) => item.kind === 'pi.compaction',
  )
  return JSON.stringify(entry?.model ?? '')
}

describe('the compaction model', () => {
  test("unset, the conversation's own model writes the summary", async () => {
    const { client, id, answered } = await bootTwo()
    expect((await client.call('harness.info', {}))['compaction']).toEqual({ model: null })
    expect((await client.call('conversation.compact', { id, wait: true }))['outcome']).toBe(
      'compacted',
    )
    expect(answered.filter((a) => a.summary)).toEqual([{ model: 'main', summary: true }])
  }, 60_000)

  test("set at launch, it writes the summary in Pi's format", async () => {
    const { client, id, answered, provider } = await bootTwo('small')
    expect((await client.call('harness.info', {}))['compaction']).toEqual({
      model: { provider, modelId: 'small' },
    })
    expect((await client.call('conversation.compact', { id, wait: true }))['outcome']).toBe(
      'compacted',
    )
    expect(answered.filter((a) => a.summary)).toEqual([{ model: 'small', summary: true }])
    expect(await summaryText(client, id)).toContain('the summary')
  }, 60_000)

  test("harness.configure sets and clears it; a model the kernel lacks leaves the summary to the conversation's", async () => {
    const { client, id, answered, provider } = await bootTwo()
    await client.call('harness.configure', {
      compaction: { model: { provider, modelId: 'nowhere' } },
    })
    expect((await client.call('harness.info', {}))['compaction']).toEqual({
      model: { provider, modelId: 'nowhere' },
    })
    expect((await client.call('conversation.compact', { id, wait: true }))['outcome']).toBe(
      'compacted',
    )
    expect(answered.filter((a) => a.summary)).toEqual([{ model: 'main', summary: true }])
    await client.call('harness.configure', { compaction: { model: null } })
    expect((await client.call('harness.info', {}))['compaction']).toEqual({ model: null })
    const refused = await client.fails('harness.configure', { compaction: { model: { provider } } })
    expect(refused.code).toBe(-32602)
  }, 60_000)
})
