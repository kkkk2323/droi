import { Database } from 'bun:sqlite'
import { readFileSync, rmSync, writeFileSync } from 'node:fs'

/**
 * The data directory is taken. Its own error, so the process reports a refusal (status `2`) rather than a
 * crash: a Supervisor may retry it, and must not file it as a bug.
 */
export class LockError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'LockError'
  }
}

/** A held data-directory lock. Closing it — or the process dying — releases it. */
export type Lock = {
  readonly path: string
  /** Release the lock and the diagnosis file. Safe to call twice. */
  release(): void
}

/** The table the lock is held through. Its single row is the owner's PID, for `sqlite3` and `lsof`. */
const TABLE = 'droi_lock'

/**
 * One kernel per data directory, with a lock **the operating system owns**.
 *
 * `kernel.lock.db` is a SQLite database whose only job is to be locked. The first starter opens it
 * read-write and keeps a write transaction open for the life of the process; every other starter's first
 * write fails with `SQLITE_BUSY`, because SQLite's file locking is `fcntl` on Unix and `LockFileEx` on
 * Windows, and both are enforced across processes by the kernel. When a kernel exits — normally, by
 * SIGTERM, by `taskkill /F`, by a segfault — the kernel drops the locks, and the next starter's write
 * succeeds. **There is no PID to check, no staleness to judge, no guard to take over and no waiting**: the
 * three earlier designs (a PID file, then a rename claim, then a guard, then a fence) each moved the same
 * check-then-delete race one layer outward, and this one has nothing to race over.
 *
 * Two details make the lock real rather than nominal:
 *
 * - `busy_timeout = 0` asks for no wait at all. A start either takes the directory now or is refused now;
 *   a retry loop here would turn a supervisor's restart storm into a queue of stuck processes.
 * - The holding statement is an `UPDATE` of an existing row, not an `INSERT` of a new one. SQLite writes
 *   the original page to the rollback journal when it first dirties a page, and that is what takes the
 *   lock; a row that stays in the page cache can leave the transaction holding nothing. Measured on Bun
 *   1.3.14 here: an `INSERT` into a table created in the same connection blocks nothing, the `UPDATE`
 *   blocks every other writer.
 *
 * `kernel.pid` beside it is **diagnosis only** — the message a refused starter prints. Nothing reads it to
 * decide anything, so a stale one misleads a human for one line and nothing more.
 *
 * A database left by a crashed kernel needs no repair: the next start finds a hot journal and rolls it
 * back, which is the same thing SQLite does after any crash.
 */
export function takeLock(path: string): Lock {
  const pidPath = `${path.slice(0, -'.lock.db'.length)}.pid`
  let db: Database | undefined
  try {
    db = new Database(path, { create: true })
    db.exec('PRAGMA busy_timeout = 0')
    db.exec(`CREATE TABLE IF NOT EXISTS ${TABLE} (owner INTEGER)`)
    db.exec(`INSERT INTO ${TABLE} (owner) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM ${TABLE})`)
    db.exec('PRAGMA locking_mode = EXCLUSIVE')
    db.exec('BEGIN EXCLUSIVE')
    db.exec(`UPDATE ${TABLE} SET owner = ${process.pid}`)
  } catch (error) {
    try {
      db?.close()
    } catch {
      // A connection that failed to lock is not worth a second error.
    }
    // Only "somebody else holds it" is a refusal a Supervisor retries. Anything else — a file that is not
    // a database, a directory it may not write — is a start that cannot succeed, and says why.
    const message = error instanceof Error ? error.message : String(error)
    if (!/SQLITE_BUSY|SQLITE_LOCKED|database is locked/i.test(message)) {
      throw new LockError(
        `the data directory lock ${JSON.stringify(path)} could not be taken: ${message}`,
      )
    }
    const owner = readPid(pidPath)
    throw new LockError(
      owner === undefined
        ? 'another kernel is using this data directory'
        : `another kernel (pid ${owner}) is using this data directory`,
    )
  }
  // Only now, with the lock held, is this process the owner those words are about.
  try {
    writeFileSync(pidPath, String(process.pid))
  } catch {
    // A diagnosis file nobody could write is not a reason to refuse a locked start.
  }
  let released = false
  return {
    path,
    release: () => {
      if (released) return
      released = true
      // Closing rolls the transaction back and drops the OS locks with it.
      try {
        db?.close()
      } catch {
        // Nothing left to release.
      }
      try {
        rmSync(pidPath, { force: true })
      } catch {
        // Same: the file is advice, not state.
      }
    },
  }
}

function readPid(path: string): number | undefined {
  try {
    const pid = Number(readFileSync(path, 'utf8').trim())
    return Number.isInteger(pid) && pid > 0 ? pid : undefined
  } catch {
    return undefined
  }
}
