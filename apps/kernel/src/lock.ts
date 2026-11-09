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
 * How long a start may wait for a lock that another start is *taking*, in milliseconds.
 *
 * The wait is needed, and it is not a nicety: opening the lock database is itself several writes — the
 * journal mode, the table, its seed row — and with a zero timeout simultaneous starters collide on those
 * and **all** give up, which is a failed start, not a refusal. Measured before this constant existed: one
 * round in twenty of three simultaneous starts ended with three refusals and no kernel at all.
 *
 * It must not be a long wait, and it must not be a retry loop: a kernel that genuinely holds the lock holds
 * it until it exits, so a bounded wait tells the two cases apart in one go — the queue behind a start that
 * is still taking the lock clears in milliseconds, and the queue behind a running kernel ends here.
 */
const SETUP_TIMEOUT_MS = 750

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
 * Three details make the lock real rather than nominal:
 *
 * - **Wait while *starting*, never while holding.** Setup runs with `busy_timeout = SETUP_TIMEOUT_MS`
 *   (750 ms): opening this database is several writes — the journal mode, the table, its seed row,
 *   `BEGIN EXCLUSIVE` — and simultaneous starters collide on them. With no wait at all, three at once
 *   sometimes ended with **all three refused** and no kernel running (measured: one round in twenty).
 *   The wait is bounded and is turned back to `0` as soon as the lock is held, so a kernel that really
 *   holds the directory still refuses the next start after the same 750 ms, and holding never waits.
 * - The holding statement is an `UPDATE` of an existing row, not an `INSERT` of a new one. SQLite writes
 *   the original page to the rollback journal when it first dirties a page, and that is what takes the
 *   lock; a row that stays in the page cache can leave the transaction holding nothing. Measured on Bun
 *   1.3.14 here: an `INSERT` into a table created in the same connection blocks nothing, the `UPDATE`
 *   blocks every other writer.
 * - A start that races a *running* kernel is refused with `LockError` — the one status a Supervisor may
 *   retry, because the directory may become free. Anything else that stops the lock from being taken (a
 *   file that is not a database, a directory it may not write) is a plain error: no retry can fix it, and
 *   reporting it as "another kernel" would send an operator looking for a process that does not exist.
 *
 * `kernel.pid` beside it is **diagnosis only** — the message a refused starter prints. Nothing reads it to
 * decide anything, so a stale one misleads a human for one line and nothing more.
 *
 * A database left by a crashed kernel needs no repair: the next start finds a hot journal and rolls it
 * back, which is the same thing SQLite does after any crash.
 *
 * **Never delete or replace this file while a kernel holds it.** The lock lives on the inode, so an
 * unlinked lock database lets the next starter create a fresh one and take it — two kernels, one data
 * directory. Nothing in the kernel removes it; a cleanup tool must not either.
 */
export function takeLock(path: string): Lock {
  const pidPath = `${path.slice(0, -'.lock.db'.length)}.pid`
  let db: Database | undefined
  try {
    db = new Database(path, { create: true })
    // A start that races another start waits for it; a start that races a *running kernel* waits the same
    // and is then refused. Both are one bounded wait, and neither is unbounded.
    db.exec(`PRAGMA busy_timeout = ${SETUP_TIMEOUT_MS}`)
    // A rollback journal, never WAL, and said so rather than inherited: this file is only ever locked, and
    // the invariant the lock depends on is the one below — that the first dirty page writes its original to
    // a rollback journal, which takes the file lock. A database an earlier build left in WAL mode would
    // keep that setting for ever, so it is normalized here (which itself needs the write lock, and so is
    // already a test of who owns the directory).
    db.exec('PRAGMA journal_mode = DELETE')
    db.exec(`CREATE TABLE IF NOT EXISTS ${TABLE} (owner INTEGER)`)
    db.exec(`INSERT INTO ${TABLE} (owner) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM ${TABLE})`)
    db.exec('PRAGMA locking_mode = EXCLUSIVE')
    db.exec('BEGIN EXCLUSIVE')
    db.exec(`UPDATE ${TABLE} SET owner = ${process.pid}`)
    // Held now, and held for the life of the process. Nothing else is written on this connection, so from
    // here on it must never wait: a bounded timeout exists to sort out the start, not to hide a would-be
    // deadlock behind it.
    db.exec('PRAGMA busy_timeout = 0')
  } catch (error) {
    try {
      db?.close()
    } catch {
      // A connection that failed to lock is not worth a second error.
    }
    // Only "somebody else holds it" is a refusal a Supervisor retries, and it exits `2`. Anything else —
    // a file that is not a database, a directory it may not write — cannot be fixed by starting again, so
    // it is a plain failure (exit `1`) that says what happened; calling it "another kernel" would send an
    // operator looking for a process that does not exist.
    const message = error instanceof Error ? error.message : String(error)
    if (!/SQLITE_BUSY|SQLITE_LOCKED|database is locked/i.test(message)) {
      throw new Error(
        `the data directory lock ${JSON.stringify(path)} cannot be used: ${message}. ` +
          'Delete it if no kernel is running in this data directory.',
        { cause: error },
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
