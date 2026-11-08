// Pi entry drafts for `conversation.import`: a seeded Session's history, as the Kernel would have written it.
// Every message carries its own time, which is what dates the Session in the list.
import { MODEL_ID, PROVIDER_ID } from './model'

export type EntryDraft = { kind: string; model?: unknown[]; data?: Record<string, unknown> }

let clock = Date.now() - 60 * 60_000

/** A time a minute after the last one handed out, so a history reads in order. */
export function nextTime(): number {
  clock += 60_000
  return clock
}

export function userEntry(text: string, at = nextTime()): EntryDraft {
  return { kind: 'pi.user', model: [{ role: 'user', content: text, timestamp: at }] }
}

export type AssistantBlock =
  | { type: 'text'; text: string }
  | { type: 'thinking'; thinking: string }
  | { type: 'toolCall'; id: string; name: string; arguments: Record<string, unknown> }

export function assistantEntry(content: string | AssistantBlock[], at = nextTime()): EntryDraft {
  return {
    kind: 'pi.assistant',
    model: [
      {
        role: 'assistant',
        content: typeof content === 'string' ? [{ type: 'text', text: content }] : content,
        provider: PROVIDER_ID,
        model: MODEL_ID,
        stopReason: 'stop',
        timestamp: at,
      },
    ],
  }
}

export function toolResultEntry(
  call: { id: string; name: string },
  text: string,
  options: { isError?: boolean; details?: unknown; at?: number } = {},
): EntryDraft {
  return {
    kind: 'pi.tool-result',
    model: [
      {
        role: 'toolResult',
        toolCallId: call.id,
        toolName: call.name,
        content: [{ type: 'text', text }],
        isError: options.isError ?? false,
        ...(options.details === undefined ? {} : { details: options.details }),
        timestamp: options.at ?? nextTime(),
      },
    ],
  }
}

/** A question and its answer. */
export function exchange(question: string, answer: string): EntryDraft[] {
  return [userEntry(question), assistantEntry(answer)]
}
