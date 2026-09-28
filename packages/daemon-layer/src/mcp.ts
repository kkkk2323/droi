// MCP Servers as the Daemon reports them for a Session, and what a Client may
// do with each. The Daemon owns the configuration (`~/.factory/mcp.json` at
// the user level, the Workspace's `.factory/mcp.json` at the project level,
// the organization's policy above both) and the connections; a Client lists,
// switches, adds and removes through it, as the droid CLI's `/mcp` does.
import type { DaemonSessionController } from '@factory/droid-sdk'

type Listed = Awaited<ReturnType<DaemonSessionController['listMcpServers']>>
export type McpServer = Listed['servers'][number]
export type McpSummary = Listed['summary']
export type McpTool = Awaited<ReturnType<DaemonSessionController['listMcpTools']>>['tools'][number]
export type McpRegistryEntry = Awaited<
  ReturnType<DaemonSessionController['listMcpRegistry']>
>['servers'][number]
export type AddMcpServerInput = Omit<
  Parameters<DaemonSessionController['addMcpServer']>[0],
  'sessionId'
>
export type McpServerType = AddMcpServerInput['type']

export const STATUS_LABELS: Record<string, string> = {
  connecting: 'Connecting',
  connected: 'Connected',
  disconnected: 'Disconnected',
  failed: 'Failed',
  disabled: 'Disabled',
}

/** The organization's servers are policy; nothing here changes them. */
export function isReadOnly(server: McpServer): boolean {
  return String(server.source) === 'org'
}

/** Only a server in the user's own `mcp.json` can be removed from here. */
export function canRemove(server: McpServer): boolean {
  return String(server.source) === 'user'
}

export function needsSignIn(server: McpServer): boolean {
  return server.requiresAuth === true && server.hasAuthTokens !== true
}

/** What a Client's form collects for a new server; every field a string. */
export interface ServerForm {
  name: string
  type: McpServerType
  command: string
  /** Whitespace-separated. */
  args: string
  url: string
  /** One `Name: value` per line. */
  headers: string
}

export const EMPTY_SERVER_FORM: ServerForm = {
  name: '',
  type: 'stdio',
  command: '',
  args: '',
  url: '',
  headers: '',
}

const NAME = /^[A-Za-z0-9_-]+$/

/** The Daemon's parameters for the form, or what is wrong with it. */
export function parseServerForm(
  form: ServerForm,
): { ok: true; params: AddMcpServerInput } | { ok: false; error: string } {
  const name = form.name.trim()
  if (!name) return { ok: false, error: 'Give the server a name.' }
  if (!NAME.test(name)) {
    return { ok: false, error: 'A name has letters, digits, "-" and "_" only.' }
  }
  if (form.type === 'stdio') {
    const command = form.command.trim()
    if (!command) return { ok: false, error: 'Give the command that starts the server.' }
    const args = form.args.split(/\s+/).filter(Boolean)
    return { ok: true, params: { name, type: 'stdio', command, ...(args.length ? { args } : {}) } }
  }
  const url = form.url.trim()
  if (!/^https?:\/\//.test(url))
    return { ok: false, error: 'The URL starts with http:// or https://.' }
  const headers: Record<string, string> = {}
  for (const line of form.headers.split('\n')) {
    if (!line.trim()) continue
    const colon = line.indexOf(':')
    if (colon <= 0)
      return { ok: false, error: `A header is "Name: value"; "${line.trim()}" is not.` }
    headers[line.slice(0, colon).trim()] = line.slice(colon + 1).trim()
  }
  return {
    ok: true,
    params: {
      name,
      type: form.type,
      url,
      ...(Object.keys(headers).length ? { headers } : {}),
    },
  }
}

/** A registry entry as the form would hold it, ready to adjust and add. */
export function formFromRegistry(entry: McpRegistryEntry): ServerForm {
  return {
    ...EMPTY_SERVER_FORM,
    name: entry.name,
    type: entry.type,
    command: entry.command ?? '',
    args: (entry.args ?? []).join(' '),
    url: entry.url ?? '',
  }
}

// A catalogue entry's command may carry a value to fill in first, written as
// an upper-case name (`--adyenApiKey=ADYEN_API_KEY`).
const PLACEHOLDER = /(?:^|=)[A-Z][A-Z0-9]*_[A-Z0-9_]+$/

/** Whether a catalogue entry has a value to fill in before it can be added as it is. */
export function needsSetup(entry: McpRegistryEntry): boolean {
  return [...(entry.args ?? []), entry.url ?? ''].some((part) => PLACEHOLDER.test(part))
}

/** The catalogue entries not added yet whose name or description matches the search. */
export function filterRegistry(
  entries: readonly McpRegistryEntry[],
  query: string,
  taken: readonly string[],
): McpRegistryEntry[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  return entries.filter((entry) => {
    if (taken.includes(entry.name)) return false
    const text = `${entry.name} ${entry.description}`.toLowerCase()
    return words.every((word) => text.includes(word))
  })
}

/** The servers sorted for a list: the user's and the project's first, by name; the organization's after. */
export function sortServers(servers: readonly McpServer[]): McpServer[] {
  return [...servers].sort(
    (a, b) => Number(isReadOnly(a)) - Number(isReadOnly(b)) || a.name.localeCompare(b.name),
  )
}
