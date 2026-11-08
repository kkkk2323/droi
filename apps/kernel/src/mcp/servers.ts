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
import type { CodemodeNamespace } from '@earendil-works/pi-durable/codemode'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import { setToolClass } from '../policy/decide.ts'
import { info, warn } from '../log.ts'
import {
  enabledOf,
  fingerprint,
  mergeEntry,
  onlySwitches,
  parseEntry,
  readConfig,
  type ConfigFile,
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
  /** A repository server's config as `mcp.set {trust}` must name it: what the person was shown. */
  fingerprint?: string
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
  /** The tool names this server gave the approval policy a class for. */
  classified: string[]
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
  /** Codemode's groups of tools, one per connected server: kept current here, read by codemode as it goes. */
  namespaces?: CodemodeNamespace[]
  version: string
  context: Context
}

/** What `trustServer` found: the server, none of that name, or one whose config is not the one the person saw. */
export type TrustOutcome = 'ok' | 'missing' | 'changed'

/** The HTTP server a sign-in is for, or why there is none to sign in to. */
export type Authorizable =
  | { ok: true; key: string; url: string }
  | { ok: false; reason: 'missing' | 'not-http' | 'untrusted' }

/**
 * The MCP servers (design/m3-kernel.md §4): the global file's, and those each repository's
 * `.factory/mcp.json` brings, which run only once the user trusts that exact config.
 *
 * Every server is an extension of its own, named `mcp-…`. A conversation selects the global ones and its own
 * repository's (`prepare`), so it is never offered another repository's tools. An extension is installed while
 * its server is connected; a conversation that names one that is not is simply offered none of its tools.
 *
 * Every change to the table runs in one queue (`#serial`): a refresh awaits the database and old connections
 * between its steps, and two of them interleaved could check the trust of one config and start another, or put
 * back a server a later read had removed. A connection itself is not queued, only started: what it runs is
 * read when it starts, inside the step that decided it may.
 */
export class McpServers {
  readonly #options: McpServersOptions
  readonly #servers = new Map<string, Server>()
  /** Each repository's file and the global file as last read, and the servers it switched on or off by name. */
  readonly #repos = new Map<
    string,
    { file: unknown; global: unknown; switches: Map<string, boolean> }
  >()
  #globalFile: ConfigFile | undefined
  #diagnostics: { path: string; message: string }[] = []
  #closed = false
  #queue: Promise<unknown> = Promise.resolve()
  /** The extension of each entry of `options.namespaces`, in the same order. */
  readonly #namespaceOwners: string[] = []

  constructor(options: McpServersOptions) {
    this.#options = options
  }

  get #globalPath(): string {
    return join(this.#options.userDir, 'mcp.json')
  }

  #serial<T>(step: () => Promise<T>): Promise<T> {
    const run = this.#queue.then(step, step)
    this.#queue = run.catch(() => undefined)
    return run
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
  refreshGlobal(): Promise<void> {
    return this.#serial(() => this.#refreshGlobal())
  }

  /** Read a repository's file again if it changed: its servers, and the global ones it switches. */
  refreshRepo(repoRoot: string): Promise<void> {
    return this.#serial(() => this.#refreshRepo(repoRoot))
  }

