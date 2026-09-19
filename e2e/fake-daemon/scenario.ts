// Scenario: the per-test script that tells the Fake Daemon what to answer and
// when to emit events. A Scenario starts from a list of Session fixtures and
// default handlers for the read-only methods; tests override or add handlers.
import { randomUUID } from 'node:crypto'
import type { FakeDaemon } from './fake-daemon'
import type { JsonRpcRequest } from './protocol'

export interface SessionFixture {
  sessionId: string
  title: string
  cwd: string
  updatedAt: number
  messages: MessageFixture[]
  archivedAt?: string
}

export interface MessageFixture {
  id: string
  role: 'user' | 'assistant' | 'tool'
  content: Array<Record<string, unknown>>
  createdAt: number
  updatedAt: number
  visibility: 'both'
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
}

export interface Scenario {
  sessions: SessionFixture[]
  handlers: Record<string, MethodHandler>
  handle(request: JsonRpcRequest, context: HandlerContext): Promise<unknown>
  on(method: string, handler: MethodHandler): void
}

export function createScenario(input: ScenarioInput): Scenario {
  const sessions = input.sessions ?? []
  const handlers: Record<string, MethodHandler> = {
    'daemon.list_available_sessions': () => ({
      sessions: sessions.map((s) => ({
        sessionId: s.sessionId,
        updatedAt: s.updatedAt,
        title: s.title,
        cwd: s.cwd,
        messagesCount: s.messages.length,
        ...(s.archivedAt ? { archivedAt: s.archivedAt } : {}),
      })),
      hasMore: false,
    }),
    'daemon.load_session': (params) => {
      const found = sessions.find((s) => s.sessionId === params['sessionId'])
      if (!found) throw new Error(`Scenario has no session ${String(params['sessionId'])}`)
      return loadSessionResult(found)
    },
    ...input.handlers,
  }

  return {
    sessions,
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
      return handler(request.params, context, request)
    },
  }
}

export const HOST_ID = '11111111-1111-4111-8111-111111111111'

export function loadSessionResult(fixture: SessionFixture): Record<string, unknown> {
  return {
    session: { messages: fixture.messages, title: fixture.title },
    hasOlderMessages: false,
    hostId: HOST_ID,
    settings: sessionSettings(),
    isAgentLoopInProgress: false,
    workingState: 'idle',
    cwd: fixture.cwd,
    updatedAt: fixture.updatedAt,
    availableModels: [
      {
        id: 'auto',
        displayName: 'Auto Model',
        shortDisplayName: 'Auto Model',
        modelProvider: 'factory',
        supportedReasoningEfforts: ['none'],
        defaultReasoningEffort: 'none',
        isCustom: false,
        noImageSupport: false,
        supportsImageGeneration: false,
        tokenMultiplier: 1,
        kind: 'router',
      },
    ],
    tokenUsage: emptyTokenUsage(),
    inclusiveTokenUsage: emptyTokenUsage(),
  }
}

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

let clock = 1_760_000_000_000

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
