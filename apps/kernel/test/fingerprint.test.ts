import { afterEach, beforeEach, expect, test } from 'bun:test'
import { join } from 'node:path'
import { fingerprintOf, sameFingerprint } from '../src/fingerprint.ts'
import { migrate } from '../src/indexer/schema.ts'
import { BunSqliteDatabase } from '../src/storage/facade.ts'
import { tempData } from './helpers.ts'

let data: ReturnType<typeof tempData>
beforeEach(() => {
  data = tempData()
})
afterEach(() => data.cleanup())

/** What a build before the hash stored: the normalized JSON itself. */
const legacy = (value: unknown) => JSON.stringify(value)

test('a fingerprint is a hash of a fixed size, however large the request', () => {
  const entries = Array.from({ length: 2000 }, (_, i) => ({
    kind: 'pi.user',
    text: 'x'.repeat(200) + i,
  }))
  const fingerprint = fingerprintOf({ entries })
  expect(fingerprint).toMatch(/^sha256:[0-9a-f]{64}$/)
  expect(fingerprintOf({ entries: [...entries] })).toBe(fingerprint)
  expect(fingerprintOf({ entries: entries.slice(1) })).not.toBe(fingerprint)
})

test('a fingerprint an older build stored as JSON still matches, and another request does not', () => {
  const request = { b: 2, a: [1, { d: 4, c: 3 }] }
  const stored = legacy({ a: [1, { c: 3, d: 4 }], b: 2 })
  expect(sameFingerprint(stored, fingerprintOf(request))).toBe(true)
  expect(sameFingerprint(stored, fingerprintOf({ ...request, b: 3 }))).toBe(false)
  expect(sameFingerprint(fingerprintOf(request), fingerprintOf(request))).toBe(true)
  expect(sameFingerprint(null, fingerprintOf(request))).toBe(false)
})

test("the index hashes an older build's fingerprints once, and they still match", async () => {
  const db = new BunSqliteDatabase(join(data.dir, 'old.sqlite'))
  await migrate(db)
  const big = { entries: Array.from({ length: 500 }, (_, i) => ({ text: 'y'.repeat(100) + i })) }
  // As a build before the hash left them.
  await db.run("DELETE FROM droi_meta WHERE key = 'index.fingerprintsHashed'")
  for (let i = 0; i < 40; i++) {
    await db.run(
      'INSERT INTO droi_import_keys (key, conversation_id, fingerprint, entry_ids) VALUES (?, ?, ?, ?)',
      `k${i}`,
      i,
      legacy({ ...big, i }),
      '[]',
    )
  }
  await db.run(
    'INSERT INTO droi_requests (conversation_id, request_id, fingerprint) VALUES (?, ?, ?)',
    1,
    'r',
    legacy({ input: 'hi' }),
  )
  await migrate(db)
  const rows = await db.all<{ key: string; fingerprint: string }>(
    'SELECT key, fingerprint FROM droi_import_keys ORDER BY rowid',
  )
  expect(rows).toHaveLength(40)
  for (const [i, row] of rows.entries()) {
    expect(row.fingerprint).toMatch(/^sha256:[0-9a-f]{64}$/)
    expect(sameFingerprint(row.fingerprint, fingerprintOf({ i, ...big }))).toBe(true)
  }
  const request = await db.get<{ fingerprint: string }>('SELECT fingerprint FROM droi_requests')
  expect(request?.fingerprint).toBe(fingerprintOf({ input: 'hi' }))
  await db.close()
})
