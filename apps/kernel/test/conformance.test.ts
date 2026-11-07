import { describe, expect, it } from 'bun:test'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { registerStorageConformance } from '@earendil-works/pi-durable/testing'
import { SqliteStorage } from '@earendil-works/pi-durable/storage/sqlite'
import { BunSqliteDatabase } from '../src/storage/facade.ts'

/** Pi's own storage suite against the kernel's SQLite facade: 32 cases, every transaction semantic. */
registerStorageConformance(
  // Bun's `expect` is an object, not a function, so it is adapted rather than passed through.
  { describe, it, expect: ((actual: unknown) => expect(actual)) as never },
  'bun:sqlite facade',
  async (use) => {
    const dir = mkdtempSync(join(tmpdir(), 'kernel-conf-'))
    const db = new BunSqliteDatabase(join(dir, 'kernel.sqlite'))
    const storage = await SqliteStorage.open(db, { prefix: 'pi_' })
    try {
      await use(storage)
    } finally {
      await storage.close({ signal: new AbortController().signal } as never).catch(() => undefined)
      await db.close().catch(() => undefined)
      // Windows releases the SQLite files a moment after close: deleting without a retry throws EBUSY.
      try {
        rmSync(dir, { recursive: true, force: true, maxRetries: 20, retryDelay: 50 })
      } catch {
        // A leftover temp directory is not a test failure.
      }
    }
  },
)
