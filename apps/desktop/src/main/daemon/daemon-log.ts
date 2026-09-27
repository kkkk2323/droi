// The Daemon's stdout and stderr go to one append-only log file so a crash can
// be read after the fact. The file rotates once to `<name>.old` when it grows
// past a megabyte, the way the Factory App keeps its daemon-stderr.log.
import { closeSync, mkdirSync, openSync, readSync, renameSync, statSync, unlinkSync } from 'node:fs'
import { dirname } from 'node:path'

export const DAEMON_LOG_MAX_BYTES = 1024 * 1024
/** How much of the end of the log a crash report shows. */
export const DAEMON_LOG_TAIL_BYTES = 2048

/**
 * Opens the log for appending, rotating it first when it is full. Returns the
 * file descriptor to hand to `spawn`'s stdio, or null when the log cannot be
 * opened (the Daemon then runs without one rather than not at all).
 */
export function openDaemonLog(path: string): number | null {
  try {
    mkdirSync(dirname(path), { recursive: true })
    rotateIfFull(path)
    return openSync(path, 'a')
  } catch {
    return null
  }
}

function rotateIfFull(path: string): void {
  let size: number
  try {
    size = statSync(path).size
  } catch {
    return
  }
  if (size <= DAEMON_LOG_MAX_BYTES) return
  const old = `${path}.old`
  try {
    unlinkSync(old)
  } catch {
    // Nothing to replace.
  }
  renameSync(path, old)
}

/** The last `bytes` of the log as text, or null when there is no log yet. */
export function readDaemonLogTail(path: string, bytes = DAEMON_LOG_TAIL_BYTES): string | null {
  let fd: number
  try {
    fd = openSync(path, 'r')
  } catch {
    return null
  }
  try {
    const { size } = statSync(path)
    const length = Math.min(bytes, size)
    const buffer = Buffer.alloc(length)
    readSync(fd, buffer, 0, length, size - length)
    const text = buffer.toString('utf8').trim()
    return text ? text : null
  } catch {
    return null
  } finally {
    closeSync(fd)
  }
}
