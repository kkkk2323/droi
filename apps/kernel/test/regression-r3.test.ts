import { test, expect } from 'bun:test'
import { join } from 'node:path'
import { Client, fauxCatalog, start, tempData, allEntries } from './helpers.ts'
import { context } from '../src/session/service.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'
import { Indexer } from '../src/indexer/index.ts'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'

test('R3 fork does not inherit submission requestId identity', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    faux.faux.setResponses([
      fauxAssistantMessage('parent answer'),
      fauxAssistantMessage('fork answer'),
    ])
    const parent = await client.call('conversation.submit', {
      id,
      input: 'same prompt',
      requestId: 'scoped-key',
    })
    const conversation = await kernel.harness.conversation(id as never, context)
    await conversation!.waitForIdle(context)
    const entries = await allEntries(client, id as number)
    const forked = await client.call('conversation.fork', { id, at: entries.at(-1)!.id })
    const result = await client.call('conversation.submit', {
      id: forked.conversationId,
      input: 'same prompt',
      requestId: 'scoped-key',
    })
    console.log('R3_FORK_REQUEST', { parent, result, child: forked.conversationId })
    expect(result.submissionId).not.toBe(parent.submissionId)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 restart after an ordinary fork does not fail UNIQUE(request_id)', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  let kernel: Awaited<ReturnType<typeof start>> | undefined = await start({ dir: data.dir, faux })
  let client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      requestId: 'parent-create-key',
      kind: 'chat',
      cwd: '/tmp',
      agent: { model: faux.model },
    })
    let at = 0
    await kernel.harness.commit(async (tx) => {
      at = (await tx.appendEntry(id as never, { kind: 'droi.note', data: { text: 'fork anchor' } }))
        .id as number
    }, context)
    const forked = await client.call('conversation.fork', { id, at })
    await client.call('conversation.list')
    client.close()
    await kernel.stop()
    kernel = undefined
    try {
      kernel = await start({ dir: data.dir, faux })
    } catch (error) {
      console.log('R3_FORK_RESTART_ERROR', String(error))
      throw error
    }
    client = new Client(kernel.port)
    const list = await client.call('conversation.list')
    expect((list.items as any[]).map((x) => x.id)).toContain(forked.conversationId)
  } finally {
    client.close()
    await kernel?.stop()
    data.cleanup()
  }
})

test('R3 fork catch-up must not move inherited FTS rows out of its parent', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  let kernel = await start({ dir: data.dir, faux })
  let client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    let at = 0
    await kernel.harness.commit(async (tx) => {
      at = (
        await tx.appendEntry(id as never, {
          kind: 'droi.note',
          data: { text: 'parentsearchneedle' },
        })
      ).id as number
    }, context)
    const forked = await client.call('conversation.fork', { id, at })
    client.close()
    await kernel.stop()
    kernel = await start({ dir: data.dir, faux })
    client = new Client(kernel.port)
    const list = await client.call('conversation.list', { query: 'parentsearchneedle' })
    console.log('R3_FORK_SEARCH', { parent: id, child: forked.conversationId, items: list.items })
    expect((list.items as any[]).map((x) => x.id)).toContain(id)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 metadata-only recovery retains createdAt as activity, not epoch zero', async () => {
  const data = tempData()
  const db = new BunSqliteDatabase(join(data.dir, 'probe.sqlite'))
  const storage = {
    async scanConversations() {
      return { items: [{ id: 2 }] }
    },
    async scanEntries() {
      return {
        items: [{ id: 8, conversationId: 2, kind: 'droi.meta', data: { createdAt: 123456789 } }],
      }
    },
  }
  const indexer = new Indexer(db, storage as never)
  try {
    await indexer.open()
    await indexer.catchUp()
    const row = await indexer.conversation(2)
    console.log('R3_META_ACTIVITY', row)
    expect(row!.last_activity_at).toBe(123456789)
  } finally {
    await indexer.close()
    await db.close()
    data.cleanup()
  }
})

