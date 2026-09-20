// Scripted agent turns for Scenarios. Each helper emits the same notification
// sequence a real Daemon produces (captured in docs/adr/0003-spike/raw.txt).
import { randomUUID } from 'node:crypto'
import type { FakeDaemon } from './fake-daemon'
import type { JsonRpcRequest } from './protocol'
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
      content: Array.isArray(params['content'])
        ? (params['content'] as Array<Record<string, unknown>>)
        : undefined,
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
  /** Full content blocks when the Client sent more than text (images). */
  content?: Array<Record<string, unknown>>
}

const activeTurns = new Map<string, TurnHandle & { cancelled: boolean }>()

interface HeldMessage {
  request: JsonRpcRequest
  params: Record<string, unknown>
  context: HandlerContext
  placement: 'end_of_turn' | 'end_of_loop'
}

/** Messages a Session received while a turn ran, in arrival order. */
const heldMessages = new Map<string, HeldMessage[]>()
const queueHandlers = new Map<string, MethodHandler>()

/**
 * Wraps an `add_user_message` handler so that, like the real Daemon, a message
 * arriving mid-turn is held and run after the turn: `end_of_turn` ones first,
 * then `end_of_loop` ones.
 */
export function withQueue(handler: MethodHandler): MethodHandler {
  return (params, context, request) => {
    const sessionId = String(params['sessionId'])
    queueHandlers.set(sessionId, handler)
    if (!activeTurns.has(sessionId)) return handler(params, context, request)
    const placement = params['queuePlacement'] === 'end_of_turn' ? 'end_of_turn' : 'end_of_loop'
    const held = heldMessages.get(sessionId) ?? []
    held.push({ request, params, context, placement })
    heldMessages.set(sessionId, held)
    return {}
  }
}

/** Handler for `daemon.resolve_queued_user_message`: only `delete` is scripted. */
export const resolveQueuedHandler: MethodHandler = (params) => {
  const sessionId = String(params['sessionId'])
  const held = heldMessages.get(sessionId) ?? []
  heldMessages.set(
    sessionId,
    held.filter((m) => String(m.request.id) !== String(params['requestId'])),
  )
  return {}
}

function drainHeld(sessionId: string): void {
  const held = heldMessages.get(sessionId) ?? []
  const next = held.find((m) => m.placement === 'end_of_turn') ?? held[0]
  const handler = queueHandlers.get(sessionId)
  if (!next || !handler) return
  heldMessages.set(
    sessionId,
    held.filter((m) => m !== next),
  )
  void handler(next.params, next.context, next.request)
}

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
      content: input.content ?? [{ type: 'text', text }],
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
  reason: 'completed' | 'cancelled' | 'permission_rejected',
): void {
  activeTurns.delete(sessionId)
  daemon.notify(sessionId, {
    type: 'agent_turn_completed',
    reason,
    turnId,
    tokenUsage: emptyTokenUsage(),
  })
  daemon.notify(sessionId, { type: 'droid_working_state_changed', newState: 'idle' })
  if (reason === 'completed') drainHeld(sessionId)
  else heldMessages.delete(sessionId)
}

export type { HandlerContext }

export interface PermissionTurnOptions {
  command: string
  /** Reply streamed after the tool ran. */
  reply: string
  /** Text of the tool result when allowed. */
  toolOutput?: string
}

/**
 * Handler for `daemon.add_user_message` where the agent wants to run a shell
 * command and asks every attached Client for permission first (ADR 0003).
 */
export function permissionTurn(options: PermissionTurnOptions): MethodHandler {
  return (params, context, request) => {
    const sessionId = String(params['sessionId'])
    const daemon = context.daemon
    const userMessageId =
      typeof params['messageId'] === 'string' ? params['messageId'] : randomUUID()
    void (async () => {
      startTurn(daemon, sessionId, userMessageId, String(params['text']), String(request.id))
      const toolUseId = `call_${randomUUID().slice(0, 8)}`
      const toolUse = {
        type: 'tool_use',
        id: toolUseId,
        name: 'Execute',
        input: { command: options.command, summary: 'Run a command', riskLevel: 'low' },
      }
      daemon.notify(sessionId, { type: 'tool_call', toolUse })
      daemon.notify(sessionId, {
        type: 'droid_working_state_changed',
        newState: 'waiting_for_tool_confirmation',
      })
      const { id, answer } = daemon.request('daemon.request_permission', {
        sessionId,
        toolUses: [
          {
            toolUse,
            confirmationType: 'exec',
            details: {
              type: 'exec',
              fullCommand: options.command,
              command: options.command.split(' ')[0],
              impactLevel: 'low',
            },
          },
        ],
        options: [
          { label: 'Yes, allow', value: 'proceed_once' },
          { label: 'Yes, and always allow low impact commands', value: 'proceed_always' },
          { label: 'No, cancel', value: 'cancel' },
        ],
      })
      const response = await answer
      const selectedOption = String(
        (response['result'] as Record<string, unknown> | undefined)?.['selectedOption'] ?? 'cancel',
      )
      daemon.notify(sessionId, {
        type: 'permission_resolved',
        requestId: id,
        toolUseIds: [toolUseId],
        selectedOption,
      })
      if (selectedOption === 'cancel') {
        const now = Date.now()
        daemon.notify(sessionId, {
          type: 'tool_result',
          toolUseId,
          content: 'Command cancelled by user.',
          isError: true,
          messageId: randomUUID(),
        })
        daemon.notify(sessionId, {
          type: 'create_message',
          message: {
            id: randomUUID(),
            role: 'assistant',
            content: [{ type: 'text', text: 'Understood, I will not run that.' }],
            createdAt: now,
            updatedAt: now,
          },
        })
        finishTurn(daemon, sessionId, userMessageId, 'permission_rejected')
        return
      }
      daemon.notify(sessionId, { type: 'droid_working_state_changed', newState: 'executing_tool' })
      await sleep(100)
      daemon.notify(sessionId, {
        type: 'tool_result',
        toolUseId,
        content: options.toolOutput ?? 'ok\n\n[Process exited with code 0]',
        isError: false,
        messageId: randomUUID(),
      })
      await streamText(daemon, sessionId, options.reply)
      finishTurn(daemon, sessionId, userMessageId, 'completed')
    })()
    return {}
  }
}

