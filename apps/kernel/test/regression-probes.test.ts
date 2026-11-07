// The second review's probes, kept as regressions. Written by the reviewer of PR #227 (see
// shared/reviews/pr227-round2.md); every case failed on that head and passes now.
import { afterEach, beforeEach, expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'
import { context } from '../src/session/service.ts'
import { Indexer } from '../src/indexer/index.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'

let data: ReturnType<typeof tempData>
let faux: ReturnType<typeof fauxCatalog>
let kernel: Awaited<ReturnType<typeof start>> | undefined
let client: Client | undefined
beforeEach(() => {
  data = tempData()
  faux = fauxCatalog()
})
afterEach(async () => {
  client?.close()
  await kernel?.stop()
  kernel = undefined
  data.cleanup()
})
async function open() {
  kernel = await start({ dir: data.dir, faux })
  client = new Client(kernel.port)
  return client
}
async function create(live: Client, extra: Record<string, unknown> = {}) {
  return live.call('conversation.create', { agent: { model: faux.model }, ...extra })
}

test('probe restart must preserve lastActivityAt', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  const before = (await live.call('conversation.list')).items as any[]
  live.close()
  await kernel!.stop()
  kernel = undefined
  await Bun.sleep(30)
  const again = await open()
  const after = (await again.call('conversation.list')).items as any[]
  console.log('ACTIVITY', { before: before[0].lastActivityAt, after: after[0].lastActivityAt })
  expect(after.find((x) => x.id === id).lastActivityAt).toBe(before[0].lastActivityAt)
})

test('probe fork must accept an entry older than the last 200', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  const entryIds: number[] = []
  await kernel!.harness.commit(async (tx) => {
    for (let i = 0; i < 205; i++) {
      const entry = await tx.appendEntry(id as never, {
        kind: 'droi.note',
        data: { text: `note ${i}` },
      })
      entryIds.push(entry.id as number)
    }
  }, context)
  const original = await kernel!.harness.conversation(id as never, context)
  const direct = await original!.fork(
    entryIds[0] as never,
    { ownership: { kind: 'ownerless' } },
    context,
  )
  console.log('FORK_DIRECT_OK', direct.id, 'AT', entryIds[0])
  const result = await live.call('conversation.fork', { id, at: entryIds[0] })
  expect(result.conversationId).toBeNumber()
})

test('probe create fingerprint must include modelTools', async () => {
  const live = await open()
  await create(live, { requestId: 'tool-key', agent: { model: faux.model, modelTools: [] } })
  const result = await live.fails('conversation.create', {
    requestId: 'tool-key',
    agent: { model: faux.model, modelTools: ['read'] },
  })
  expect(result.code).toBe(-32013)
})

test('probe concurrent create with the same requestId must converge', async () => {
  const live = await open()
  const params = { requestId: 'racing-create', agent: { model: faux.model } }
  const other = new Client(kernel!.port)
  await other.opened
  try {
    const results = await Promise.all([
      live.call('conversation.create', params),
      other.call('conversation.create', params),
    ])
    console.log(
      'CREATE_RACE',
      results.map((x) => x.conversationId),
    )
    expect(results[0]!.conversationId).toBe(results[1]!.conversationId)
  } finally {
    other.close()
  }
})

test('probe concurrent different submits must conflict', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  faux.faux.setResponses([fauxAssistantMessage('reply')])
  const other = new Client(kernel!.port)
  await other.opened
  try {
    const results = await Promise.allSettled([
      live.call('conversation.submit', { id, requestId: 'racing-submit', input: 'first' }),
      other.call('conversation.submit', { id, requestId: 'racing-submit', input: 'second' }),
    ])
    console.log('SUBMIT_RACE', results)
    expect(
      results.filter((x) => x.status === 'rejected' && (x.reason as any).code === -32013),
    ).toHaveLength(1)
  } finally {
    other.close()
  }
})

test('probe submit retry after missing fingerprint write must conflict', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  const remember = kernel!.indexer.rememberRequest.bind(kernel!.indexer)
  kernel!.indexer.rememberRequest = async () => {}
  faux.faux.setResponses([fauxAssistantMessage('reply')])
  const first = await live.call('conversation.submit', {
    id,
    requestId: 'crash-submit',
    input: 'first',
  })
  kernel!.indexer.rememberRequest = remember
  const second = await live.fails('conversation.submit', {
    id,
    requestId: 'crash-submit',
    input: 'second',
  })
  console.log('MISSING_MEMO', { first, second })
  expect(second.code).toBe(-32013)
})