test('R3 a persisted assistant entry advances activity while the kernel is live', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    const before = ((await client.call('conversation.list')).items as any[]).find(
      (x) => x.id === id,
    ).lastActivityAt
    await Bun.sleep(30)
    const timestamp = Date.now()
    await kernel.harness.commit(async (tx) => {
      await tx.appendEntry(id as never, {
        kind: 'pi.assistant',
        model: [{ ...fauxAssistantMessage('new work'), timestamp }],
      })
    }, context)
    const list = await client.call('conversation.list')
    const row = (list.items as any[]).find((x) => x.id === id)
    console.log('R3_LIVE_ACTIVITY', { before, timestamp, lastActivityAt: row.lastActivityAt })
    expect(row.lastActivityAt).toBeGreaterThan(before)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 independent facade transaction cannot join and roll back with another caller', async () => {
  const data = tempData()
  const db = new BunSqliteDatabase(join(data.dir, 'probe.sqlite'))
  let release!: () => void
  let entered!: () => void
  const inside = new Promise<void>((resolve) => (entered = resolve))
  const gate = new Promise<void>((resolve) => (release = resolve))
  try {
    await db.exec('CREATE TABLE proof(v INTEGER)')
    const outer = db
      .transaction(async (tx) => {
        await tx.run('INSERT INTO proof VALUES (1)')
        entered()
        await gate
        throw new Error('rollback first caller')
      })
      .catch(() => {})
    await inside
    const independent = db.transaction(async (tx) => {
      await tx.run('INSERT INTO proof VALUES (2)')
    })
    release()
    await Promise.all([outer, independent])
    const rows = await db.all('SELECT v FROM proof')
    console.log('R3_TRANSACTION_ROWS', rows)
    expect(rows).toEqual([{ v: 2 }])
  } finally {
    release?.()
    await db.close()
    data.cleanup()
  }
})

test('R3 every JSON-RPC method is gated by failure, including unwatch and unsupported', async () => {
  const data = tempData()
  const kernel = await start({ dir: data.dir, faux: fauxCatalog() })
  const client = new Client(kernel.port)
  try {
    await client.opened
    kernel.kernel.fail(new Error('injected final failure'))
    const result = []
    for (const method of [
      'harness.info',
      'conversation.unwatch',
      'model.complete',
      'mcp.set',
      'not.a.method',
    ]) {
      const answer = await client.call(method, { id: 2 }).then(
        (value) => ({ method, value, code: 0 }),
        (error) => ({ method, code: error.code }),
      )
      result.push(answer)
    }
    console.log('R3_FAILURE_SURFACE', result)
    expect(result.map((x) => x.code)).toEqual(result.map(() => -32010))
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 recovery of submission cache ignores inherited ancestor memos', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  let kernel = await start({ dir: data.dir, faux })
  let client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    faux.faux.setResponses([fauxAssistantMessage('done'), fauxAssistantMessage('child')])
    const first = await client.call('conversation.submit', {
      id,
      input: 'same',
      requestId: 'same-key',
    })
    const conversation = await kernel.harness.conversation(id as never, context)
    await conversation!.waitForIdle(context)
    const entries = await allEntries(client, id as number)
    const fork = await client.call('conversation.fork', { id, at: entries.at(-1)!.id })
    client.close()
    await kernel.stop()
    kernel = await start({ dir: data.dir, faux })
    client = new Client(kernel.port)
    const second = await client.call('conversation.submit', {
      id: fork.conversationId,
      input: 'same',
      requestId: 'same-key',
    })
    console.log('R3_FORK_CACHE', { first, second })
    expect(second.submissionId).not.toBe(first.submissionId)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 metadata contributes no messages to the model but remains in raw view', async () => {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const created = await client.call('conversation.create', {
      requestId: 'private-create-marker',
      agent: { model: faux.model },
    })
    faux.faux.setResponses([fauxAssistantMessage('fine')])
    await client.call('conversation.submit', {
      id: created.conversationId,
      input: 'hello',
      requestId: 'private-submit-marker',
    })
    const conversation = await kernel.harness.conversation(created.conversationId as never, context)
    await conversation!.waitForIdle(context)
    const view = await conversation!.context(context)
    const opened = await client.call('conversation.open', { id: created.conversationId })
    console.log('R3_META_VISIBLE', {
      entries: view.entries.map((x) => x.kind),
      messages: view.messages.map((x) => x.role),
    })
    expect(JSON.stringify(view.messages)).not.toContain('private-create-marker')
    expect(JSON.stringify(view.messages)).not.toContain('private-submit-marker')
    expect(JSON.stringify(opened)).toContain('droi.meta')
    expect(JSON.stringify(opened)).toContain('droi.request')
    const list = await client.call('conversation.list', { query: 'private-create-marker' })
    expect(list.items).toEqual([])
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 closing during replacement unsubscribe does not leave a viewer on a closed connection', async () => {
  const data = tempData()
  const kernel = await start({ dir: data.dir, faux: fauxCatalog() })
  const client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create')
    await client.call('conversation.open', { id })
    const connection = kernel.server.connections()[0]!
    const unsubscribe = connection.unsubscribe.bind(connection)
    connection.unsubscribe = async (conversationId: number) => {
      const result = await unsubscribe(conversationId)
      // Same sequence as the server's WebSocket close handler, at the awaited old-watch stop.
      await connection.close()
      kernel.kernel.releaseConnection(connection.id)
      return result
    }
    await kernel.kernel.methods.get('conversation.open')!({ id }, connection)
    console.log('R3_REPLACEMENT_CLOSE', {
      open: connection.open,
      subscribed: connection.subscribed(id as number),
      observations: kernel.kernel.liveObservations,
    })
    expect(connection.subscribed(id as number)).toBe(false)
    expect(kernel.kernel.liveObservations).toBe(0)
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 a failed index flush cannot later watermark past its discarded entries', async () => {
  const data = tempData()
  const db = new BunSqliteDatabase(join(data.dir, 'probe.sqlite'))
  const entries = [
    {
      id: 10,
      conversationId: 2,
      kind: 'pi.user',
      model: [{ role: 'user', content: 'lostindexneedle', timestamp: 10 }],
    },
    {
      id: 20,
      conversationId: 2,
      kind: 'pi.assistant',
      model: [{ role: 'assistant', content: 'later answer', timestamp: 20 }],
    },
  ]
  const storage = {
    async scanConversations() {
      return { items: [{ id: 2 }] }
    },
    async scanEntries(query: any) {
      return { items: entries.filter((x) => x.id >= (query.minEntryId ?? 0)) }
    },
  }
  let indexer = new Indexer(db, storage as never, { flushIntervalMs: 60000 })
  const transaction = db.transaction.bind(db)
  try {
    await indexer.open()
    await indexer.touch(2, 1, { createdAt: 1 })
    await indexer.flush()
    indexer.entry(entries[0] as never)
    db.transaction = async () => {
      throw new Error('one transient index write failure')
    }
    await indexer.flush().catch(() => {})
    db.transaction = transaction
    indexer.entry(entries[1] as never)
    await indexer.flush()
    await indexer.close()
    indexer = new Indexer(db, storage as never)
    await indexer.open()
    await indexer.catchUp()
    const found = await indexer.list({ text: 'lostindexneedle' })
    console.log('R3_FAILED_FLUSH_RECOVERY', { found: found.items, watermark: indexer.watermark(2) })
    expect(found.items.map((x) => x.id)).toContain(2)
  } finally {
    db.transaction = transaction
    await indexer.close()
    await db.close()
    data.cleanup()
  }
})

