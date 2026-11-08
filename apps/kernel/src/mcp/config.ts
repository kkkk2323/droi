import { createHash } from 'node:crypto'
import { chmodSync, mkdirSync, readFileSync, renameSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'

/**
 * `mcp.json`: `{"mcpServers": {"<name>": {…}}}`, the shape every MCP client reads. An entry is a command for
 * stdio (`command`, `args`, `env`, `cwd`) or a URL for streamable HTTP (`url`, `headers`), switched off by
 * `"disabled": true` (Factory's word) or `"enabled": false`. Fields this file does not know, such as an `oauth`
 * block another client keeps, are kept as they are when the Kernel writes the file.
 */
export type RawEntry = Record<string, unknown>

export type ServerConfig =
  | {
      transport: 'stdio'
      command: string
      args: string[]
      env: Record<string, string>
      cwd?: string
    }
  | { transport: 'http'; url: string; headers: Record<string, string> }

export type ParsedEntry = { config?: ServerConfig; enabled: boolean; error?: string }

export type ConfigFile = {
  entries: Record<string, RawEntry>
  /** Why the file could not be read; a missing file is no error. */
  error?: string
}

/** A read of the file, cached by its size and modification time. */
type Cached = { key: string; file: ConfigFile }
const cache = new Map<string, Cached>()

export function readConfig(path: string): ConfigFile {
  let key: string
  try {
    const stat = statSync(path)
    key = `${stat.size}:${stat.mtimeMs}`
  } catch {
    cache.delete(path)
    return { entries: {} }
  }
  const cached = cache.get(path)
  if (cached?.key === key) return cached.file
  const file = parseFile(path)
  cache.set(path, { key, file })
  return file
}

function parseFile(path: string): ConfigFile {
  let parsed: unknown
  try {
    parsed = JSON.parse(readFileSync(path, 'utf8'))
  } catch (cause) {
    return {
      entries: {},
      error: `${path}: ${cause instanceof Error ? cause.message : String(cause)}`,
    }
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    return { entries: {}, error: `${path}: not a JSON object` }
  }
  const servers = (parsed as Record<string, unknown>)['mcpServers']
  if (servers === undefined) return { entries: {} }
  if (typeof servers !== 'object' || servers === null || Array.isArray(servers)) {
    return { entries: {}, error: `${path}: mcpServers is not an object` }
  }
  const entries: Record<string, RawEntry> = {}
  for (const [name, entry] of Object.entries(servers as Record<string, unknown>)) {
    if (typeof entry === 'object' && entry !== null && !Array.isArray(entry))
      entries[name] = entry as RawEntry
  }
  return { entries }
}

/** Whether an entry only switches a server of the same name on or off, adding nothing that runs. */
export function onlySwitches(entry: RawEntry): boolean {
  return Object.keys(entry).every((key) => key === 'enabled' || key === 'disabled')
}

export function enabledOf(entry: RawEntry): boolean {
  return entry['enabled'] !== false && entry['disabled'] !== true
}

export function parseEntry(entry: RawEntry): ParsedEntry {
  const enabled = enabledOf(entry)
  const type = String(entry['type'] ?? entry['transport'] ?? '')
  const url = entry['url']
  const command = entry['command']
  if (type === 'sse')
    return { enabled, error: 'SSE servers are not supported; use streamable HTTP' }
  if (typeof url === 'string' && url !== '' && type !== 'stdio') {
    return { enabled, config: { transport: 'http', url, headers: stringMap(entry['headers']) } }
  }
  if (typeof command === 'string' && command !== '') {
    const args = Array.isArray(entry['args']) ? entry['args'].map(String) : []
    const cwd = typeof entry['cwd'] === 'string' && entry['cwd'] !== '' ? entry['cwd'] : undefined
    return {
      enabled,
      config: {
        transport: 'stdio',
        command,
        args,
        env: stringMap(entry['env']),
        ...(cwd === undefined ? {} : { cwd }),
      },
    }
  }
  return { enabled, error: 'neither a command nor a url' }
}

function stringMap(value: unknown): Record<string, string> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return {}
  const out: Record<string, string> = {}
  for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
    if (item !== undefined && item !== null) out[key] = String(item)
  }
  return out
}

/**
 * What a repository's server would run, as a fingerprint: the SHA-256 of its config with sorted keys. Trust is
 * given to this exact config, so a changed command, argument, variable or URL is a new question.
 */
export function fingerprint(config: ServerConfig): string {
  return createHash('sha256').update(stable(config)).digest('hex')
}

function stable(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stable).join(',')}]`
  if (typeof value === 'object' && value !== null) {
    const record = value as Record<string, unknown>
    return `{${Object.keys(record)
      .sort()
      .filter((key) => record[key] !== undefined)
      .map((key) => `${JSON.stringify(key)}:${stable(record[key])}`)
      .join(',')}}`
  }
  return JSON.stringify(value)
}

/**
 * Change one entry of the global file and write it whole: through a temporary file and a rename, so a reader
 * never sees half of it, and readable by its owner only, since an entry can carry a token in its headers.
 */
export function writeEntry(path: string, name: string, entry: RawEntry | undefined): void {
  let document: Record<string, unknown> = {}
  try {
    const parsed = JSON.parse(readFileSync(path, 'utf8')) as unknown
    if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
      document = parsed as Record<string, unknown>
    } else {
      throw new Error(`${path} is not a JSON object`)
    }
  } catch (cause) {
    // A file that is there but cannot be read is the user's: it is not replaced by one that drops it.
    if ((cause as NodeJS.ErrnoException).code !== 'ENOENT') throw cause
  }
  const servers = { ...((document['mcpServers'] as Record<string, unknown> | undefined) ?? {}) }
  if (entry === undefined) delete servers[name]
  else servers[name] = entry
  document = { ...document, mcpServers: servers }
  mkdirSync(dirname(path), { recursive: true })
  const temporary = join(dirname(path), `.mcp.json.${process.pid}.${Date.now()}`)
  writeFileSync(temporary, `${JSON.stringify(document, null, 2)}\n`, { mode: 0o600 })
  try {
    chmodSync(temporary, 0o600)
  } catch {
    // Windows has no modes to set.
  }
  renameSync(temporary, path)
  cache.delete(path)
}