  async #refreshGlobal(): Promise<void> {
    if (this.#closed) return
    const file = readConfig(this.#globalPath)
    if (file === this.#globalFile) return
    this.#globalFile = file
    this.#setDiagnostic(this.#globalPath, file.error)
    const seen = new Set<string>()
    for (const [name, raw] of Object.entries(file.entries)) {
      const key = serverKey(name)
      seen.add(key)
      await this.#upsert(key, name, undefined, raw)
    }
    for (const server of [...this.#servers.values()]) {
      if (server.repoRoot === undefined && !seen.has(server.key)) await this.#remove(server)
    }
    // A repository's entry can take fields from the global one of its name.
    for (const root of [...this.#repos.keys()]) await this.#refreshRepo(root)
    this.#changed()
  }

  async #refreshRepo(repoRoot: string): Promise<void> {
    if (this.#closed) return
    const path = join(repoRoot, '.factory', 'mcp.json')
    const file = readConfig(path)
    const global = this.#globalFile ?? readConfig(this.#globalPath)
    const known = this.#repos.get(repoRoot)
    if (known?.file === file && known.global === global) return
    const switches = new Map<string, boolean>()
    this.#repos.set(repoRoot, { file, global, switches })
    this.#setDiagnostic(path, file.error)
    const seen = new Set<string>()
    for (const [name, raw] of Object.entries(file.entries)) {
      if (onlySwitches(raw)) {
        switches.set(name, enabledOf(raw))
        continue
      }
      const key = serverKey(name, repoRoot)
      seen.add(key)
      await this.#upsert(key, name, repoRoot, mergeEntry(global.entries[name], raw))
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

  #find(name: string, repoRoot?: string): Server | undefined {
    return this.#servers.get(serverKey(name, repoRoot))
  }

  /**
   * Trust, or stop trusting, a repository's server. Trust is for the config the person was shown: `shown` is the
   * fingerprint `mcp.list` gave with it, and a file that changed since is a new question, not a yes.
   */
  trustServer(
    name: string,
    repoRoot: string,
    trusted: boolean,
    shown: string | undefined,
  ): Promise<TrustOutcome> {
    return this.#serial(async () => {
      await this.#refreshRepo(repoRoot)
      const server = this.#find(name, repoRoot)
      const config = server?.parsed.config
      if (server === undefined || config === undefined) return 'missing'
      const current = fingerprint(config)
      if (trusted) {
        if (shown !== current) return 'changed'
        await this.#options.db.run(
          'INSERT OR REPLACE INTO droi_mcp_trust (repo_root, name, fingerprint, at) VALUES (?, ?, ?, ?)',
          repoRoot,
          name,
          current,
          Date.now(),
        )
      } else {
        await this.#options.db.run(
          'DELETE FROM droi_mcp_trust WHERE repo_root = ? AND name = ?',
          repoRoot,
          name,
        )
      }
      await this.#reconcile(server)
      this.#changed()
      return 'ok'
    })
  }

  /**
   * The HTTP server a sign-in is for: the global one by that name, or, with `repoRoot`, that repository's own,
   * and only once it is trusted, since signing in already talks to its URL.
   */
  authorizable(name: string, repoRoot?: string): Promise<Authorizable> {
    return this.#serial(async () => {
      if (repoRoot === undefined) await this.#refreshGlobal()
      else await this.#refreshRepo(repoRoot)
      const server = this.#find(name, repoRoot)
      const config = server?.parsed.config
      if (server === undefined || config === undefined) return { ok: false, reason: 'missing' }
      if (config.transport !== 'http') return { ok: false, reason: 'not-http' }
      if (server.repoRoot !== undefined && !(await this.#trusted(server, config))) {
        return { ok: false, reason: 'untrusted' }
      }
      return { ok: true, key: server.key, url: config.url }
    })
  }

  /** Connect a server again now, as after an authorization; by the key `authorizable` gave. */
  reconnect(key: string): Promise<void> {
    return this.#serial(async () => {
      const server = this.#servers.get(key)
      if (server === undefined || this.#closed) return
      await this.#disconnect(server)
      await this.#reconcile(server)
      this.#changed()
    })
  }

  async close(): Promise<void> {
    this.#closed = true
    await this.#queue
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
      classified: [],
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

  /** Bring one server to what its config, its switch and its trust say. Runs inside the queue. */
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
      await this.#disconnect(server)
      this.#set(server, 'untrusted', null)
      return
    }
    // The config the trust was checked against is the one that runs: nothing else changes the table while
    // this step holds the queue, and the connection reads it before its first await.
    if (server.parsed !== parsed || this.#servers.get(server.key) !== server) return
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
      // Through the queue, and only for the server and the config it failed with: a retry never outlives a
      // change that came in meanwhile.
      void this.#serial(async () => {
        if (generation !== server.generation || this.#servers.get(server.key) !== server) return
        await this.#reconcile(server)
      })
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
    for (const name of server.classified) setToolClass(name, server.extension, undefined)
    server.classified = []
    for (const tool of server.tools) {
      const name = toolName(server.name, tool.name, used)
      used.add(name)
      setToolClass(
        name,
        server.extension,
        tool.annotations?.readOnlyHint === true ? 'read' : 'exec',
      )
      server.classified.push(name)
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
    this.#setNamespace(server, {
      name: server.name,
      prefix: `${cleanName(server.name)}___`,
      ...(instructions === undefined || instructions.trim() === ''
        ? {}
        : { instructions: instructions.trim() }),
    })
  }

  #uninstall(server: Server): void {
    if (this.#options.registry.snapshot().extension(server.extension) !== undefined) {
      this.#options.registry.uninstall({ name: server.extension } as Extension)
    }
    for (const name of server.classified) setToolClass(name, server.extension, undefined)
    server.classified = []
    this.#setNamespace(server, undefined)
  }

  /** Codemode's namespaces, one per extension: two repositories' servers of one name keep one each. */
  #setNamespace(server: Server, namespace: CodemodeNamespace | undefined): void {
    const list = this.#options.namespaces
    if (list === undefined) return
    const at = this.#namespaceOwners.indexOf(server.extension)
    if (at >= 0) {
      list.splice(at, 1)
      this.#namespaceOwners.splice(at, 1)
    }
    if (namespace !== undefined) {
      list.push(namespace)
      this.#namespaceOwners.push(server.extension)
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
            ...(config === undefined ? {} : { fingerprint: fingerprint(config) }),
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
  let name = `${cleanName(server)}___${cleanName(tool)}`
  if (name.length > MAX_TOOL_NAME || used.has(name)) {
    const hash = createHash('sha256').update(`${server}\u0000${tool}`).digest('hex').slice(0, 8)
    name = `${name.slice(0, MAX_TOOL_NAME - 9)}_${hash}`
  }
  return name
}

function cleanName(value: string): string {
  return value.replace(/[^A-Za-z0-9_-]/g, '_')
}

/** A server's own key: the global one by its name, a repository's by its root and its name. */
export function serverKey(name: string, repoRoot?: string): string {
  return repoRoot === undefined ? `g:${name}` : `r:${repoRoot}:${name}`
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
