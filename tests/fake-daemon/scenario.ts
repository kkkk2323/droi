// Scenario: the per-test script that tells the Fake Daemon what to answer and
// when to emit events. A Scenario starts from a list of Session fixtures and
// default handlers for the read-only methods; tests override or add handlers.
import { randomUUID } from 'node:crypto'
import { RpcError, type FakeDaemon } from './fake-daemon'
import type { JsonRpcRequest } from './protocol'
import { toolHandlers, type ToolsInput } from './tools'

export interface SessionFixture {
  sessionId: string
  title: string
  cwd: string
  updatedAt: number
  messages: MessageFixture[]
  archivedAt?: string
  settings?: Record<string, unknown>
  tags?: Array<{ name: string; metadata?: Record<string, string> }>
  /** Exists on disk but is not loaded in the Daemon; per-Session RPCs fail until load_session. */
  inactive?: boolean
  /** Opened by initialize_session and not written to yet; the Daemon lists only written Sessions. */
  unwritten?: boolean
  /** Branch and uncommitted changes get_git_diff reports; absent means not a Git repository. */
  git?: {
    branch: string
    files: Array<{ path: string; status: string; additions: number; deletions: number }>
  }
  /** A subagent: the calling Session and Task call, and the run its caller's load reports. */
  subagent?: {
    callingSessionId: string
    callingToolUseId: string
    subagentType: string
    description: string
    status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
    toolUseCount?: number
    durationMs?: number
  }
}

export interface MessageFixture {
  id: string
  role: 'user' | 'assistant' | 'tool'
  content: Array<Record<string, unknown>>
  createdAt: number
  updatedAt: number
  /** `user_only` for what the model never sees, such as the calls a Script made. */
  visibility: 'both' | 'user_only'
}

export interface HandlerContext {
  daemon: FakeDaemon
  connectionId: number
}

export type MethodHandler = (
  params: Record<string, unknown>,
  context: HandlerContext,
  request: JsonRpcRequest,
) => unknown | Promise<unknown>

export interface ScenarioInput {
  sessions?: SessionFixture[]
  handlers?: Record<string, MethodHandler>
  /** Directories the Fake Daemon treats as existing; others fail validation. */
  validDirectories?: string[]
  /** Custom commands (`.factory/commands`) the Daemon lists for every Session. */
  commands?: Array<{ name: string; description: string; argumentHint?: string }>
  /** Skills the Daemon lists; `userInvocable` defaults to true. */
  skills?: ToolsInput['skills']
  /** Whether the Sessions' Workspaces take project-level skill switches. */
  projectAvailable?: boolean
  /** MCP Servers the Daemon has, and Factory's registry of ones to add. */
  mcpServers?: ToolsInput['mcpServers']
  mcpRegistry?: ToolsInput['mcpRegistry']
  /** Context tokens the breakdown reports for every Session; defaults to 1k per message. */
  contextUsedTokens?: number
  /** Overrides for the Session defaults the Daemon reports (`management`, say). */
  defaults?: Record<string, unknown>
  /** The models the Daemon offers, in place of AVAILABLE_MODELS. */
  models?: Array<Record<string, unknown>>
  /** Files on the computer, by absolute path, that get_workspace_file_content serves. */
  files?: Record<string, { mimeType: string; base64: string }>
  /** Sessions open in the Daemon (by any Client) with their working state, for list_opened_sessions. */
  opened?: Array<{ sessionId: string; workingState: string }>
  /** How long load_session takes, as the real Daemon reading a long Session from disk. */
  loadDelayMs?: number
}

export const CONTEXT_BUDGET = 200_000

export interface Scenario {
  sessions: SessionFixture[]
  /** Files get_workspace_file_content serves; a test rewrites one to stand for a retaken screenshot. */
  files: Record<string, { mimeType: string; base64: string }>
  handlers: Record<string, MethodHandler>
  handle(request: JsonRpcRequest, context: HandlerContext): Promise<unknown>
  on(method: string, handler: MethodHandler): void
}

