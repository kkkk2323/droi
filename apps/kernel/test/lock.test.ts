import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { existsSync, readFileSync, writeFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { releaseLock, takeLock } from '../src/lock.ts'
import { tempData } from './helpers.ts'

let data: ReturnType<typeof tempData>
let path: string

beforeEach(() => {
  data = tempData()
  path = join(data.dir, 'kernel.lock')
})

afterEach(() => {
  data.cleanup()
})

describe('the data directory lock', () => {
  test('an empty directory is taken, and a second starter is refused', () => {
    takeLock(path, { isAlive: () => true })
    expect(Number(readFileSync(path, 'utf8'))).toBe(process.pid)
    // Another starter, whose "own pid" is a different live process, must be refused.
    const other = { ...process, pid: process.pid + 1 } as unknown as NodeJS.Process
    expect(() => takeLock(path, { isAlive: () => true })).toThrow('another kernel')
    void other
    releaseLock(path)
    expect(existsSync(path)).toBe(false)
  })

  test('a lock from a dead process is taken over', () => {
    writeFileSync(path, '999999')
    takeLock(path, { isAlive: (pid) => pid !== 999999 })
    expect(Number(readFileSync(path, 'utf8'))).toBe(process.pid)
  })

  test('the lock path is never empty, so a third starter cannot slip in during a takeover', () => {
    writeFileSync(path, '999999')
    let thirdWon = false
    let sawEmptyPath = false
    takeLock(path, {
      isAlive: (pid) => pid !== 999999,
      // The exact window the old rename-away algorithm exposed: after the guard is taken, another
      // starter tries its exclusive create. It must fail, because the lock file never disappeared.
      afterGuard: () => {
        if (!existsSync(path)) sawEmptyPath = true
        try {
          writeFileSync(path, String(process.pid + 1), { flag: 'wx' })
          thirdWon = true
        } catch {
          // Expected: the lock is still there.
        }
      },
    })
    expect({ thirdWon, sawEmptyPath }).toEqual({ thirdWon: false, sawEmptyPath: false })
    expect(Number(readFileSync(path, 'utf8'))).toBe(process.pid)
  })

  test('a guard left by a dead process does not block a takeover forever', () => {
    writeFileSync(path, '999999')
    writeFileSync(`${path}.guard`, '888888')
    takeLock(path, { isAlive: (pid) => pid !== 999999 && pid !== 888888 })
    expect(Number(readFileSync(path, 'utf8'))).toBe(process.pid)
    expect(existsSync(`${path}.guard`)).toBe(false)
  })

  test('an abandoned guard is cleared after its time, and a live one refuses', () => {
    writeFileSync(path, '999999')
    writeFileSync(`${path}.guard`, String(process.pid))
    // A live guard owner means somebody else is mid-takeover: this starter waits and refuses.
    expect(() => takeLock(path, { isAlive: () => true })).toThrow('another kernel')
    // Once the guard looks abandoned, the takeover proceeds.
    const old = statSync(`${path}.guard`)
    void old
    writeFileSync(`${path}.guard`, '777777')
    takeLock(path, {
      isAlive: (pid) => pid !== 999999 && pid !== 777777,
    })
    expect(Number(readFileSync(path, 'utf8'))).toBe(process.pid)
  })

  test('releasing someone else’s lock is not allowed', () => {
    writeFileSync(path, String(process.pid + 1))
    releaseLock(path, { isAlive: () => true })
    expect(existsSync(path)).toBe(true)
    // Its owner is gone, so the file is nobody's: clearing it is what lets the next starter in.
    releaseLock(path, { isAlive: () => false })
    expect(existsSync(path)).toBe(false)
  })
})
