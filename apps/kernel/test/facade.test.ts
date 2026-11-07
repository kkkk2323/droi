import { describe, expect, test } from 'bun:test'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { BunSqliteDatabase } from '../src/storage/facade.ts'

function temp(): { db: BunSqliteDatabase; close: () => Promise<void> } {
  const dir = mkdtempSync(join(tmpdir(), 'kernel-facade-'))
  const db = new BunSqliteDatabase(join(dir, 'k.sqlite'))
  return {
    db,
    close: async () => {
      await db.close()
      try {
        rmSync(dir, { recursive: true, force: true, maxRetries: 20, retryDelay: 50 })
      } catch {
        // Windows may hold the file a moment longer.
      }
    },
  }
}

describe('BunSqliteDatabase', () => {
  test('an int64 below 2^53 comes back exactly, as a number', async () => {
    const { db, close } = temp()
    try {
      await db.exec('CREATE TABLE t (v INTEGER)')
      await db.run('INSERT INTO t VALUES (?)', 9_007_199_254_740_991n as never)
      const row = await db.get<{ v: number }>('SELECT v FROM t')
      expect(row?.v).toBe(9_007_199_254_740_991)
      expect(typeof row?.v).toBe('number')
    } finally {
      await close()
    }
  })

  test('an int64 past 2^53 throws instead of rounding', async () => {
    const { db, close } = temp()
    try {
      await db.exec('CREATE TABLE t (v INTEGER)')
      await db.run('INSERT INTO t VALUES (?)', 9_007_199_254_740_993n as never)
      expect(db.get('SELECT v FROM t')).rejects.toThrow('outside the range')
    } finally {
      await close()
    }
  })

  test('a failed transaction rolls back and the next one still runs', async () => {
    const { db, close } = temp()
    try {
      await db.exec('CREATE TABLE t (v INTEGER)')
      await db
        .transaction(async (tx) => {
          await tx.run('INSERT INTO t VALUES (1)')
          throw new Error('nope')
        })
        .catch(() => undefined)
      const after = await db.all<{ v: number }>('SELECT v FROM t')
      expect(after).toEqual([])
      await db.transaction(async (tx) => {
        await tx.run('INSERT INTO t VALUES (2)')
      })
      expect(await db.all<{ v: number }>('SELECT v FROM t')).toEqual([{ v: 2 }])
    } finally {
      await close()
    }
  })

  test('statements run in call order, so a transaction never interleaves', async () => {
    const { db, close } = temp()
    try {
      await db.exec('CREATE TABLE t (v INTEGER)')
      await Promise.all([
        db.transaction(async (tx) => {
          await tx.run('INSERT INTO t VALUES (1)')
          await Bun.sleep(5)
          await tx.run('INSERT INTO t VALUES (2)')
        }),
        db.run('INSERT INTO t VALUES (3)'),
        db.run('INSERT INTO t VALUES (4)'),
      ])
      expect(
        (await db.all<{ v: number }>('SELECT v FROM t ORDER BY rowid')).map((r) => r.v),
      ).toEqual([1, 2, 3, 4])
    } finally {
      await close()
    }
  })
})
