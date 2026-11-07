import { readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs'

/** How long a guard file may exist before it is treated as abandoned, in milliseconds. */
const GUARD_TTL_MS = 10_000
/** How many times a starter may lose the guard race before it gives up. */
const ATTEMPTS = 6

export type LockOptions = {
  /** Whether a pid is alive; the real one signals 0. */
  isAlive?: (pid: number) => boolean
  /** For a test: run between the guard being taken and the lock being re-read. */
  afterGuard?: () => void
}

/**
 * One kernel per data directory: a second one would open a second connection to the same SQLite file, and
 * a synchronous `busy_timeout` wait there blocks the whole event loop.
 *
 * The lock is `kernel.lock`, holding a pid, and **the path is never left empty**: a stale lock is
 * replaced by an atomic `rename` while holding `kernel.lock.guard`, never by `unlink` followed by
 * `create`. Deleting it would open a window in which a third starter's exclusive create succeeds, and
 * the recovering process would then overwrite that third process's lock — two kernels, one database.
 *
 * The sequence:
 *
 * 1. `wx` (exclusive create) — the common case, and the only case when no kernel died.
 * 2. The file exists: read its pid. Alive → another kernel owns this directory; refuse.
 * 3. Dead: take `kernel.lock.guard` with `wx`. Two starters cannot both hold it, so only one performs
 *    the takeover.
 * 4. Re-read `kernel.lock` **while holding the guard**. If it now names a different live process, that
 *    process won the race after our first read: release the guard and refuse. Otherwise write our pid into
 *    the lock by renaming a fresh file over it — atomic, so a concurrent reader sees either the old pid or
 *    ours, never nothing.
 * 5. Release the guard.
 *
 * A guard left by a process that died (or one older than `GUARD_TTL_MS`) is removed and the attempt
 * repeats; the `wx` create after the removal still lets only one starter through.
 */
export function takeLock(path: string, options: LockOptions = {}): void {
  const alive = options.isAlive ?? isAlive
  const guard = `${path}.guard`
  for (let attempt = 0; attempt < ATTEMPTS; attempt++) {
    if (claim(path)) return
    const owner = readPid(path)
    if (owner !== undefined && alive(owner)) {
      throw new Error(`another kernel (pid ${owner}) is using this data directory`)
    }
    if (!claim(guard)) {
      // Somebody else is taking the lock over. Only a dead or abandoned guard may be cleared.
      if (!stale(guard, alive)) {
        throw new Error('another kernel is starting in this data directory')
      }
      rmSync(guard, { force: true })
      continue
    }
    try {
      options.afterGuard?.()
      const current = readPid(path)
      if (current !== undefined && current !== owner && alive(current)) {
        throw new Error(`another kernel (pid ${current}) is using this data directory`)
      }
      // Replace atomically: the path always holds a pid, so no exclusive create can slip in.
      const fresh = `${path}.${process.pid}.new`
      writeFileSync(fresh, String(process.pid))
      try {
        renameSync(fresh, path)
      } catch (error) {
        rmSync(fresh, { force: true })
        throw error
      }
      return
    } finally {
      rmSync(guard, { force: true })
    }
  }
  throw new Error('could not take the data directory lock')
}

/** Drop the lock, but only if this process owns it — or its owner is gone, as after a SIGKILL. */
export function releaseLock(path: string, options: LockOptions = {}): void {
  const owner = readPid(path)
  const alive = options.isAlive ?? isAlive
  if (owner !== undefined && owner !== process.pid && alive(owner)) return
  rmSync(path, { force: true })
}

function claim(path: string): boolean {
  try {
    writeFileSync(path, String(process.pid), { flag: 'wx' })
    return true
  } catch (error) {
    if ((error as { code?: string }).code === 'EEXIST') return false
    throw error
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

/** A guard is stale when its owner is gone, or when it is older than any takeover could reasonably take. */
function stale(guard: string, alive: (pid: number) => boolean): boolean {
  const owner = readPid(guard)
  if (owner !== undefined && !alive(owner)) return true
  try {
    return Date.now() - statSync(guard).mtimeMs > GUARD_TTL_MS
  } catch {
    // Gone already: the next attempt may claim it.
    return true
  }
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}
