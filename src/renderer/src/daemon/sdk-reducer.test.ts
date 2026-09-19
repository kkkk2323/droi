// Contract test: the Client renders whatever the SDK's state manager derives
// from Daemon notifications (ADR 0004). These cases pin the behaviour the
// transcript relies on so an SDK upgrade that changes it fails here first.
import { describe, expect, test } from 'vitest'
import { LOCAL_MACHINE_ID, MultiSessionStateManager } from '@factory/droid-sdk'
import type { FactoryDroidMessage } from '@factory/droid-sdk'

const SESSION = 'session-1'

function manager() {
  const state = new MultiSessionStateManager()
  state.loadSession(SESSION, LOCAL_MACHINE_ID, [], { workingState: 'idle' as never })
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
