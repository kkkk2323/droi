// Which Workspace a Session runs in, from its session file. The Daemon starts
// the Memory Server in its own directory, not the Session's, and tells it only
// the Session id (in each tool call's `_meta`); the file the Daemon keeps for
// that Session opens with a session_start line that carries the cwd.
import { closeSync, existsSync, openSync, readdirSync, readSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

/** ~/.factory/sessions, or under FACTORY_HOME_OVERRIDE as the Shell and the Daemon honour it. */
export function sessionsDir(env: NodeJS.ProcessEnv = process.env): string {
  return join(env['FACTORY_HOME_OVERRIDE'] ?? join(homedir(), '.factory'), 'sessions')
}

const FIRST_LINE_BYTES = 64 * 1024

export function readSessionCwd(file: string): string | null {
  let fd: number
  try {
    fd = openSync(file, 'r')
  } catch {
    return null
  }
  try {
    const buffer = Buffer.alloc(FIRST_LINE_BYTES)
    const read = readSync(fd, buffer, 0, buffer.length, 0)
    const first = buffer.subarray(0, read).toString('utf8').split('\n', 1)[0] ?? ''
    const parsed = JSON.parse(first) as { type?: unknown; cwd?: unknown }
    return parsed.type === 'session_start' && typeof parsed.cwd === 'string' ? parsed.cwd : null
  } catch {
    return null
  } finally {
    closeSync(fd)
  }
}

/** Looks the Session's file up in every Workspace folder the Daemon keeps; null when absent. */
export function findSessionCwd(dir: string, sessionId: string): string | null {
  if (!/^[A-Za-z0-9-]+$/.test(sessionId)) return null
  let folders: string[]
  try {
    folders = readdirSync(dir)
  } catch {
    return null
  }
  for (const folder of folders) {
    const file = join(dir, folder, `${sessionId}.jsonl`)
    if (existsSync(file)) return readSessionCwd(file)
  }
  return null
}