export function createScenario(input: ScenarioInput): Scenario {
  // Handlers mutate the fixtures (rename, archive, settings). Spec files
  // declare theirs at module level, shared by every test in the worker, so
  // each Fake Daemon works on its own copy.
  const sessions = structuredClone(input.sessions ?? [])
  const files = structuredClone(input.files ?? {})
  // Like ~/.factory/settings.json behind the real Daemon: one set per Fake Daemon.
  let defaults: Record<string, unknown> = { ...sessionDefaults(), ...input.defaults }
  const models = input.models ?? AVAILABLE_MODELS
  const tools = toolHandlers(input)
  // Like the real Daemon, a Session it has not loaded has no commands or skills.
  const activeOrThrow = (sessionId: unknown) => {
    if (sessions.find((s) => s.sessionId === sessionId)?.inactive) {
      throw new RpcError(-32603, 'No active session found for ID')
    }
  }
  const handlers: Record<string, MethodHandler> = {
    // Newest first, `limit` at a time; `endBefore` (epoch seconds) is the cursor
    // the Daemon hands back as `nextCursor` to fetch the page before it.
    'daemon.list_available_sessions': (params) => {
      const limit = typeof params['limit'] === 'number' ? params['limit'] : 50
      const endBefore = typeof params['endBefore'] === 'number' ? params['endBefore'] : Infinity
      const listed = sessions
        .filter((s) => !s.unwritten)
        .filter((s) => params['includeArchived'] === true || !s.archivedAt)
        .filter((s) => s.updatedAt < endBefore)
        .sort((a, b) => b.updatedAt - a.updatedAt)
      const page = listed.slice(0, limit)
      const last = page[page.length - 1]
      return {
        sessions: page.map((s) => ({
          sessionId: s.sessionId,
          updatedAt: s.updatedAt,
          title: s.title,
          cwd: s.cwd,
          messagesCount: s.messages.length,
          ...(s.archivedAt ? { archivedAt: s.archivedAt } : {}),
          ...(s.tags ? { tags: s.tags } : {}),
          ...(s.subagent
            ? {
                callingSessionId: s.subagent.callingSessionId,
                callingToolUseId: s.subagent.callingToolUseId,
              }
            : {}),
        })),
        hasMore: listed.length > page.length,
        ...(listed.length > page.length && last ? { nextCursor: last.updatedAt } : {}),
      }
    },
    // A plain substring search over titles and message text, with the Daemon's
    // <mark> highlighting in one snippet per Session.
    'daemon.search_sessions': (params) => {
      const query = String(params['query']).toLowerCase()
      const limit = typeof params['limitSessions'] === 'number' ? params['limitSessions'] : 20
      const found = sessions
        .filter((s) => !s.archivedAt)
        .flatMap((s) => {
          const texts = s.messages.flatMap((m) =>
            (m.content ?? []).flatMap((block) =>
              block['type'] === 'text' ? [String(block['text'])] : [],
            ),
          )
          const hit = texts.find((text) => text.toLowerCase().includes(query))
          if (!hit && !s.title.toLowerCase().includes(query)) return []
          const at = hit ? hit.toLowerCase().indexOf(query) : -1
          const snippet = hit
            ? `${hit.slice(Math.max(0, at - 40), at)}<mark>${hit.slice(at, at + query.length)}</mark>${hit.slice(at + query.length, at + query.length + 40)}`
            : null
          return [
            {
              sessionId: s.sessionId,
              title: s.title,
              updatedAt: s.updatedAt * 1000,
              hits: snippet
                ? [{ docId: `${s.sessionId}:0`, kind: 'message_text', snippets: [snippet] }]
                : [],
            },
          ]
        })
        .sort((a, b) => b.updatedAt - a.updatedAt)
      return { query: params['query'], sessions: found.slice(0, limit) }
    },
    'daemon.list_opened_sessions': () => ({
      sessions: (input.opened ?? []).map((open) => {
        const found = mustFind(sessions, open.sessionId)
        return {
          sessionId: found.sessionId,
          updatedAt: found.updatedAt,
          workingState: open.workingState,
          cwd: found.cwd,
          messagesCount: found.messages.length,
        }
      }),
    }),
    'daemon.update_session_settings': (params, context) => {
      const found = mustFind(sessions, params['sessionId'])
      if (found.inactive) throw new Error('No active session found for ID')
      const { sessionId: _ignored, ...patch } = params
      found.settings = { ...sessionSettings(), ...found.settings, ...patch }
      if (Array.isArray(patch['tags'])) found.tags = patch['tags'] as SessionFixture['tags']
      context.daemon.notify(found.sessionId, { type: 'settings_updated', settings: found.settings })
      return {}
    },
    'daemon.rename_session': (params, context) => {
      const found = mustFind(sessions, params['sessionId'])
      found.title = String(params['title'])
      context.daemon.notify(found.sessionId, { type: 'session_title_updated', title: found.title })
      return { success: true }
    },
    'daemon.archive_session': (params, context) => {
      const found = mustFind(sessions, params['sessionId'])
      found.archivedAt = new Date().toISOString()
      context.daemon.notifyArchiveState(found.sessionId, found.archivedAt)
      return { success: true, archivedAt: found.archivedAt }
    },
    'daemon.get_default_settings': () => ({ ...defaults, availableModels: models }),
    // The real Daemon merges the patch, drops keys sent as null, and replaces
    // subagentModelSettings as a whole.
    'daemon.update_session_defaults': (params) => {
      const next = { ...defaults }
      for (const [key, value] of Object.entries(params)) {
        if (value === null) delete next[key]
        else next[key] = value
      }
      defaults = next
      return { success: true, defaults: { ...defaults, availableModels: models } }
    },
    'daemon.get_context_breakdown': (params) => {
      const found = mustFind(sessions, params['sessionId'])
      const usedTokens = input.contextUsedTokens ?? found.messages.length * 1_000
      return {
        modelId: 'claude-fable-5.1',
        modelDisplayName: 'Claude Fable 5.1',
        contextBudget: CONTEXT_BUDGET,
        usedTokens,
        freeTokens: CONTEXT_BUDGET - usedTokens,
        categories: [{ name: 'Messages', tokens: usedTokens, colorKey: 'messages' }],
        skills: [],
        mcpServers: [],
        droids: [],
      }
    },
    'daemon.get_git_diff': (params) => {
      const found = mustFind(sessions, params['sessionId'])
      if (!found.git) {
        return {
          success: false,
          unavailableReason: 'not_git_repository',
          unavailableMessage: 'This session working directory is not a Git repository.',
        }
      }
      const sum = (key: 'additions' | 'deletions') =>
        found.git!.files.reduce((total, file) => total + file[key], 0)
      const range = {
        diff: '',
        files: found.git.files,
        totalAdditions: sum('additions'),
        totalDeletions: sum('deletions'),
      }
      return {
        success: true,
        data: {
          branch: found.git.branch,
          baseBranch: 'main',
          remoteUrl: null,
          commits: [],
          ...range,
          committedDiff: '',
          committedFiles: [],
          committedTotalAdditions: 0,
          committedTotalDeletions: 0,
          localDiff: range.diff,
          localFiles: range.files,
          localTotalAdditions: range.totalAdditions,
          localTotalDeletions: range.totalDeletions,
          unstagedDiff: range.diff,
          unstagedFiles: range.files,
          unstagedTotalAdditions: range.totalAdditions,
          unstagedTotalDeletions: range.totalDeletions,
        },
      }
    },
    'daemon.get_workspace_file_content': (params) => {
      const found = mustFind(sessions, params['sessionId'])
      const requested = String(params['filePath'])
      // The Daemon resolves a relative path against the Session's Workspace.
      const path = requested.startsWith('/') ? requested : `${found.cwd}/${requested}`
      const file = files[path]
      if (!file) throw new RpcError(-32000, `ENOENT: no such file or directory, open '${path}'`)
      return {
        content: file.base64,
        byteLength: Buffer.from(file.base64, 'base64').byteLength,
        encoding: 'base64',
        mimeType: file.mimeType,
      }
    },
    'daemon.list_commands': (params) => {
      activeOrThrow(params['sessionId'])
      return { commands: input.commands ?? [] }
    },
    ...tools,
    'daemon.list_skills': (params, context, request) => {
      activeOrThrow(params['sessionId'])
      return tools['daemon.list_skills']!(params, context, request)
    },
    'daemon.validate_working_directory': (params, context) => {
      const path = String(params['workingDirectory'])
      const known = new Set([
        ...(input.validDirectories ?? []),
        ...sessions.map((s) => s.cwd),
        ...context.daemon.scratchFolders,
      ])
      return known.has(path)
        ? { isValid: true, resolvedPath: path }
        : { isValid: false, error: `Directory does not exist: ${path}` }
    },
    'daemon.initialize_session': (params) => {
      const title = typeof params['title'] === 'string' ? params['title'] : 'New session'
      const created = session(title, String(params['cwd']), [], {
        ...(typeof params['sessionId'] === 'string' ? { sessionId: params['sessionId'] } : {}),
        ...(Array.isArray(params['tags'])
          ? { tags: params['tags'] as SessionFixture['tags'] }
          : {}),
      })
      created.unwritten = true
      sessions.unshift(created)
      return {
        sessionId: created.sessionId,
        hostId: HOST_ID,
        session: { messages: [], title: created.title },
        // Like the real Daemon, a mode asked for here is the Session's for good.
        settings: {
          ...sessionSettings(),
          ...(typeof params['toolExecutionMode'] === 'string'
            ? { toolExecutionMode: params['toolExecutionMode'] }
            : {}),
        },
        availableModels: models,
      }
    },
    // Like the real Daemon, closing a Session nobody wrote to deletes it.
    'daemon.close_session': (params) => {
      const index = sessions.findIndex((s) => s.sessionId === params['sessionId'])
      const found = sessions[index]
      if (found && found.messages.length === 0) sessions.splice(index, 1)
      else if (found) found.inactive = true
      return {}
    },
    // `/compact` is a handoff: the summary starts a new Session that carries
    // the parent's tags; the parent stays listed.
    'daemon.compact_session': (params) => {
      const found = mustFind(sessions, params['sessionId'])
      const child = session(found.title, found.cwd, [
        assistantMessage(
          `Summary of the earlier conversation (${found.messages.length} messages).`,
        ),
      ])
      if (found.tags) child.tags = [...found.tags]
      child.inactive = true
      sessions.unshift(child)
      return { newSessionId: child.sessionId, removedCount: found.messages.length }
    },
    'daemon.unarchive_session': (params, context) => {
      const found = mustFind(sessions, params['sessionId'])
      delete found.archivedAt
      context.daemon.notifyArchiveState(found.sessionId, undefined)
      return { success: true }
    },
    // The messages before the cursor (a message id), newest first, as the Daemon pages them;
    // with `role`, only that role's.
    'daemon.get_session_messages': (params) => {
      const found = mustFind(sessions, params['sessionId'])
      const limit = typeof params['limit'] === 'number' ? params['limit'] : 20
      const cursor = params['cursor']
      const role = params['role']
      const messages =
        typeof role === 'string' ? found.messages.filter((m) => m.role === role) : found.messages
      const end =
        typeof cursor === 'string' ? messages.findIndex((m) => m.id === cursor) : messages.length
      if (end < 0) throw new RpcError(-32602, `No message ${String(cursor)}`)
      const start = Math.max(0, end - limit)
      const page = messages.slice(start, end).reverse()
      return {
        messages: page,
        hasMore: start > 0,
        ...(start > 0 ? { nextCursor: page[page.length - 1]!.id } : {}),
      }
    },
    'daemon.load_session': async (params) => {
      const found = sessions.find((s) => s.sessionId === params['sessionId'])
      if (!found) throw new Error(`Scenario has no session ${String(params['sessionId'])}`)
      if (input.loadDelayMs) await new Promise((resolve) => setTimeout(resolve, input.loadDelayMs))
      delete found.inactive
      const subagentInvocations = sessions.flatMap((s) =>
        s.subagent?.callingSessionId === found.sessionId
          ? [
              {
                childSessionId: s.sessionId,
                status: s.subagent.status,
                subagentType: s.subagent.subagentType,
                description: s.subagent.description,
                ...(s.subagent.toolUseCount !== undefined
                  ? { toolUseCount: s.subagent.toolUseCount }
                  : {}),
                ...(s.subagent.durationMs !== undefined
                  ? { durationMs: s.subagent.durationMs }
                  : {}),
              },
            ]
          : [],
      )
      return {
        ...loadSessionResult(found, params['messageLimit']),
        availableModels: models,
        ...(found.subagent
          ? {
              callingSessionId: found.subagent.callingSessionId,
              callingToolUseId: found.subagent.callingToolUseId,
            }
          : {}),
        ...(subagentInvocations.length > 0 ? { subagentInvocations } : {}),
      }
    },
    ...input.handlers,
  }

  return {
    sessions,
    files,
    handlers,
    on(method, handler) {
      handlers[method] = handler
    },
    async handle(request, context) {
      const handler = handlers[request.method]
      if (!handler) {
        throw new Error(
          `Scenario has no handler for ${request.method}. Params: ${JSON.stringify(request.params).slice(0, 300)}`,
        )
      }
      const result = await handler(request.params, context, request)
      // The real Daemon writes a Session on its first message and then names
      // it; the title notification is how Clients learn it is listed now.
      const written = sessions.find((s) => s.sessionId === request.params['sessionId'])
      if (request.method === 'daemon.add_user_message' && written?.unwritten) {
        delete written.unwritten
        context.daemon.notify(written.sessionId, {
          type: 'session_title_updated',
          title: written.title,
        })
      }
      return result
    },
  }
}

