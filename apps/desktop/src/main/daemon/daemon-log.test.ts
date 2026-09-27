import { afterEach, describe, expect, test } from 'vitest'
import {
  closeSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
  writeSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DAEMON_LOG_MAX_BYTES, openDaemonLog, readDaemonLogTail } from './daemon-log'

describe('daemon log', () => {
  const dir = mkdtempSync(join(tmpdir(), 'droi-daemon-log-'))
  const path = join(dir, 'logs', 'daemon.log')

  afterEach(() => rmSync(dir, { recursive: true, force: true }))

  test('opens the log for appending, creating its folder', () => {
    const fd = openDaemonLog(path)
    expect(fd).not.toBeNull()
    writeSync(fd!, 'hello\n')
    closeSync(fd!)
    expect(readFileSync(path, 'utf8')).toBe('hello\n')
  })

  test('a full log is rotated to .old before the next Daemon writes', () => {
    openDaemonLog(path)
    writeFileSync(path, 'x'.repeat(DAEMON_LOG_MAX_BYTES + 1))
    const fd = openDaemonLog(path)
    writeSync(fd!, 'fresh\n')
    closeSync(fd!)
    expect(readFileSync(path, 'utf8')).toBe('fresh\n')
    expect(existsSync(`${path}.old`)).toBe(true)
  })

  test('the tail is the end of the log, or null without a log', () => {
    expect(readDaemonLogTail(path)).toBeNull()
    const fd = openDaemonLog(path)
    writeSync(fd!, 'first line\nlast line\n')
    closeSync(fd!)
    expect(readDaemonLogTail(path, 10)).toBe('last line')
    expect(readDaemonLogTail(path)).toBe('first line\nlast line')
  })
})
