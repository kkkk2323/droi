// What a Session is, from the files the Daemon keeps for it. The Daemon starts
// the Memory Server in its own directory, not the Session's, and tells it only
// the Session id (in each tool call's `_meta`); the Session's transcript opens
// with a session_start line that carries the cwd, and its tags sit in the
// settings file beside it.
import { closeSync, existsSync, openSync, readdirSync, readFileSync, readSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { isMemorySession } from '@droi/daemon-layer/memory-session'

/** ~/.factory/sessions, or under FACTORY_HOME_OVERRIDE as the Shell and the Daemon honour it. */
export function sessionsDir(env: NodeJS.ProcessEnv = process.env): string {
  return join(env['FACTORY_HOME_OVERRIDE'] ?? join(homedir(), '.factory'), 'sessions')
}

const FIRST_LINE_BYTES = 64 * 1024

export function readSessionWorkspace(file: string): string | null {
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

/** The Session's settings file, which the Daemon keeps beside its transcript. */
export function sessionSettingsFile(transcriptFile: string): string {
  return transcriptFile.replace(/\.jsonl$/, '.settings.json')
}

// The Scratch Workspace tag (ADR 0008) as packages/daemon-layer/src/sessions.ts
// names it; that module pulls in React, which neither Memory entry bundles.
const SCRATCH_TAG = 'droi.scratch'

/** The Session's tags from its settings file; none when the file is absent or unreadable. */
function sessionTags(transcriptFile: string): Array<{ name: string }> {
  try {
    const parsed = JSON.parse(readFileSync(sessionSettingsFile(transcriptFile), 'utf8')) as {
      tags?: unknown
    }
    return Array.isArray(parsed.tags) ? (parsed.tags as Array<{ name: string }>) : []
  } catch {
    return []
  }
}

/** Whether the transcript belongs to a Memory Session, from the tags in its settings file. */
export function isMemorySessionTranscript(transcriptFile: string): boolean {
  return isMemorySession(sessionTags(transcriptFile))
}

/**
 * Whether the transcript belongs to a Session in a Scratch Workspace. Such a
 * Session has no Project Memory: its folder is made for it and trashed with it,
 * so nothing saved under that path would be found again.
 */
export function isScratchSessionTranscript(transcriptFile: string): boolean {
  return sessionTags(transcriptFile).some((t) => t.name === SCRATCH_TAG)
}

/** Looks the Session's transcript up in every Workspace folder the Daemon keeps; null when absent. */
export function findSessionTranscript(dir: string, sessionId: string): string | null {
  if (!/^[A-Za-z0-9-]+$/.test(sessionId)) return null
  let folders: string[]
  try {
    folders = readdirSync(dir)
  } catch {
    return null
  }
  for (const folder of folders) {
    const file = join(dir, folder, `${sessionId}.jsonl`)
    if (existsSync(file)) return file
  }
  return null
}

export function findSessionWorkspace(dir: string, sessionId: string): string | null {
  const file = findSessionTranscript(dir, sessionId)
  return file ? readSessionWorkspace(file) : null
}
