import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import { join } from 'node:path'
import {
  Indexer,
  escapeLike,
  likeClauses,
  searchWords,
  SHORT_TERM_WINDOW,
} from '../src/indexer/index.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'
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

/** One conversation whose user message is `text`, so the index holds it. */
async function seed(text: string): Promise<number> {
  const created = await client.call('conversation.create', {
    agent: { model: faux.model, cwd: process.cwd() },
  })
  const id = created['conversationId'] as number
  faux.faux.appendResponses([fauxAssistantMessage('noted')])
  await client.call('conversation.submit', { id, input: text })
  const deadline = Date.now() + 10_000
  while (Date.now() < deadline) {
    const page = await client.call('conversation.entries', { id, limit: 50 })
    if ((page['items'] as unknown[]).length >= 2) break
    await Bun.sleep(20)
  }
  return id
}

async function search(text: string): Promise<number[]> {
  const page = await client.call('conversation.list', { query: text })
  return (page['items'] as { id: number }[]).map((item) => item.id)
}

describe('search', () => {
  test('a Chinese two-character word finds the conversation that holds it', async () => {
    const target = await seed('这里是内存里的摘要，说明上下文是怎么压缩的')
    await seed('an unrelated English conversation')
    expect(await search('内存')).toEqual([target])
    expect(await search('摘要')).toEqual([target])
    // A word that is not there stays absent, rather than matching every Chinese row.
    expect(await search('数据库')).toEqual([])
  })

  test('a Chinese phrase of three or more characters goes through the index', async () => {
    const target = await seed('修复 parseConfig 的空输入校验')
    await seed('内存里没有这个东西')
    expect(await search('空输入校验')).toEqual([target])
  })

  test('an English word is found inside a longer identifier, and case does not matter', async () => {
    const target = await seed('the helper parseConfigLocale is not exported')
    expect(await search('parseConfigLocale')).toEqual([target])
    expect(await search('PARSECONFIG')).toEqual([target])
    expect(await search('configLoc')).toEqual([target])
  })

  test('two words narrow each other', async () => {
    const target = await seed('alpha and beta live together')
    await seed('alpha alone')
    await seed('beta alone')
    expect(await search('alpha beta')).toEqual([target])
  })

  test('a word in the title alone still finds the conversation', async () => {
    const target = await seed('nothing remarkable in this input')
    await client.call('index.update', { id: target, title: '配置文件迁移' })
    expect(await search('配置')).toEqual([target])
  })

  test('LIKE wildcards in the query are literal, not wildcards', async () => {
    const target = await seed('the pattern a%b is odd')
    await seed('the pattern axb is ordinary')
    expect(await search('a%b')).toEqual([target])
    expect(await search('%')).toEqual([])
  })

  test('the query words keep a run of Chinese whole and split Latin text', () => {
    expect(searchWords('内存 摘要')).toEqual(['内存', '摘要'])
    expect(searchWords('  parse conf  ')).toEqual(['parse', 'conf'])
    expect(searchWords('「内存」')).toEqual(['内存'])
    expect(searchWords('   ')).toEqual([])
    expect(escapeLike('a%b_c\\d')).toBe('a\\%b\\_c\\\\d')
  })

  test('the query builder bounds a short term and lets a long one through', () => {
    const long = likeClauses(['空输入'])
    expect(long.clauses[0]).toBe("text LIKE ? ESCAPE '\\'")
    const short = likeClauses(['内存'])
    expect(short.clauses[0]).toContain('rowid >')
    expect(short.values).toContain(SHORT_TERM_WINDOW)
    expect(SHORT_TERM_WINDOW).toBe(50_000)
  })

  test('a short term is bounded in the query the kernel actually runs', async () => {
    // The helper and the list must agree: a test of \`likeClauses\` alone passes even when \`list()\` does
    // not call it, which is what happened before.
    const db = new BunSqliteDatabase(join(data.dir, 'bounded.sqlite'))
    const indexer = new Indexer(db, {} as never)
    try {
      await indexer.open()
      await indexer.touch(1, 1, { createdAt: 1 })
      await indexer.touch(2, 2, { createdAt: 2 })
      await indexer.flush()
      // One old row that matches, behind 50,002 newer rows that do not.
      await db.run(
        'INSERT INTO droi_entries_fts(rowid, text, conversation) VALUES(1, ?, 1)',
        '古老内存记录',
      )
      await db.run(
        'WITH RECURSIVE rows(id) AS (SELECT 2 UNION ALL SELECT id + 1 FROM rows WHERE id < 50002) ' +
          "INSERT INTO droi_entries_fts(rowid, text, conversation) SELECT id, 'newest row', 2 FROM rows",
      )
      const short = await indexer.list({ text: '内存' })
      expect(short.items.map((item) => item.id)).toEqual([])
      // A three-character term still reaches it: that is the index, not the bounded scan.
      const long = await indexer.list({ text: '古老内存' })
      expect(long.items.map((item) => item.id)).toEqual([1])
    } finally {
      await indexer.close()
      await db.close()
    }
  })
})
