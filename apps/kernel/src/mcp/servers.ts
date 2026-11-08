import { createHash } from 'node:crypto'
import { homedir } from 'node:os'
import { join } from 'node:path'
import type { Context } from '@earendil-works/chord'
import { defineExtension, defineTool, section } from '@earendil-works/pi-durable'
import type { Extension, Registry, ToolRegistration } from '@earendil-works/pi-durable'
import {
  McpClient,
  StdioTransport,
  StreamableHttpTransport,
  toLlmContent,
  type AuthProvider,
  type McpTransport,
  type Tool,
} from '@earendil-works/pi-mcp'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import { setToolClass } from '../policy/decide.ts'
import { info, warn } from '../log.ts'
import {
  enabledOf,
  fingerprint,
  onlySwitches,
  parseEntry,
  readConfig,
  type ParsedEntry,
  type RawEntry,
  type ServerConfig,
} from './config.ts'

export type ServerStatus = 'connected' | 'connecting' | 'failed' | 'disabled' | 'untrusted'

/** One server as `mcp.list`, `mcp.set` and `mcp.changed` report it. */
export type WireServer = {
  name: string
  transport?: 'stdio' | 'http'
  status: ServerStatus
  enabled: boolean
  tools: string[]
  error: string | null
  repoRoot?: string
  trusted?: boolean
  command?: string
  args?: string[]
  url?: string
}

type Server = {
  key: string
  name: string
  /** Set for a server a repository's own file brings. */
  repoRoot?: string
  raw: RawEntry
  parsed: ParsedEntry
  status: ServerStatus
  error: string | null
  tools: Tool[]
  client?: McpClient
  /** Bumped by every connect and every close: an older attempt that finishes late does nothing. */
  generation: number
  retryDelay: number
  retry?: ReturnType<typeof setTimeout>
  extension: string
}

/** How long one connection attempt, and its first tool list, may take. */
const CONNECT_TIMEOUT_MS = 30_000
/** A tool call is a person's work: long, but not for ever. */
const CALL_TIMEOUT_MS = 30 * 60_000
const RETRY_FIRST_MS = 1000
const RETRY_MAX_MS = 30_000
/** The longest tool name a provider takes (OpenAI's limit). */
const MAX_TOOL_NAME = 64

export type McpServersOptions = {
  /** Where the global `mcp.json` is. */
  userDir: string
  db: BunSqliteDatabase
  registry: Registry
  notify: (method: string, payload: unknown) => void
  /** The credentials of an HTTP server that asks for authorization (mcp/oauth.ts). */
  authProvider?: (name: string, url: string) => AuthProvider | undefined
  version: string
  context: Context
}

/**
 * The MCP servers (design/m3-kernel.md §4): the global file's, and those each repository's
 * `.factory/mcp.json` brings, which run only once the user trusts that exact config.
 *
 * Every server is an extension of its own, named `mcp-…`. A conversation selects the global ones and its own
 * repository's (`prepare`), so it is never offered another repository's tools. An extension is installed while
 * its server is connected; a conversation that names one that is not is simply offered none of its tools.
 */
export class McpServers {
  readonly #options: McpServersOptions
  readonly #servers = new Map<string, Server>()
  /** Each repository's file as last read, and the servers it switched on or off by name. */
  readonly #repos = new Map<string, { file: unknown; switches: Map<string, boolean> }>()
  #globalFile: unknown
  #diagnostics: { path: string; message: string }[] = []
  #closed = false

  constructor(options: McpServersOptions) {
    this.#options = options
  }

  get #globalPath(): string {
    return join(this.#options.userDir, 'mcp.json')
  }