export const HOST_ID = '11111111-1111-4111-8111-111111111111'

function mustFind(sessions: SessionFixture[], sessionId: unknown): SessionFixture {
  const found = sessions.find((s) => s.sessionId === sessionId)
  if (!found) throw new Error(`Scenario has no session ${String(sessionId)}`)
  return found
}

/** Like the Daemon, a load brings the most recent `messageLimit` messages (all without one). */
export function loadSessionResult(
  fixture: SessionFixture,
  messageLimit: unknown = undefined,
): Record<string, unknown> {
  const limit = typeof messageLimit === 'number' ? messageLimit : fixture.messages.length
  return {
    session: { messages: fixture.messages.slice(-limit), title: fixture.title },
    hasOlderMessages: fixture.messages.length > limit,
    hostId: HOST_ID,
    settings: { ...sessionSettings(), ...fixture.settings },
    isAgentLoopInProgress: false,
    workingState: 'idle',
    cwd: fixture.cwd,
    updatedAt: fixture.updatedAt,
    availableModels: AVAILABLE_MODELS,
    tokenUsage: emptyTokenUsage(),
    inclusiveTokenUsage: emptyTokenUsage(),
  }
}

export const AVAILABLE_MODELS = [
  {
    id: 'auto',
    displayName: 'Auto Model',
    shortDisplayName: 'Auto',
    modelProvider: 'factory',
    supportedReasoningEfforts: ['none'],
    defaultReasoningEffort: 'none',
    isCustom: false,
    tokenMultiplier: 1,
    kind: 'router',
  },
  {
    id: 'claude-opus-4-1',
    displayName: 'Claude Opus 4.1',
    shortDisplayName: 'Opus 4.1',
    modelProvider: 'anthropic',
    supportedReasoningEfforts: ['off', 'low', 'medium', 'high'],
    defaultReasoningEffort: 'medium',
    isCustom: false,
    tokenMultiplier: 1.6,
  },
  {
    id: 'gpt-5',
    displayName: 'GPT-5',
    shortDisplayName: 'GPT-5',
    modelProvider: 'openai',
    supportedReasoningEfforts: ['low', 'medium', 'high', 'xhigh'],
    defaultReasoningEffort: 'medium',
    isCustom: false,
    tokenMultiplier: 0.8,
  },
]

