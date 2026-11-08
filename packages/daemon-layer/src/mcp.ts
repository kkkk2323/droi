// MCP Servers as the computer's Kernel reports them (apps/kernel/protocol/README.md, "MCP
// servers"), and what a Client may do with each. Two files feed the list: the user's global
// mcp.json, which a Client switches, adds to and removes from, and a repository's own
// .factory/mcp.json, whose servers run only once the person allows them. A server the brand's
// app gives is not the user's, so nothing here changes it.

export type McpTransport = 'stdio' | 'http'
export type McpStatus = 'connected' | 'connecting' | 'failed' | 'disabled' | 'untrusted'

/** One server as `mcp.list` and `mcp.changed` report it. */
export interface McpServer {
  name: string
  transport?: McpTransport
  status: McpStatus
  enabled: boolean
  /** The tools' names as the model sees them, `<server>___<tool>`; empty until it connects. */
  tools: string[]
  error: string | null
  /** Set for a server a repository's own file brings. */
  repoRoot?: string
  /** A server the brand's app gives. */
  app?: boolean
  trusted?: boolean
  /** A repository server's config as allowing it must name it: what the person was shown. */
  fingerprint?: string
  command?: string
  args?: string[]
  url?: string
}

/** What `mcp.set` takes to add a global server. */
export interface AddMcpServerInput {
  name: string
  transport: McpTransport
  command?: string
  args?: string[]
  url?: string
  headers?: Record<string, string>
}

export const STATUS_LABELS: Record<string, string> = {
  connecting: 'Connecting',
  connected: 'Connected',
  failed: 'Failed',
  disabled: 'Disabled',
  untrusted: 'Waiting for your OK',
}

/** A repository's server and the brand's app's are not the user's mcp.json to switch. */
export function isReadOnly(server: McpServer): boolean {
  return server.repoRoot !== undefined || server.app === true
}

/** Only a server in the user's own mcp.json can be removed from here. */
export function canRemove(server: McpServer): boolean {
  return !isReadOnly(server)
}

/** An HTTP server that answered 401: the Kernel retries it only once someone signs in. */
export function needsSignIn(server: McpServer): boolean {
  return server.status === 'failed' && (server.error ?? '').startsWith('authorization required')
}

/** A repository's server that waits for the person to allow what it runs. */
export function needsTrust(server: McpServer): boolean {
  return server.status === 'untrusted'
}

/** What a repository's server would run, as the confirmation shows it. */
export function serverCommand(server: McpServer): string {
  if (server.command) return [server.command, ...(server.args ?? [])].join(' ')
  return server.url ?? ''
}

/** A tool's name without its server's prefix. */
export function toolLabel(server: Pick<McpServer, 'name'>, tool: string): string {
  const prefix = `${server.name.replace(/[^A-Za-z0-9_-]/g, '_')}___`
  return tool.startsWith(prefix) ? tool.slice(prefix.length) : tool
}

/** The key a sign-in is kept under: a global server and a repository's of one name are two servers. */
export function serverKey(server: Pick<McpServer, 'name' | 'repoRoot'>): string {
  return `${server.repoRoot ?? ''}\u0000${server.name}`
}

/** What a Client's form collects for a new server; every field a string. */
export interface ServerForm {
  name: string
  transport: McpTransport
  command: string
  /** Whitespace-separated. */
  args: string
  url: string
  /** One `Name: value` per line. */
  headers: string
}

export const EMPTY_SERVER_FORM: ServerForm = {
  name: '',
  transport: 'stdio',
  command: '',
  args: '',
  url: '',
  headers: '',
}

const NAME = /^[A-Za-z0-9_-]+$/

/** The server the form describes, or what is wrong with it. */
export function parseServerForm(
  form: ServerForm,
): { ok: true; params: AddMcpServerInput } | { ok: false; error: string } {
  const name = form.name.trim()
  if (!name) return { ok: false, error: 'Enter a server name.' }
  if (!NAME.test(name)) {
    return { ok: false, error: 'Server name can only contain letters, digits, "-", and "_".' }
  }
  if (form.transport === 'stdio') {
    const command = form.command.trim()
    if (!command) return { ok: false, error: 'Enter the command that starts the server.' }
    const args = form.args.split(/\s+/).filter(Boolean)
    return {
      ok: true,
      params: { name, transport: 'stdio', command, ...(args.length ? { args } : {}) },
    }
  }
  const url = form.url.trim()
  if (!/^https?:\/\//.test(url))
    return { ok: false, error: 'URL must start with http:// or https://.' }
  const headers: Record<string, string> = {}
  for (const line of form.headers.split('\n')) {
    if (!line.trim()) continue
    const colon = line.indexOf(':')
    if (colon <= 0)
      return { ok: false, error: `Format headers as "Name: value" (invalid: "${line.trim()}").` }
    headers[line.slice(0, colon).trim()] = line.slice(colon + 1).trim()
  }
  return {
    ok: true,
    params: { name, transport: 'http', url, ...(Object.keys(headers).length ? { headers } : {}) },
  }
}

/** The servers sorted for a list: the user's first, by name; a repository's and the app's after. */
export function sortServers(servers: readonly McpServer[]): McpServer[] {
  return [...servers].sort(
    (a, b) => Number(isReadOnly(a)) - Number(isReadOnly(b)) || a.name.localeCompare(b.name),
  )
}
