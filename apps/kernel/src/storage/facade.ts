import { Database } from 'bun:sqlite'
import type { SqliteDatabase, SqliteExecutor, SqliteValue } from '@earendil-works/chord/sqlite'

/**
 * The SQLite `bun:sqlite` hands back for an INTEGER column, made safe.
 *
 * `{ safeIntegers: true }` makes the driver return BigInt instead of a double, so an int64 no longer
 * rounds silently — but Pi's storage is written against `node:sqlite`, which returns numbers, and it
 * puts record IDs into JSON documents, where a BigInt cannot go. So each row is converted back to
 * numbers as it leaves the driver, and a value outside the range a double holds exactly throws.
 *
 * That throw is the point. Without `safeIntegers` a stored 2^53+1 already comes back as 2^53: the loss
 * happens inside the driver, and no check of ours can see it afterwards. Pi mints IDs from a counter
 * that starts at 1 (`SqliteStorage.mintId()`, which itself refuses past `Number.isSafeInteger`), so in
 * practice the check never fires; it guards a corruption or a future change instead.
 */
const MAX_SAFE = BigInt(Number.MAX_SAFE_INTEGER)
const MIN_SAFE = -MAX_SAFE

function revive<T>(value: T): T {
  switch (typeof value) {
    case 'bigint':
      if (value > MAX_SAFE || value < MIN_SAFE) {
        throw new RangeError(
          `SQLite integer ${value} is outside the range a JavaScript number holds exactly`,
        )
      }
      return Number(value) as T
    case 'object': {
      if (value === null || value instanceof Uint8Array) return value
      if (Array.isArray(value)) return value.map(revive) as T
      const out: Record<string, unknown> = {}
      for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
        out[key] = revive(item)
      }
      return out as T
    }
    default:
      return value
  }
}

/** Statement execution on one connection, without the queue: what a transaction callback gets. */
class Direct implements SqliteExecutor {
  constructor(protected db: Database) {}
  async exec(sql: string): Promise<void> {
    this.db.exec(sql)
  }
  async run(sql: string, ...params: SqliteValue[]): Promise<void> {
    this.db.query(sql).run(...(params as never[]))
  }
  async runCounted(sql: string, ...params: SqliteValue[]): Promise<number> {
    return Number(this.db.query(sql).run(...(params as never[])).changes)
  }
  async get<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T | undefined> {
    const row = this.db.query(sql).get(...(params as never[]))
    return row === null ? undefined : revive(row as T)
  }
  async all<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T[]> {
    return revive(this.db.query(sql).all(...(params as never[])) as T[])
  }
}

/**
 * The one SQLite connection the kernel opens, as Chord's `SqliteDatabase`.
 *
 * Three constraints, from the Windows measurements:
 *
 * - **One connection.** A second one makes the synchronous `busy_timeout` wait block the event loop
 *   (5.5 s measured), so the sidecar opens exactly one and the Host never opens the file.
 * - **`BEGIN IMMEDIATE`** for transactions: a deferred transaction that upgrades to a write can fail
 *   with `SQLITE_BUSY` after it has already read.
 * - **Serial order.** Every statement goes through one queue, so a transaction callback never
 *   interleaves with another statement.
 */
export class BunSqliteDatabase extends Direct implements SqliteDatabase {
  #tail: Promise<unknown> = Promise.resolve()

  constructor(path: string) {
    super(new Database(path, { create: true, safeIntegers: true }))
    this.db.exec('PRAGMA journal_mode = WAL')
    this.db.exec('PRAGMA synchronous = NORMAL')
    this.db.exec('PRAGMA busy_timeout = 5000')
  }

  #enqueue<T>(fn: () => T | Promise<T>): Promise<T> {
    const run = this.#tail.then(fn)
    this.#tail = run.then(
      () => undefined,
      () => undefined,
    )
    return run
  }

  override exec(sql: string) {
    return this.#enqueue(() => super.exec(sql))
  }
  override run(sql: string, ...params: SqliteValue[]) {
    return this.#enqueue(() => super.run(sql, ...params))
  }
  override runCounted(sql: string, ...params: SqliteValue[]) {
    return this.#enqueue(() => super.runCounted(sql, ...params))
  }
  override get<T extends object>(sql: string, ...params: SqliteValue[]) {
    return this.#enqueue(() => super.get<T>(sql, ...params))
  }
  override all<T extends object>(sql: string, ...params: SqliteValue[]) {
    return this.#enqueue(() => super.all<T>(sql, ...params))
  }

  transaction<T>(callback: (transaction: SqliteExecutor) => Promise<T>): Promise<T> {
    return this.#enqueue(async () => {
      this.db.exec('BEGIN IMMEDIATE')
      try {
        const result = await callback(new Direct(this.db))
        this.db.exec('COMMIT')
        return result
      } catch (error) {
        try {
          this.db.exec('ROLLBACK')
        } catch {
          // The original error stands; a failed rollback has nothing better to say.
        }
        throw error
      }
    })
  }

  async close(): Promise<void> {
    await this.#enqueue(() => this.db.close())
  }
}
