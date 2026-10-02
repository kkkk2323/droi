// Contract test: the Client renders whatever the SDK's state manager derives
// from Daemon notifications (ADR 0004). These cases pin the behaviour the
// transcript relies on so an SDK upgrade that changes it fails here first.
import { describe, expect, test } from 'vitest'
import {
  DaemonRequestPermissionSchema,
  LOCAL_MACHINE_ID,
  SessionNotificationPayloadSchema,
} from '@factory/droid-sdk'
import type { FactoryDroidMessage } from '@factory/droid-sdk'
import { createSessionState } from './connection'

const SESSION = 'session-1'

function manager(history: FactoryDroidMessage[] = []) {
  const state = createSessionState()
  state.loadSession(SESSION, LOCAL_MACHINE_ID, history as never, { workingState: 'idle' as never })
  const notify = (notification: Record<string, unknown>) =>
    state.handleNotification({ sessionId: SESSION, notification } as never)
  const messages = () => state.getDisplayMessages(SESSION)
  const working = () => state.getSessionManager(SESSION)!.getDroidWorkingState() as string
  return { state, notify, messages, working }
}

const text = (message: FactoryDroidMessage) =>
  message.content
    .filter((b) => b.type === 'text')
    .map((b) => (b as { text: string }).text)
    .join('')