export interface AskUserTurnOptions {
  question: string
  options: string[]
  multiSelect?: boolean
}

/** Handler for `daemon.add_user_message` where the agent asks the user a question first. */
export function askUserTurn(options: AskUserTurnOptions): MethodHandler {
  return (params, context, request) => {
    const sessionId = String(params['sessionId'])
    const daemon = context.daemon
    const userMessageId =
      typeof params['messageId'] === 'string' ? params['messageId'] : randomUUID()
    void (async () => {
      startTurn(daemon, sessionId, userMessageId, String(params['text']), String(request.id))
      const toolUseId = `call_${randomUUID().slice(0, 8)}`
      daemon.notify(sessionId, {
        type: 'tool_call',
        toolUse: { type: 'tool_use', id: toolUseId, name: 'AskUser', input: {} },
      })
      daemon.notify(sessionId, {
        type: 'droid_working_state_changed',
        newState: 'waiting_for_tool_confirmation',
      })
      const { answer } = daemon.request('daemon.ask_user', {
        sessionId,
        toolCallId: toolUseId,
        questions: [
          {
            index: 1,
            topic: 'Choice',
            question: options.question,
            options: options.options,
            ...(options.multiSelect ? { multiSelect: true } : {}),
          },
        ],
      })
      const response = await answer
      const answers = ((response['result'] as Record<string, unknown> | undefined)?.['answers'] ??
        []) as Array<{ answer: string }>
      const chosen = answers.map((a) => a.answer).join(', ')
      daemon.notify(sessionId, {
        type: 'tool_result',
        toolUseId,
        content: `[answer] ${chosen}`,
        isError: false,
        messageId: randomUUID(),
      })
      await streamText(daemon, sessionId, `You chose ${chosen}.`)
      finishTurn(daemon, sessionId, userMessageId, 'completed')
    })()
    return {}
  }
}

function startTurn(
  daemon: FakeDaemon,
  sessionId: string,
  userMessageId: string,
  text: string,
  requestId: string,
): void {
  const now = Date.now()
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
}

async function streamText(daemon: FakeDaemon, sessionId: string, text: string): Promise<void> {
  const messageId = randomUUID()
  daemon.notify(sessionId, {
    type: 'droid_working_state_changed',
    newState: 'streaming_assistant_message',
  })
  const words = text.split(' ')
  for (let i = 0; i < words.length; i++) {
    await sleep(40)
    daemon.notify(sessionId, {
      type: 'assistant_text_delta',
      messageId,
      blockIndex: 0,
      textDelta: (i === 0 ? '' : ' ') + words[i],
    })
  }
  daemon.notify(sessionId, { type: 'assistant_text_complete', messageId, blockIndex: 0 })
  const now = Date.now()
  daemon.notify(sessionId, {
    type: 'create_message',
    message: {
      id: messageId,
      role: 'assistant',
      content: [{ type: 'text', text }],
      createdAt: now,
      updatedAt: now,
    },
  })
}

export interface TodoTurnOptions {
  todos: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' }>
  reply: string
}

/** Handler for `daemon.add_user_message` where the agent writes a task list, then replies. */
export function todoTurn(options: TodoTurnOptions): MethodHandler {
  return (params, context, request) => {
    const sessionId = String(params['sessionId'])
    const daemon = context.daemon
    const userMessageId =
      typeof params['messageId'] === 'string' ? params['messageId'] : randomUUID()
    void (async () => {
      startTurn(daemon, sessionId, userMessageId, String(params['text']), String(request.id))
      activeTurns.set(sessionId, {
        sessionId,
        turnId: userMessageId,
        cancelled: false,
        cancel() {},
      })
      const toolUseId = `call_${randomUUID().slice(0, 8)}`
      daemon.notify(sessionId, {
        type: 'tool_call',
        toolUse: {
          type: 'tool_use',
          id: toolUseId,
          name: 'TodoWrite',
          input: { todos: options.todos.map((t) => ({ ...t, priority: 'medium' })) },
        },
      })
      daemon.notify(sessionId, { type: 'droid_working_state_changed', newState: 'executing_tool' })
      await sleep(50)
      daemon.notify(sessionId, {
        type: 'tool_result',
        toolUseId,
        content: 'TODO List Updated',
        isError: false,
        messageId: randomUUID(),
      })
      await streamText(daemon, sessionId, options.reply)
      finishTurn(daemon, sessionId, userMessageId, 'completed')
    })()
    return {}
  }
}