export function sessionSettings(): Record<string, unknown> {
  return {
    modelId: 'auto',
    reasoningEffort: 'none',
    interactionMode: 'auto',
    autonomyLevel: 'low',
    availableAutonomyLevels: ['off', 'low', 'medium', 'high'],
    tags: [],
    compactionThresholdCheckEnabled: true,
    toolExecutionMode: 'direct_only',
    allowSubagentsInScripts: false,
    additionalToolIds: [],
    enabledToolIds: [],
    disabledToolIds: [],
    restrictToolIds: [],
  }
}

/** What `daemon.get_default_settings` reports before anything was changed. */
export function sessionDefaults(): Record<string, unknown> {
  return {
    modelId: 'auto',
    reasoningEffort: 'none',
    interactionMode: 'auto',
    autonomyLevel: 'low',
    availableAutonomyLevels: ['off', 'low', 'medium', 'high'],
    compactionModel: 'current-model',
    compactionThresholdCheckEnabled: true,
    subagentModelSettings: {},
    specSavePresets: { userFactoryDir: '/Users/test/.factory' },
  }
}

export function emptyTokenUsage(): Record<string, number> {
  return {
    inputTokens: 0,
    outputTokens: 0,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
    thinkingTokens: 0,
    factoryCredits: 0,
  }
}