test('R3 the production index timer broadcasts index_failed on rejection', async () => {
  const data = tempData()
  const kernel = await start({ dir: data.dir, faux: fauxCatalog() })
  const client = new Client(kernel.port)
  const flush = kernel.indexer.flush.bind(kernel.indexer)
  try {
    await client.opened
    kernel.indexer.flush = async () => {
      throw new Error('timer probe rejection')
    }
    await client.call('conversation.create')
    const note = await client.waitFor(
      (n) => n.method === 'warning' && n.params.kind === 'index_failed',
      2000,
    )
    expect(note.params.message).toBe('timer probe rejection')
  } finally {
    kernel.indexer.flush = flush
    client.close()
    await kernel.stop()
    data.cleanup()
  }
})

test('R3 short search counts entries rather than the gap in Pi global IDs', async () => {
  const data = tempData()
  const db = new BunSqliteDatabase(join(data.dir, 'probe.sqlite'))
  const indexer = new Indexer(db, {} as never)
  try {
    await indexer.open()
    await indexer.touch(1, 1, { createdAt: 1 })
    await indexer.touch(2, 2, { createdAt: 2 })
    await indexer.flush()
    await db.run(
      "INSERT INTO droi_entries_fts(rowid,text,conversation) VALUES(1,'古老内存记录',1),(100000,'newest entry',2)",
    )
    const found = await indexer.list({ text: '内存' })
    console.log('R3_SPARSE_SHORT_SEARCH', { entryCount: 2, found: found.items })
    expect(found.items.map((x) => x.id)).toContain(1)
  } finally {
    await indexer.close()
    await db.close()
    data.cleanup()
  }
})