  async open(): Promise<void> {
    await this.#options.db.exec(`CREATE TABLE IF NOT EXISTS droi_mcp_trust (
      repo_root TEXT NOT NULL,
      name TEXT NOT NULL,
      fingerprint TEXT NOT NULL,
      at INTEGER NOT NULL,
      PRIMARY KEY (repo_root, name)
    )`)
    await this.refreshGlobal()
  }

  /** Read the global file again if it changed, and bring the servers in line with it. */
  async refreshGlobal(): Promise<void> {
    if (this.#closed) return
    const file = readConfig(this.#globalPath)
    if (file === this.#globalFile) return
    this.#globalFile = file
    this.#setDiagnostic(this.#globalPath, file.error)
    const seen = new Set<string>()
    for (const [name, raw] of Object.entries(file.entries)) {
      const key = `g:${name}`
      seen.add(key)
      await this.#upsert(key, name, undefined, raw)
    }
    for (const server of [...this.#servers.values()]) {
      if (server.repoRoot === undefined && !seen.has(server.key)) await this.#remove(server)
    }
    this.#changed()
  }

  /** Read a repository's file again if it changed: its servers, and the global ones it switches. */
  async refreshRepo(repoRoot: string): Promise<void> {
    if (this.#closed) return
    const path = join(repoRoot, '.factory', 'mcp.json')
    const file = readConfig(path)
    const known = this.#repos.get(repoRoot)
    if (known?.file === file) return
    const switches = new Map<string, boolean>()
    this.#repos.set(repoRoot, { file, switches })
    this.#setDiagnostic(path, file.error)
    const seen = new Set<string>()
    for (const [name, raw] of Object.entries(file.entries)) {
      if (onlySwitches(raw)) {
        switches.set(name, enabledOf(raw))
        continue
      }
      const key = `r:${repoRoot}:${name}`
      seen.add(key)
      await this.#upsert(key, name, repoRoot, raw)
    }
    for (const server of [...this.#servers.values()]) {
      if (server.repoRoot === repoRoot && !seen.has(server.key)) await this.#remove(server)
    }
    this.#changed()
  }

  /**
   * The extensions a conversation in `repoRoot` (or in no repository) selects: every enabled global server
   * its repository neither switches off nor replaces, and its repository's own enabled servers.
   */
  selectionFor(repoRoot: string | undefined): string[] {
    const repo = repoRoot === undefined ? undefined : this.#repos.get(repoRoot)
    const own = [...this.#servers.values()].filter(
      (s) => repoRoot !== undefined && s.repoRoot === repoRoot,
    )
    const replaced = new Set(own.map((s) => s.name))
    const names: string[] = []
    for (const server of this.#servers.values()) {
      if (server.repoRoot !== undefined) continue
      if (replaced.has(server.name)) continue
      const switched = repo?.switches.get(server.name)
      if (switched ?? server.parsed.enabled) names.push(server.extension)
    }
    for (const server of own) if (server.parsed.enabled) names.push(server.extension)
    return names
  }

  /** `mcp.list`: the global servers, and a repository's (or, without one, every repository's seen so far). */
  list(repoRoot?: string): {
    servers: WireServer[]
    diagnostics?: { path: string; message: string }[]
  } {
    const servers = [...this.#servers.values()]
      .filter((s) => s.repoRoot === undefined || repoRoot === undefined || s.repoRoot === repoRoot)
      .map((s) => this.#wire(s))
    return {
      servers,
      ...(this.#diagnostics.length === 0 ? {} : { diagnostics: [...this.#diagnostics] }),
    }
  }

  /** A server by name: a repository's when `repoRoot` is given, else the global one. */
  find(name: string, repoRoot?: string): Server | undefined {
    return this.#servers.get(repoRoot === undefined ? `g:${name}` : `r:${repoRoot}:${name}`)
  }

  /** The HTTP server a sign-in is for: the global one by that name, else a repository's. */
  httpServer(name: string): { server: Server; url: string } | undefined {
    const candidates = [
      this.find(name),
      ...[...this.#servers.values()].filter((s) => s.repoRoot !== undefined && s.name === name),
    ]
    for (const server of candidates) {
      const config = server?.parsed.config
      if (server !== undefined && config?.transport === 'http') return { server, url: config.url }
    }
    return undefined
  }

  /** Trust, or stop trusting, a repository's server: its exact config, as it is now. */
  async trust(server: Server, trusted: boolean): Promise<void> {
    if (server.repoRoot === undefined || server.parsed.config === undefined) return
    if (trusted) {
      await this.#options.db.run(
        'INSERT OR REPLACE INTO droi_mcp_trust (repo_root, name, fingerprint, at) VALUES (?, ?, ?, ?)',
        server.repoRoot,
        server.name,
        fingerprint(server.parsed.config),
        Date.now(),
      )
    } else {
      await this.#options.db.run(
        'DELETE FROM droi_mcp_trust WHERE repo_root = ? AND name = ?',
        server.repoRoot,
        server.name,
      )
    }
    await this.#reconcile(server)
    this.#changed()
  }

  /** Connect a server again now, as after an authorization. */
  async reconnect(server: Server): Promise<void> {
    this.#disconnect(server)
    await this.#reconcile(server)
    this.#changed()
  }

  async close(): Promise<void> {
    this.#closed = true
    await Promise.all([...this.#servers.values()].map((server) => this.#disconnect(server)))
  }

  async #upsert(
    key: string,
    name: string,
    repoRoot: string | undefined,
    raw: RawEntry,
  ): Promise<void> {
    const existing = this.#servers.get(key)
    if (existing !== undefined && JSON.stringify(existing.raw) === JSON.stringify(raw)) return
    if (existing !== undefined) await this.#disconnect(existing)
    const server: Server = existing ?? {
      key,
      name,
      ...(repoRoot === undefined ? {} : { repoRoot }),
      raw,
      parsed: { enabled: true },
      status: 'connecting',
      error: null,
      tools: [],
      generation: 0,
      retryDelay: RETRY_FIRST_MS,
      extension: extensionName(name, repoRoot),
    }
    server.raw = raw
    server.parsed = parseEntry(raw)
    server.retryDelay = RETRY_FIRST_MS
    this.#servers.set(key, server)
    await this.#reconcile(server)
  }

  async #remove(server: Server): Promise<void> {
    await this.#disconnect(server)
    this.#servers.delete(server.key)
  }

  /** Bring one server to what its config, its switch and its trust say. */
  async #reconcile(server: Server): Promise<void> {
    const parsed = server.parsed
    if (parsed.config === undefined) {
      this.#set(server, 'failed', parsed.error ?? 'invalid entry')
      return
    }
    if (!parsed.enabled) {
      this.#set(server, 'disabled', null)
      return
    }
    if (server.repoRoot !== undefined && !(await this.#trusted(server, parsed.config))) {
      this.#disconnect(server)
      this.#set(server, 'untrusted', null)
      return
    }
    if (
      server.status === 'connected' ||
      (server.status === 'connecting' && server.client !== undefined)
    )
      return
    void this.#connect(server)
  }

  async #trusted(server: Server, config: ServerConfig): Promise<boolean> {
    const row = await this.#options.db.get<{ fingerprint: string }>(
      'SELECT fingerprint FROM droi_mcp_trust WHERE repo_root = ? AND name = ?',
      server.repoRoot ?? '',
      server.name,
    )
    return row?.fingerprint === fingerprint(config)
  }

  async #connect(server: Server): Promise<void> {
    if (this.#closed || server.parsed.config === undefined) return
    const generation = ++server.generation
    if (server.retry !== undefined) clearTimeout(server.retry)
    server.retry = undefined
    this.#set(server, 'connecting', null)
    this.#changed()
    const client = new McpClient({
      name: 'droi',
      version: this.#options.version,
      requestTimeoutMs: CONNECT_TIMEOUT_MS,
    })
    server.client = client
    try {
      await client.connect(this.#transport(server, server.parsed.config))
      const tools = await client.listTools({ timeoutMs: CONNECT_TIMEOUT_MS })
      if (generation !== server.generation) {
        await client.close().catch(() => undefined)
        return
      }
      server.tools = tools
      server.retryDelay = RETRY_FIRST_MS
      this.#install(server, client)
      this.#set(server, 'connected', null)
      info('mcp server connected', { name: server.name, tools: tools.length })
      client.onClose(() => {
        if (generation !== server.generation || this.#closed) return
        this.#uninstall(server)
        server.client = undefined
        this.#set(server, 'failed', 'the connection closed')
        this.#scheduleRetry(server)
        this.#changed()
      })
    } catch (failure) {
      await client.close().catch(() => undefined)
      if (generation !== server.generation) return
      server.client = undefined
      const message = failure instanceof Error ? failure.message : String(failure)
      this.#set(
        server,
        'failed',
        needsAuthorization(failure) ? `authorization required: ${message}` : message,
      )
      warn('mcp server failed', { name: server.name, error: message })
      // A server that wants a person to sign in waits for mcp.authorize, not for a timer.
      if (!needsAuthorization(failure)) this.#scheduleRetry(server)
    }
    this.#changed()
  }

  #scheduleRetry(server: Server): void {
    if (this.#closed) return
    const delay = server.retryDelay
    server.retryDelay = Math.min(delay * 2, RETRY_MAX_MS)
    const generation = server.generation
    server.retry = setTimeout(() => {
      server.retry = undefined
      if (generation === server.generation) void this.#connect(server)
    }, delay)
    server.retry.unref?.()
  }

  #transport(server: Server, config: ServerConfig): McpTransport {
    if (config.transport === 'stdio') {
      return new StdioTransport({
        command: config.command,
        args: config.args,
        env: config.env,
        inheritEnv: true,
        cwd: config.cwd ?? server.repoRoot ?? homedir(),
        stderr: 'pipe',
      })
    }
    const auth = this.#options.authProvider?.(server.name, config.url)
    return new StreamableHttpTransport({
      url: config.url,
      headers: config.headers,
      ...(auth === undefined ? {} : { authProvider: auth }),
    })
  }

  async #disconnect(server: Server): Promise<void> {
    server.generation++
    if (server.retry !== undefined) clearTimeout(server.retry)
    server.retry = undefined
    this.#uninstall(server)
    const client = server.client
    server.client = undefined
    server.tools = []
    if (server.status === 'connected' || server.status === 'connecting')
      this.#set(server, 'connecting', null)
    await client?.close().catch(() => undefined)
  }

  #install(server: Server, client: McpClient): void {
    const used = new Set<string>()
    const tools: ToolRegistration[] = []
    for (const tool of server.tools) {
      const name = toolName(server.name, tool.name, used)
      used.add(name)
      setToolClass(name, tool.annotations?.readOnlyHint === true ? 'read' : undefined)
      tools.push(
        defineTool({
          name,
          description: tool.description ?? tool.title ?? tool.name,
          parameters: (tool.inputSchema ?? { type: 'object' }) as never,
          replay: 'unsafe',
          async execute(args, _api, context) {
            const result = await client.callTool(tool.name, args as Record<string, unknown>, {
              timeoutMs: CALL_TIMEOUT_MS,
              ...(context.abortSignal === undefined ? {} : { signal: context.abortSignal }),
            })
            return {
              output: toLlmContent(result) as never,
              ...(result.isError === true ? { isError: true } : {}),
            }
          },
        }) as ToolRegistration,
      )
    }
    const instructions = client.instructions
    const extension: Extension = defineExtension({
      name: server.extension,
      tools,
      ...(instructions === undefined || instructions.trim() === ''
        ? {}
        : {
            sections: [
              section(
                server.extension,
                () => `Instructions of the MCP server "${server.name}":\n${instructions.trim()}`,
              ),
            ],
          }),
    })
    this.#options.registry.install(extension)
  }

  #uninstall(server: Server): void {
    if (this.#options.registry.snapshot().extension(server.extension) !== undefined) {
      this.#options.registry.uninstall({ name: server.extension } as Extension)
    }
  }

  #set(server: Server, status: ServerStatus, error: string | null): void {
    server.status = status
    server.error = error
  }

  #setDiagnostic(path: string, message: string | undefined): void {
    this.#diagnostics = this.#diagnostics.filter((d) => d.path !== path)
    if (message !== undefined) {
      this.#diagnostics.push({ path, message })
      warn('mcp.json not read', { path, error: message })
    }
  }

  #changed(): void {
    if (this.#closed) return
    this.#options.notify('mcp.changed', this.list())
  }

  #wire(server: Server): WireServer {
    const config = server.parsed.config
    return {
      name: server.name,
      ...(config === undefined ? {} : { transport: config.transport }),
      status: server.status,
      enabled: server.parsed.enabled,
      tools: server.status === 'connected' ? this.#toolNames(server) : [],
      error: server.error,
      ...(server.repoRoot === undefined
        ? {}
        : {
            repoRoot: server.repoRoot,
            trusted: server.status !== 'untrusted' && config !== undefined,
          }),
      ...(config?.transport === 'stdio' ? { command: config.command, args: config.args } : {}),
      ...(config?.transport === 'http' ? { url: config.url } : {}),
    }
  }

  #toolNames(server: Server): string[] {
    const used = new Set<string>()
    return server.tools.map((tool) => {
      const name = toolName(server.name, tool.name, used)
      used.add(name)
      return name
    })
  }
}

/** `<server>___<tool>`, in the characters every provider takes, and short enough for all of them. */
export function toolName(
  server: string,
  tool: string,
  used: ReadonlySet<string> = new Set(),
): string {
  const clean = (value: string) => value.replace(/[^A-Za-z0-9_-]/g, '_')
  let name = `${clean(server)}___${clean(tool)}`
  if (name.length > MAX_TOOL_NAME || used.has(name)) {
    const hash = createHash('sha256').update(`${server}\u0000${tool}`).digest('hex').slice(0, 8)
    name = `${name.slice(0, MAX_TOOL_NAME - 9)}_${hash}`
  }
  return name
}

/** The extension of one server: a repository's carries its root's hash, so two repositories never share one. */
function extensionName(name: string, repoRoot: string | undefined): string {
  const clean = name.replace(/[^A-Za-z0-9_-]/g, '_')
  if (repoRoot === undefined) return `mcp-${clean}`
  return `mcp-${createHash('sha256').update(repoRoot).digest('hex').slice(0, 8)}-${clean}`
}

function needsAuthorization(failure: unknown): boolean {
  const name = (failure as { name?: unknown } | null)?.name
  return name === 'McpAuthRequiredError' || name === 'McpOAuthAuthorizationRequiredError'
}