// Fixtures are "recent" for the sidebar's three-day window: start a few hours ago.
let clock = Date.now() - 6 * 60 * 60 * 1000

export function userMessage(text: string): MessageFixture {
  clock += 1_000
  return {
    id: randomUUID(),
    role: 'user',
    content: [{ type: 'text', text }],
    createdAt: clock,
    updatedAt: clock,
    visibility: 'both',
  }
}

export function thinkingBlock(thinking: string, durationMs?: number): Record<string, unknown> {
  return { type: 'thinking', thinking, signature: '', ...(durationMs ? { durationMs } : {}) }
}

export function assistantMessage(text: string): MessageFixture {
  clock += 1_000
  return {
    id: randomUUID(),
    role: 'assistant',
    content: [{ type: 'text', text }],
    createdAt: clock,
    updatedAt: clock,
    visibility: 'both',
  }
}

export function toolCallMessage(
  id: string,
  name: string,
  input: Record<string, unknown>,
): MessageFixture {
  return { ...assistantMessage(''), content: [{ type: 'tool_use', id, name, input }] }
}

export function toolResultMessage(toolUseId: string, content: string): MessageFixture {
  return {
    ...assistantMessage(''),
    role: 'tool',
    content: [{ type: 'tool_result', toolUseId, content }],
  }
}

export function session(
  title: string,
  cwd: string,
  messages: MessageFixture[] = [],
  extra: Partial<SessionFixture> = {},
): SessionFixture {
  clock += 60_000
  return {
    sessionId: randomUUID(),
    title,
    cwd,
    updatedAt: Math.floor(clock / 1000),
    messages,
    ...extra,
  }
}