describe('SDK state manager as the turn reducer', () => {
  test('text deltas grow one assistant message, completion keeps it, turn end goes idle', () => {
    const m = manager()
    m.notify({ type: 'droid_working_state_changed', newState: 'streaming_assistant_message' })
    m.notify({ type: 'assistant_text_delta', messageId: 'a1', blockIndex: 0, textDelta: 'Hel' })
    m.notify({ type: 'assistant_text_delta', messageId: 'a1', blockIndex: 0, textDelta: 'lo' })
    expect(m.messages().map(text)).toEqual(['Hello'])
    expect(m.working()).toBe('streaming_assistant_message')

    m.notify({ type: 'assistant_text_complete', messageId: 'a1', blockIndex: 0 })
    m.notify({
      type: 'create_message',
      message: {
        id: 'a1',
        role: 'assistant',
        content: [{ type: 'text', text: 'Hello' }],
        createdAt: 1,
        updatedAt: 1,
      },
    })
    m.notify({ type: 'agent_turn_completed', reason: 'completed', tokenUsage: usage() })
    m.notify({ type: 'droid_working_state_changed', newState: 'idle' })
    expect(m.messages().map(text)).toEqual(['Hello'])
    expect(m.working()).toBe('idle')
  })

  test('thinking deltas build a reasoning block', () => {
    const m = manager()
    m.notify({ type: 'thinking_text_delta', messageId: 'a2', blockIndex: 0, textDelta: 'plan ' })
    m.notify({ type: 'thinking_text_delta', messageId: 'a2', blockIndex: 0, textDelta: 'more' })
    m.notify({ type: 'thinking_text_complete', messageId: 'a2', blockIndex: 0, durationMs: 50 })
    const [message] = m.messages()
    expect(message?.content[0]).toMatchObject({ type: 'thinking', thinking: 'plan more' })
  })

  test('tool call then tool result are both present', () => {
    const m = manager()
    m.notify({
      type: 'tool_call',
      toolUse: { type: 'tool_use', id: 'call-1', name: 'Execute', input: { command: 'ls' } },
    })
    m.notify({ type: 'droid_working_state_changed', newState: 'executing_tool' })
    expect(m.working()).toBe('executing_tool')
    m.notify({
      type: 'tool_result',
      toolUseId: 'call-1',
      content: 'file.txt',
      isError: false,
      messageId: 't1',
    })
    const all = m.messages()
    const toolUse = all.flatMap((msg) => msg.content).find((b) => b.type === 'tool_use')
    const toolResult = all.flatMap((msg) => msg.content).find((b) => b.type === 'tool_result')
    expect(toolUse).toMatchObject({ id: 'call-1', name: 'Execute' })
    expect(toolResult).toMatchObject({ toolUseId: 'call-1', content: 'file.txt' })
  })

  // The SDK drops a notification that fails its schema. An assistant message
  // it cannot parse never replaces the placeholder its tool_call made, so every
  // later tool call of the turn piles into that placeholder, away from its reasoning.
  test('an assistant message from a provider the SDK does not list still parses', () => {
    const parsed = SessionNotificationPayloadSchema.safeParse({
      type: 'create_message',
      message: {
        id: 'a5',
        role: 'assistant',
        content: [{ type: 'tool_use', id: 'call-2', name: 'Execute', input: { command: 'ls' } }],
        createdAt: 1,
        updatedAt: 1,
        modelId: 'claude-opus-5-5',
        apiProvider: 'azure_anthropic',
      },
    })
    expect(parsed.success).toBe(true)
  })

  // droid-sdk 0.9.1 had no `script` confirmation; the Daemon's one request
  // for a whole Script run was dropped and the Session waited forever.
  test("a Script's one-time permission request parses", () => {
    const parsed = DaemonRequestPermissionSchema.safeParse({
      type: 'request',
      jsonrpc: '2.0',
      factoryApiVersion: '1.0.0',
      id: 'r1',
      method: 'daemon.request_permission',
      params: {
        sessionId: SESSION,
        toolUses: [
          {
            toolUse: { type: 'tool_use', id: 'run', name: 'Script', input: { script: 'x' } },
            confirmationType: 'script',
            details: {
              type: 'script',
              impactLevel: 'medium',
              calls: [
                {
                  toolName: 'Execute',
                  line: 5,
                  column: 26,
                  toolInput: { command: 'touch probe.txt', riskLevel: 'medium' },
                  confirmation: {
                    type: 'exec',
                    fullCommand: 'touch probe.txt',
                    impactLevel: 'medium',
                  },
                },
              ],
            },
          },
        ],
        options: [{ label: 'Yes, allow all', value: 'proceed_once' }],
      },
    })
    expect(parsed.success).toBe(true)
  })

  test("a Script's calls arrive after it, tagged with the run", () => {
    const m = manager()
    const script = { type: 'tool_use', id: 'run', name: 'Script', input: { script: 'x' } }
    m.notify({ type: 'tool_call', toolUse: script })
    m.notify({
      type: 'create_message',
      message: { id: 'a6', role: 'assistant', content: [script], createdAt: 1, updatedAt: 1 },
    })
    m.notify({
      type: 'create_message',
      message: {
        id: 'repl-tool-call:run-1',
        role: 'assistant',
        content: [
          {
            type: 'tool_use',
            id: 'run-1',
            name: 'Read',
            input: { file_path: 'README.md' },
            scriptExecution: { runId: 'run', outerToolUseId: 'run' },
          },
        ],
        createdAt: 2,
        updatedAt: 2,
        visibility: 'user_only',
      },
    })
    const uses = m
      .messages()
      .flatMap((msg) => msg.content)
      .filter((b) => b.type === 'tool_use')
    expect(uses).toMatchObject([
      { id: 'run', name: 'Script' },
      { id: 'run-1', scriptExecution: { runId: 'run', outerToolUseId: 'run' } },
    ])
  })

  test('a retracted assistant message disappears', () => {
    const m = manager()
    m.notify({ type: 'assistant_text_delta', messageId: 'a3', blockIndex: 0, textDelta: 'oops' })
    expect(m.messages()).toHaveLength(1)
    m.notify({ type: 'assistant_message_retracted', messageId: 'a3' })
    expect(m.messages()).toHaveLength(0)
  })

  test('a cancelled turn keeps the partial text and returns to idle', () => {
    const m = manager()
    m.notify({ type: 'droid_working_state_changed', newState: 'streaming_assistant_message' })
    m.notify({ type: 'assistant_text_delta', messageId: 'a4', blockIndex: 0, textDelta: 'partial' })
    m.notify({ type: 'agent_turn_completed', reason: 'cancelled', tokenUsage: usage() })
    m.notify({ type: 'droid_working_state_changed', newState: 'idle' })
    expect(m.messages().map(text)).toEqual(['partial'])
    expect(m.working()).toBe('idle')
  })
})

describe('long and compacted Sessions', () => {
  const history = (count: number): FactoryDroidMessage[] =>
    Array.from({ length: count }, (_, i) => ({
      id: `m${i}`,
      role: (i % 2 ? 'assistant' : 'user') as never,
      content: [{ type: 'text', text: `message ${i}` }] as never,
      createdAt: i + 1,
      updatedAt: i + 1,
      ...(i > 0 ? { parentId: `m${i - 1}` } : {}),
    }))

  test('every message of a long Session is shown, not just the last 30', () => {
    expect(manager(history(80)).messages()).toHaveLength(80)
  })

  test('the Daemon compacting the context in place keeps the earlier messages', () => {
    const m = manager(history(40))
    m.notify({
      type: 'session_compacted',
      summaryId: 'summary-1',
      removedCount: 20,
      visibleBoundaryMessageId: 'm20',
    })
    expect(m.messages()).toHaveLength(40)
    expect(text(m.messages()[0]!)).toBe('message 0')
  })
})

function usage() {
  return {
    inputTokens: 0,
    outputTokens: 0,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
    thinkingTokens: 0,
    factoryCredits: 0,
  }
}
