// The Memory Server's protocol: MCP over stdio, one JSON-RPC message per line.
// Hand-written rather than taken from the MCP SDK because the server runs as a
// single bundled file under Electron's Node (ADR 0010) and needs only
// initialize, tools/list and tools/call.
import { createInterface } from 'node:readline'
import type { Readable, Writable } from 'node:stream'
import {
  CATEGORIES,
  LIMITS,
  isCategory,
  projectSlot,
  type Category,
  type MemoryEntry,
  type MemorySlot,
  type MemoryStore,
  type Scope,
  type WriteResult,
} from './store'

export const SERVER_NAME = 'droi-memory'
const PROTOCOL_VERSION = '2025-06-18'

const scopeProperty = {
  type: 'string',
  enum: ['project', 'global'],
  description:
    'project (default): this Workspace’s Memory. global: facts that hold in every Workspace, such as how the user likes to be answered.',
}
const categoryProperty = { type: 'string', enum: [...CATEGORIES] }

export const TOOLS = [
  {
    name: 'memory_search',
    description:
      'Search Memory for facts recorded in earlier Sessions: conventions, tooling quirks, past failures, corrections, preferences. Search before acting on how this Workspace does things.',
    inputSchema: {
      type: 'object',
      properties: {
        query: { type: 'string', description: 'Words to look for; any of them may match.' },
        scope: scopeProperty,
        category: categoryProperty,
        limit: { type: 'integer', minimum: 1, maximum: 50, description: 'Default 10.' },
      },
      required: ['query'],
      additionalProperties: false,
    },
    annotations: { title: 'Search Memory', readOnlyHint: true },
  },
  {
    name: 'memory_list',
    description: 'List every Memory entry of a scope, optionally of one category.',
    inputSchema: {
      type: 'object',
      properties: { scope: scopeProperty, category: categoryProperty },
      required: ['scope'],
      additionalProperties: false,
    },
    annotations: { title: 'List Memory', readOnlyHint: true },
  },
  {
    name: 'memory_add',
    description:
      'Record one durable fact for later Sessions: a correction the user made, a convention, a tool quirk, a failure and its cause, an insight, a preference. One fact per entry, written so it stands on its own. Never store secrets, credentials or one-off task details.',
    inputSchema: {
      type: 'object',
      properties: {
        scope: scopeProperty,
        category: categoryProperty,
        text: { type: 'string', description: 'The fact, in one or two sentences.' },
      },
      required: ['scope', 'category', 'text'],
      additionalProperties: false,
    },
    annotations: { title: 'Add to Memory', readOnlyHint: false, destructiveHint: false },
  },
  {
    name: 'memory_replace',
    description: 'Rewrite an entry that is no longer quite true. Keeps its id and category.',
    inputSchema: {
      type: 'object',
      properties: { id: { type: 'string' }, text: { type: 'string' } },
      required: ['id', 'text'],
      additionalProperties: false,
    },
    annotations: { title: 'Replace a Memory entry', readOnlyHint: false, destructiveHint: true },
  },
  {
    name: 'memory_remove',
    description: 'Remove an entry that is wrong or no longer applies.',
    inputSchema: {
      type: 'object',
      properties: { id: { type: 'string' } },
      required: ['id'],
      additionalProperties: false,
    },
    annotations: { title: 'Remove a Memory entry', readOnlyHint: false, destructiveHint: true },
  },
] as const

export interface MemoryServerOptions {
  store: MemoryStore
  /** The Workspace a Session runs in, whose Project Memory is its `project` scope. */
  workspaceOf: (sessionId: string) => string | null
  /** After every successful write, with the Memory it changed. */
  onWrite?: (slot: MemorySlot) => void
}

type ToolResult = { content: Array<{ type: 'text'; text: string }>; isError?: boolean }

const text = (value: string, isError = false): ToolResult => ({
  content: [{ type: 'text', text: value }],
  ...(isError ? { isError: true } : {}),
})

function describeEntries(entries: MemoryEntry[], what: string): string {
  if (entries.length === 0) return `No entries ${what}.`
  const lines = entries.map((e) => `- ${e.id} [${e.scope}/${e.category}, ${e.day}] ${e.text}`)
  return `${entries.length} ${entries.length === 1 ? 'entry' : 'entries'} ${what}:\n${lines.join('\n')}`
}

const slotOfEntry = (entry: MemoryEntry): MemorySlot =>
  entry.scope === 'global'
    ? { scope: 'global' }
    : { scope: 'project', workspace: entry.workspace ?? '' }

/**
 * One tool call. `sessionId` is the calling Session, which the Daemon puts in
 * the call's `_meta`; without it only Global Memory can be reached.
 */
