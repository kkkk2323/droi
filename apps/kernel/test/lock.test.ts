import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { existsSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { LockError, takeLock } from '../src/lock.ts'
import { tempData } from './helpers.ts'

let data: ReturnType<typeof tempData>
let path: string

beforeEach(() => {
  data = tempData()
  path = join(data.dir, 'kernel.lock.db')
})

afterEach(() => {
  data.cleanup()
})

/**
 * The process-level cases — a live owner, a killed one, three starters at once — are in
 * `regression-r4.test.ts`, where they run against real kernels. They cannot be here: SQLite's file locks
 * are `fcntl` locks, which belong to a process, so a second connection opened by *this* test would not be
 * a second owner at all. `bun test` also reaps a child this file leaves alive, which makes a unit test of
 * one a race with the runner.
 */
describe('the data directory lock', () => {
  test('a fresh directory is taken, and says who owns it', () => {
    const lock = takeLock(path)
    expect(existsSync(path)).toBe(true)
    expect(readFileSync(join(data.dir, 'kernel.pid'), 'utf8').trim()).toBe(String(process.pid))
    lock.release()
    expect(existsSync(join(data.dir, 'kernel.pid'))).toBe(false)
    // Released: the same directory may be taken again.
    takeLock(path).release()
  })

  test('releasing twice is not an error, and does not release the second owner', () => {
    const first = takeLock(path)
    first.release()
    const second = takeLock(path)
    first.release()
    expect(existsSync(join(data.dir, 'kernel.pid'))).toBe(true)
    second.release()
  })

  test('an empty lock file left by a crash is not a lock', () => {
    // R4b-2: the previous algorithm created a claim file and wrote its owner afterwards, so a SIGKILL
    // between the two refused every later start for ever. Here the file *is* the database: an empty one is
    // an empty database, and SQLite initializes it on the next open.
    writeFileSync(path, '')
    const started = Date.now()
    takeLock(path).release()
    expect(Date.now() - started).toBeLessThan(2000)
    expect(statSync(path).size).toBeGreaterThan(0)
  })

  test('an old PID text file beside it is ignored, not read as the lock', () => {
    // A pre-release build's lock was a file of text. No version reads it now, and it must not be mistaken
    // for the database either.
    writeFileSync(join(data.dir, 'kernel.lock'), '99999999')
    takeLock(path).release()
    expect(existsSync(join(data.dir, 'kernel.lock'))).toBe(true)
    rmSync(join(data.dir, 'kernel.lock'), { force: true })
  })

  test('a file that is not a database is a failure, not a refusal', () => {
    // "Another kernel is using this" is the one thing a Supervisor retries. A lock database that cannot
    // be opened is not that: starting again will fail the same way, and the message has to say what is
    // wrong instead of naming a process that does not exist.
    writeFileSync(path, 'this is not a SQLite database at all, it is a sentence')
    let failure: unknown
    try {
      takeLock(path)
    } catch (error) {
      failure = error
    }
    expect(failure).toBeInstanceOf(Error)
    expect(failure).not.toBeInstanceOf(LockError)
    expect((failure as Error).message).toContain('kernel.lock.db')
    expect((failure as Error).message).toContain('cannot be used')
  })
})
