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
  slotOfEntry,
  type Category,
  type MemoryCall,
  type MemoryEntry,
  type MemorySlot,
  type MemoryStore,
  type Scope,
  type WriteResult,
} from './store'
import { similarEntries } from './similar'

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
        query: {
          type: 'string',
          description:
            'One to three specific words; entries holding all of them come first, and any of them only when none holds all. Quote a phrase to keep it whole.',
        },
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
      'Record one durable fact for later Sessions: a correction the user made, a convention, a tool quirk, a failure and its cause, an insight, a preference. One fact per entry, written so it stands on its own. Never store secrets, credentials or one-off task details. The answer lists entries that may already say the same thing.',
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
  /** Whether the calling Session is a Memory Session, which gets no Memory of its own. */
  isMemorySession?: (sessionId: string) => boolean
  /** Whether the calling Session runs in a Scratch Workspace, which has no Project Memory. */
  isScratchSession?: (sessionId: string) => boolean
  /** After every successful write, with the Memory it changed. */
  onWrite?: (slot: MemorySlot) => void
}

const MEMORY_SESSION_REFUSAL =
  'Memory tools are not available in a Memory Session. Answer from the material you were given.'
const NO_PROJECT_MEMORY =
  'This Session is a chat without a project, so it has no Project Memory. Only scope "global" applies here'
const SCRATCH_READ = `${NO_PROJECT_MEMORY}: search or list Global Memory instead.`
const SCRATCH_WRITE = `${NO_PROJECT_MEMORY}: save a fact about the user or their environment with scope "global"; do not save anything else.`

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
  // A Memory Session runs on the same Daemon, so it sees these tools too; a
  // consolidation that wrote to Memory while rewriting it would race itself.
  if (sessionId && options.isMemorySession?.(sessionId)) return text(MEMORY_SESSION_REFUSAL, true)
  const call: MemoryCall = { sessionId, tool: name, slot: null, query: null, ok: true }
  const result = runToolCall(options, name, args, sessionId, call)
  call.ok = result.isError !== true
  try {
    options.store.logCall(call)
  } catch (error) {
    // The log only measures Memory; a busy database must not fail the call.
    process.stderr.write(`droi-memory: call log failed: ${String(error)}\n`)
  }
  return result
}

function runToolCall(
  options: MemoryServerOptions,
  name: string,
  args: Record<string, unknown>,
  sessionId: string | null,
  call: MemoryCall,
): ToolResult {
  const { store } = options
  const scopeOf = (value: unknown): Scope | null =>
    value === undefined || value === 'project' ? 'project' : value === 'global' ? 'global' : null
  const slotFor = (scope: Scope, write = false): MemorySlot | ToolResult => {
    if (scope === 'global') return { scope: 'global' }
    if (sessionId && options.isScratchSession?.(sessionId)) {
      // A read finds nothing there, so it is an answer; a write is a refusal.
      return write ? text(SCRATCH_WRITE, true) : text(SCRATCH_READ)
    }
    const workspace = sessionId ? options.workspaceOf(sessionId) : null
    if (!workspace) {
      return text(
        'Memory cannot tell which Workspace this Session runs in, so only scope "global" is available.',
        true,
      )
    }
    return projectSlot(workspace)
  }
  /** The calling Session's Project Memory, if it has one. */
  const sessionProject = (): MemorySlot | null => {
    if (!sessionId || options.isScratchSession?.(sessionId)) return null
    const workspace = options.workspaceOf(sessionId)
    return workspace ? projectSlot(workspace) : null
  }
  const optionalCategory = (value: unknown): Category | undefined | null =>
    value === undefined ? undefined : isCategory(value) ? value : null
  const wrote = (slot: MemorySlot) => {
    if (sessionId) store.recordWrite(sessionId)
    options.onWrite?.(slot)
  }
  const written = (result: WriteResult, verb: string): ToolResult => {
    if (!result.ok) return text(result.reason, true)
    call.slot = slotOfEntry(result.entry)
    call.written = result.entry.id
    wrote(call.slot)
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
      call.slot = slot
      call.query = args['query']
      call.found = found.map((e) => e.id)
      return text(describeEntries(found, `in ${scope} Memory match "${args['query']}"`))
    }
    case 'memory_list': {
      const scope = scopeOf(args['scope'])
      const category = optionalCategory(args['category'])
      if (!scope || category === null) return text('memory_list needs a valid scope.', true)
      const slot = slotFor(scope)
      if ('content' in slot) return slot
      call.slot = slot
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
      const slot = slotFor(scope, true)
      if ('content' in slot) return slot
      const result = store.add(slot, args['category'], args['text'])
      const saved = written(result, 'Saved')
      if (!result.ok) return saved
      // Droid adds far more often than it replaces, so a fact that changed is
      // usually saved beside the entry it supersedes, often in the other scope.
      const other: MemorySlot | null =
        slot.scope === 'project' ? { scope: 'global' } : sessionProject()
      const similar = similarEntries(
        result.entry.text,
        [slot, ...(other ? [other] : [])]
          .flatMap((s) => store.list(s))
          .filter((e) => e.id !== result.entry.id),
      )
      if (similar.length === 0) return saved
      call.similar = similar.map((e) => e.id)
      return text(
        `${saved.content[0]!.text}\n\n${describeEntries(similar, 'already in Memory may say the same thing or now be out of date')}\nIf one of them repeats this fact, keep one: replace it with the combined fact and remove ${result.entry.id}. If one is now wrong, replace or remove it.`,
      )
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
      call.slot = slotOfEntry(found)
      call.written = found.id
      wrote(call.slot)
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
