import { existsSync } from 'node:fs'
import { homedir as osHomedir } from 'node:os'
import { delimiter, join } from 'node:path'

export interface LocateDroidOptions {
  /** Explicit path from Desktop Shell settings. */
  override?: string | null
  pathEnv?: string
  homedir?: string
  exists?: (path: string) => boolean
}

const BINARY = process.platform === 'win32' ? 'droid.exe' : 'droid'

/** Find the `droid` executable: settings override, then PATH, then ~/.local/bin. */
export function locateDroid(options: LocateDroidOptions = {}): string | null {
  const exists = options.exists ?? existsSync
  const pathEnv = options.pathEnv ?? process.env['PATH'] ?? ''
  const homedir = options.homedir ?? osHomedir()

  if (options.override && exists(options.override)) return options.override

  for (const dir of pathEnv.split(delimiter).filter(Boolean)) {
    const candidate = join(dir, BINARY)
    if (exists(candidate)) return candidate
  }

  const local = join(homedir, '.local', 'bin', BINARY)
  return exists(local) ? local : null
}