test('probe replacement watch must retain live observation', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  await live.call('conversation.open', { id })
  await live.call('conversation.open', { id, window: { min: 50, max: 100 } })
  await Bun.sleep(30)
  console.log('REPLACED_LIVE_OBSERVATIONS', kernel!.kernel.liveObservations)
  expect(kernel!.kernel.liveObservations).toBe(1)
})

test('probe unwatch one of two viewers must retain the other live observer', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  const other = new Client(kernel!.port)
  try {
    await live.call('conversation.open', { id })
    await other.call('conversation.open', { id })
    await live.call('conversation.unwatch', { id })
    console.log('ONE_VIEWER_REMAINS', kernel!.kernel.liveObservations)
    expect(kernel!.kernel.liveObservations).toBe(1)
  } finally {
    other.close()
  }
})

test('probe short search must exclude entry outside 50000 window', async () => {
  const db = new BunSqliteDatabase(`${data.dir}/search.sqlite`)
  const indexer = new Indexer(db, {} as never)
  try {
    await indexer.open()
    await indexer.touch(1, 1, {})
    await indexer.flush()
    await db.run(
      'INSERT INTO droi_entries_fts(rowid, text, conversation) VALUES(1, ?, 1)',
      '古老内存记录',
    )
    await db.run(
      "WITH RECURSIVE rows(id) AS (SELECT 2 UNION ALL SELECT id + 1 FROM rows WHERE id < 50002) INSERT INTO droi_entries_fts(rowid, text, conversation) SELECT id, 'newest row', 2 FROM rows",
    )
    const result = await indexer.list({ text: '内存' })
    console.log(
      'UNBOUNDED_SHORT_SEARCH',
      result.items.map((x) => x.id),
    )
    expect(result.items).toHaveLength(0)
  } finally {
    await indexer.close()
    await db.close()
  }
})

test('probe watermark cannot skip another conversation after interrupted catchUp', async () => {
  const db = new BunSqliteDatabase(`${data.dir}/watermark.sqlite`)
  let interrupted = true
  let indexer: Indexer
  const storage = {
    async scanConversations() {
      return { items: [{ id: 1 }, { id: 2 }] }
    },
    async scanEntries(query: any) {
      if (query.conversationId === 2 && interrupted) {
        await indexer.flush()
        throw new Error('simulated crash after timer flushed first conversation')
      }
      const entries =
        query.conversationId === 1
          ? [
              {
                id: 100,
                conversationId: 1,
                kind: 'pi.user',
                model: [{ role: 'user', content: 'firstconv' }],
              },
            ]
          : [
              {
                id: 50,
                conversationId: 2,
                kind: 'pi.user',
                model: [{ role: 'user', content: 'lostneedle' }],
              },
            ]
      return { items: entries.filter((x) => x.id >= (query.minEntryId ?? 0)) }
    },
  }
  indexer = new Indexer(db, storage as never)
  try {
    await indexer.open()
    await indexer.catchUp().catch(() => {})
    console.log('INTERRUPTED_WATERMARK', indexer.watermark)
    interrupted = false
    indexer = new Indexer(db, storage as never)
    await indexer.open()
    await indexer.catchUp()
    const result = await indexer.list({ text: 'lostneedle' })
    console.log(
      'AFTER_SECOND_RECOVERY',
      result.items.map((x) => x.id),
    )
    expect(result.items.map((x) => x.id)).toContain(2)
  } finally {
    await indexer.close()
    await db.close()
  }
})

test('probe read-only startup must not mutate activity or viewedAt', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  const before = (await live.call('conversation.list')).items as any[]
  live.close()
  await kernel!.stop()
  kernel = undefined
  await Bun.sleep(30)
  kernel = await start({ dir: data.dir, faux, overrides: { readOnly: true } })
  client = new Client(kernel.port)
  await client.call('conversation.open', { id })
  const after = (await client.call('conversation.list')).items as any[]
  console.log('READONLY_META', { before: before[0], after: after[0] })
  expect(after[0].viewedAt).toBe(before[0].viewedAt)
  expect(after[0].lastActivityAt).toBe(before[0].lastActivityAt)
})

test('probe closing connection midway through open must not leak a subscription', async () => {
  const live = await open()
  const { conversationId: id } = await create(live)
  const connection = kernel!.server.connections()[0]!
  const opening = kernel!.kernel.methods.get('conversation.open')!({ id }, connection)
  await connection.close()
  await opening
  expect(connection.subscribed(id as number)).toBe(false)
  expect(kernel!.kernel.liveObservations).toBe(0)
})
