import { afterEach, describe, expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
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
