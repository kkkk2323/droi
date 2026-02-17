import { execFileSync } from 'child_process'
import { existsSync } from 'fs'
import { join } from 'path'
import { homedir } from 'os'

export function resolveDroidPath(): string {
  const locator = process.platform === 'win32' ? 'where' : 'which'
  try {
    const resolved = execFileSync(locator, ['droid'], { stdio: ['ignore', 'pipe', 'ignore'] }).toString().trim()
    if (resolved) return resolved
  } catch { /* ignore */ }

  if (process.platform === 'darwin') {
    try {
      const resolved = execFileSync('/bin/zsh', ['-lc', 'which droid'], { stdio: ['ignore', 'pipe', 'ignore'] }).toString().trim()
      if (resolved) return resolved
    } catch { /* ignore */ }
  }

  const home = homedir()
  const candidates = [
    join(home, '.local', 'bin', 'droid'),
    join(home, '.cargo', 'bin', 'droid'),
    '/opt/homebrew/bin/droid',
    '/usr/local/bin/droid',
  ]
  for (const p of candidates) {
    if (existsSync(p)) return p
  }

  return 'droid'
}

