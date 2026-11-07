import { readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs'

/**
 * How long a **fence** may sit unreadable before it counts as abandoned, in milliseconds. A fence is held
 * for one read and one unlink, so anything older than a blink belongs to a process that died mid-way.
 */
const FENCE_ABANDONED_MS = 500
/** How many times a starter may lose a race before it gives up. */
const ATTEMPTS = 6

/** The data directory is taken. Its own error, so the process can report a refusal rather than a crash. */
export class LockError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'LockError'
  }
}

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
 * The lock is `kernel.lock`, holding a pid, and **the path is never left empty**: a stale lock is replaced
 * by an atomic `rename` while holding `kernel.lock.guard`, never by `unlink` followed by `create`. Deleting
 * it would open a window in which a third starter's exclusive create succeeds, and the recovering process
 * would then overwrite that third process's lock — two kernels, one database.
 *
 * The sequence:
 *
 * 1. `wx` (exclusive create) — the common case, and the only case when no kernel died.
 * 2. The file exists: read its pid. Alive → another kernel owns this directory; refuse.
 * 3. Dead: take `kernel.lock.guard` with `wx`. Two starters cannot both hold it, so only one performs the
 *    takeover. A guard whose owner is **gone** is removed first, and only under the fence that makes that
 *    removal exclusive (see `clearGuard`).
 * 4. Re-read `kernel.lock` **while holding the guard**. If it now names a different live process, that
 *    process won the race after our first read: release the guard and refuse. Otherwise write our pid into
 *    the lock by renaming a fresh file over it — atomic, so a concurrent reader sees either the old pid or
 *    ours, never nothing.
 * 5. Release the guard.
 *
 * Every removal—a dead guard, its fence, our own guard—is identity-checked. An observation ("its owner was
 * dead a moment ago") is never the authorization to unlink a path: a starter that re-created the guard in
 * the meantime would lose it, and two starters would then take the lock over at once.
 */
export function takeLock(path: string, options: LockOptions = {}): void {
  const alive = options.isAlive ?? isAlive
  const guard = `${path}.guard`
  for (let attempt = 0; attempt < ATTEMPTS; attempt++) {
    if (claim(path)) return
    const owner = readPid(path)
    if (owner === undefined) continue
    if (alive(owner)) {
      throw new LockError(`another kernel (pid ${owner}) is using this data directory`)
    }
    if (!takeGuard(guard, alive)) {
      // An alive starter holds the guard: it is taking this same dead lock over, and will own it.
      throw new LockError('another kernel is starting in this data directory')
    }
    try {
      options.afterGuard?.()
      const current = readPid(path)
      if (current !== undefined && current !== owner && alive(current)) {
        throw new LockError(`another kernel (pid ${current}) is using this data directory`)
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
      releaseGuard(guard, process.pid)
    }
  }
  throw new LockError('could not take the data directory lock')
}

/** Drop the lock, but only if this process owns it — or its owner is gone, as after a SIGKILL. */
export function releaseLock(path: string, options: LockOptions = {}): void {
  const owner = readPid(path)
  const alive = options.isAlive ?? isAlive
  if (owner !== undefined && owner !== process.pid && alive(owner)) return
  rmSync(path, { force: true })
}

/**
 * Take the guard, or say that somebody alive holds it.
 *
 * Removal happens only for a guard whose owner is gone: a live holder is mid-takeover, and deleting its
 * guard would let a second starter rename over the lock in the same instant.
 */
function takeGuard(guard: string, alive: (pid: number) => boolean): boolean {
  for (let attempt = 0; attempt < ATTEMPTS; attempt++) {
    if (claim(guard)) return true
    const owner = readPid(guard)
    if (owner === undefined) continue
    if (alive(owner)) return false
    clearGuard(guard, owner, alive)
  }
  return false
}

/**
 * Remove a guard whose owner is gone — and only that one.
 *
 * Reading the guard and unlinking it are two calls, so a guard that a racing starter re-created in between
 * would be deleted by mistake, and two starters could then take the lock over at once. The removal is
 * therefore fenced: while `kernel.lock.guard.break` exists, no other starter removes a guard, and the guard
 * is re-read **after** the fence is taken. Nothing else unlinks a guard, and a guard is only ever created
 * with `wx`, so the file that is finally unlinked is the one that was judged dead.
 */
function clearGuard(guard: string, expected: number, alive: (pid: number) => boolean): void {
  const fence = `${guard}.break`
  if (!claim(fence)) {
    const holder = readPid(fence)
    // A live holder is already removing this guard; wait for it, and look again on the next attempt.
    if (holder !== undefined && alive(holder)) return
    // Its owner died inside the critical section. The fence is short-lived, so waiting it out is short
    // too, and it is the only way to be sure nobody is still filling it in.
    if (!waitForAbandoned(fence, FENCE_ABANDONED_MS)) return
    rmSync(fence, { force: true })
    return
  }
  try {
    const current = readPid(guard)
    // Re-read under the fence: a different file means another starter already cleared this generation, and
    // a live one is never ours to remove.
    if (current !== expected || alive(current)) return
    rmSync(guard, { force: true })
  } finally {
    releaseGuard(fence, process.pid)
  }
}

/** Drop a guard or a fence, but only while it is still ours. */
function releaseGuard(path: string, pid: number): void {
  const owner = readPid(path)
  if (owner !== undefined && owner !== pid) return
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

/**
 * Block until a file with no readable owner is older than `limitMs`, or that long at most.
 *
 * Startup is single-threaded and nothing else is running yet, so the wait is a synchronous sleep rather
 * than a promise; it is bounded by a few hundred milliseconds in the one case that reaches it.
 */
function waitForAbandoned(path: string, limitMs: number): boolean {
  const deadline = Date.now() + limitMs
  for (;;) {
    let age: number
    try {
      age = Date.now() - statSync(path).mtimeMs
    } catch {
      // Gone already: there is nothing to wait for.
      return true
    }
    if (age > limitMs) return true
    if (Date.now() >= deadline + limitMs) return false
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, Math.max(1, limitMs - age + 1))
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