export function handleToolCall(
  options: MemoryServerOptions,
  name: string,
  args: Record<string, unknown>,
  sessionId: string | null,
): ToolResult {
  const { store } = options
  const scopeOf = (value: unknown): Scope | null =>
    value === undefined || value === 'project' ? 'project' : value === 'global' ? 'global' : null
  const slotFor = (scope: Scope): MemorySlot | ToolResult => {
    if (scope === 'global') return { scope: 'global' }
    const workspace = sessionId ? options.workspaceOf(sessionId) : null
    if (!workspace) {
      return text(
        'Memory cannot tell which Workspace this Session runs in, so only scope "global" is available.',
        true,
      )
    }
    return projectSlot(workspace)
  }
  const optionalCategory = (value: unknown): Category | undefined | null =>
    value === undefined ? undefined : isCategory(value) ? value : null
  const wrote = (slot: MemorySlot) => {
    if (sessionId) store.recordWrite(sessionId)
    options.onWrite?.(slot)
  }
  const written = (result: WriteResult, verb: string): ToolResult => {
    if (!result.ok) return text(result.reason, true)
    wrote(slotOfEntry(result.entry))
    const where = result.entry.scope === 'global' ? 'Global Memory' : 'Project Memory'
    const full = result.overSoftLimit
      ? ` ${where} is past ${LIMITS[result.entry.scope].soft.toLocaleString('en')} characters; suggest that the user consolidates it in Droi under Settings → Memory.`
      : ''
    return text(`${verb} ${result.entry.id} in ${where}.${full}`)
  }

  switch (name) {
    case 'memory_search': {
      const scope = scopeOf(args['scope'])
      const category = optionalCategory(args['category'])
      if (typeof args['query'] !== 'string' || !scope || category === null) {
        return text('memory_search needs a query, and a valid scope and category if given.', true)
      }
      const slot = slotFor(scope)
      if ('content' in slot) return slot
      const limit =
        typeof args['limit'] === 'number'
          ? Math.min(Math.max(1, Math.floor(args['limit'])), 50)
          : 10
      const found = store.search({ query: args['query'], slot, category, limit })
      return text(describeEntries(found, `in ${scope} Memory match "${args['query']}"`))
    }
    case 'memory_list': {
      const scope = scopeOf(args['scope'])
      const category = optionalCategory(args['category'])
      if (!scope || category === null) return text('memory_list needs a valid scope.', true)
      const slot = slotFor(scope)
      if ('content' in slot) return slot
      return text(describeEntries(store.list(slot, category), `in ${scope} Memory`))
    }
    case 'memory_add': {
      const scope = scopeOf(args['scope'])
      if (!scope || !isCategory(args['category']) || typeof args['text'] !== 'string') {
        return text(
          `memory_add needs a scope, a category (${CATEGORIES.join(', ')}) and text.`,
          true,
        )
      }
      const slot = slotFor(scope)
      if ('content' in slot) return slot
      return written(store.add(slot, args['category'], args['text']), 'Saved')
    }
    case 'memory_replace': {
      if (typeof args['id'] !== 'string' || typeof args['text'] !== 'string') {
        return text('memory_replace needs an id and the new text.', true)
      }
      return written(store.replace(args['id'], args['text']), 'Replaced')
    }
    case 'memory_remove': {
      if (typeof args['id'] !== 'string') return text('memory_remove needs an id.', true)
      const found = store.get(args['id'])
      if (!found || !store.remove(args['id'])) {
        return text(`No Memory entry has the id ${args['id']}.`, true)
      }
      wrote(slotOfEntry(found))
      return text(`Removed ${found.id}.`)
    }
    default:
      return text(`Unknown tool ${name}.`, true)
  }
}

interface Request {
  jsonrpc: '2.0'
  id?: string | number | null
  method: string
  params?: Record<string, unknown>
}

/** Serves MCP on the two streams until the input ends. */
export function serveMemory(
  input: Readable,
  output: Writable,
  options: MemoryServerOptions,
): Promise<void> {
  const send = (message: Record<string, unknown>): void => {
    output.write(`${JSON.stringify(message)}\n`)
  }
  const reply = (id: Request['id'], result: unknown) => send({ jsonrpc: '2.0', id, result })
  const fail = (id: Request['id'], code: number, message: string) =>
    send({ jsonrpc: '2.0', id, error: { code, message } })

  const handle = (request: Request): void => {
    const isNotification = request.id === undefined || request.id === null
    switch (request.method) {
      case 'initialize':
        return reply(request.id, {
          protocolVersion:
            typeof request.params?.['protocolVersion'] === 'string'
              ? request.params['protocolVersion']
              : PROTOCOL_VERSION,
          capabilities: { tools: {} },
          serverInfo: { name: SERVER_NAME, title: 'Droi Memory', version: '1.0.0' },
        })
      case 'ping':
        return reply(request.id, {})
      case 'tools/list':
        return reply(request.id, { tools: TOOLS })
      case 'tools/call': {
        const name = request.params?.['name']
        const args = request.params?.['arguments']
        const meta = request.params?.['_meta'] as Record<string, unknown> | undefined
        const sessionId =
          typeof meta?.['assemblySessionId'] === 'string' ? meta['assemblySessionId'] : null
        if (typeof name !== 'string') return fail(request.id, -32602, 'tools/call needs a name')
        try {
          return reply(
            request.id,
            handleToolCall(
              options,
              name,
              typeof args === 'object' && args !== null ? (args as Record<string, unknown>) : {},
              sessionId,
            ),
          )
        } catch (error) {
          return reply(
            request.id,
            text(error instanceof Error ? error.message : String(error), true),
          )
        }
      }
      default:
        if (!isNotification) return fail(request.id, -32601, `Method not found: ${request.method}`)
    }
  }

  const lines = createInterface({ input, crlfDelay: Infinity })
  lines.on('line', (line) => {
    if (!line.trim()) return
    let request: Request
    try {
      request = JSON.parse(line) as Request
    } catch {
      fail(null, -32700, 'Parse error')
      return
    }
    handle(request)
  })
  return new Promise((resolve) => lines.once('close', () => resolve()))
}
