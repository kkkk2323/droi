// Scripted agent turns for Scenarios. Each helper emits the same notification
// sequence a real Daemon produces (captured in docs/adr/0003-spike/raw.txt).
import { randomUUID } from 'node:crypto'
import type { FakeDaemon } from './fake-daemon'
import type { HandlerContext, MethodHandler } from './scenario'
import { emptyTokenUsage } from './scenario'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

export interface StreamedReplyOptions {
  /** Text chunks streamed as assistant_text_delta, in order. */
  deltas: string[]
  /** Gap between deltas. */
  delayMs?: number
  /** Called after the user message is acknowledged; return false to stop before replying. */
  onTurnStart?: (turn: TurnHandle) => void
}

export interface TurnHandle {
  sessionId: string
  turnId: string
  cancel(): void
}

/**
 * Handler for `daemon.add_user_message` that echoes the user message and then
 * streams a reply. The returned handler resolves the RPC immediately and runs
 * the turn in the background, like the real Daemon.
 */
export function streamedReply(options: StreamedReplyOptions): MethodHandler {
  return (params, context, request) => {
    const sessionId = String(params['sessionId'])
    const text = String(params['text'])
    const userMessageId =
      typeof params['messageId'] === 'string' ? params['messageId'] : randomUUID()
    void runTurn({
      daemon: context.daemon,
      sessionId,
      requestId: String(request.id),
      userMessageId,
      text,
      ...options,
    })
    return {}
  }
}

interface RunTurnInput extends StreamedReplyOptions {
  daemon: FakeDaemon
  sessionId: string
  requestId: string
  userMessageId: string
  text: string
}

const activeTurns = new Map<string, TurnHandle & { cancelled: boolean }>()

/** Handler for `daemon.interrupt_session`: ends the running turn as cancelled. */
export const interruptHandler: MethodHandler = (params, context) => {
  const sessionId = String(params['sessionId'])
  const turn = activeTurns.get(sessionId)
  if (turn) {
    turn.cancel()
    finishTurn(context.daemon, sessionId, turn.turnId, 'cancelled')
  }
  return {}
}

async function runTurn(input: RunTurnInput): Promise<void> {
  const { daemon, sessionId, requestId, userMessageId, text } = input
  const now = Date.now()
  const turn = {
    sessionId,
    turnId: userMessageId,
    cancelled: false,
    cancel() {
      this.cancelled = true
    },
  }
  activeTurns.set(sessionId, turn)

  daemon.notify(sessionId, {
    type: 'droid_working_state_changed',
    newState: 'streaming_assistant_message',
  })
  daemon.notify(sessionId, {
    type: 'create_message',
    message: {
      id: userMessageId,
      role: 'user',
      content: [{ type: 'text', text }],
      createdAt: now,
      updatedAt: now,
    },
    requestId,
  })
  daemon.notify(sessionId, { type: 'droid_working_state_changed', newState: 'thinking' })
  input.onTurnStart?.(turn)

  const assistantMessageId = randomUUID()
  let full = ''
  for (const delta of input.deltas) {
    await sleep(input.delayMs ?? 150)
    if (turn.cancelled) return
    if (full === '') {
      daemon.notify(sessionId, {
        type: 'droid_working_state_changed',
        newState: 'streaming_assistant_message',
      })
    }
    full += delta
    daemon.notify(sessionId, {
      type: 'assistant_text_delta',
      messageId: assistantMessageId,
      blockIndex: 0,
      textDelta: delta,
    })
  }
  if (turn.cancelled) return
  daemon.notify(sessionId, {
    type: 'assistant_text_complete',
    messageId: assistantMessageId,
    blockIndex: 0,
  })
  const done = Date.now()
  daemon.notify(sessionId, {
    type: 'create_message',
    message: {
      id: assistantMessageId,
      role: 'assistant',
      content: [{ type: 'text', text: full }],
      createdAt: done,
      updatedAt: done,
    },
    parentId: userMessageId,
  })
  finishTurn(daemon, sessionId, userMessageId, 'completed')
}

function finishTurn(
  daemon: FakeDaemon,
  sessionId: string,
  turnId: string,
  reason: 'completed' | 'cancelled',
): void {
  activeTurns.delete(sessionId)
  daemon.notify(sessionId, {
    type: 'agent_turn_completed',
    reason,
    turnId,
    tokenUsage: emptyTokenUsage(),
  })
  daemon.notify(sessionId, { type: 'droid_working_state_changed', newState: 'idle' })
}

export type { HandlerContext }
