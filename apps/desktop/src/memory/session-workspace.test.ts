import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { findSessionWorkspace, sessionsDir } from './session-workspace'

let dir: string

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'droi-sessions-'))
})

afterEach(() => rmSync(dir, { recursive: true, force: true }))

function sessionFile(folder: string, id: string, lines: unknown[]) {
  mkdirSync(join(dir, folder), { recursive: true })
  writeFileSync(join(dir, folder, `${id}.jsonl`), lines.map((l) => JSON.stringify(l)).join('\n'))
}

describe('findSessionWorkspace', () => {
  it('reads the cwd from the session_start line of the Session’s file', () => {
    sessionFile('-Users-dev-other', 'aaa', [{ type: 'session_start', cwd: '/Users/dev/other' }])
    sessionFile('-Users-dev-app', 'bbb', [
      { type: 'session_start', id: 'bbb', cwd: '/Users/dev/app' },
      { type: 'message', message: { role: 'user', content: [] } },
    ])
    expect(findSessionWorkspace(dir, 'bbb')).toBe('/Users/dev/app')
  })

  it('answers null for an unknown Session, a strange id or a file without a start line', () => {
    sessionFile('-x', 'ccc', [{ type: 'message' }])
    expect(findSessionWorkspace(dir, 'missing')).toBeNull()
    expect(findSessionWorkspace(dir, '../etc/passwd')).toBeNull()
    expect(findSessionWorkspace(dir, 'ccc')).toBeNull()
    expect(findSessionWorkspace(join(dir, 'nope'), 'ccc')).toBeNull()
  })

  it('follows FACTORY_HOME_OVERRIDE', () => {
    expect(sessionsDir({ FACTORY_HOME_OVERRIDE: '/tmp/f' })).toBe('/tmp/f/sessions')
  })
})
