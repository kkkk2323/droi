import type { Context } from '@earendil-works/chord'
import { CompactionTask, defineExtension, hook } from '@earendil-works/pi-durable'
import type { Extension, HookApi, ModelRef } from '@earendil-works/pi-durable'
import type { AssistantMessage, Message } from '@earendil-works/pi-ai'
import { CALL_HEADER } from '../completion.ts'

/**
 * The compaction model: the model that writes a compaction's summary in every conversation, in place of the
 * conversation's own (Settings → Session defaults → Compaction). Droid had it; pi-durable 1.5.0 always summarizes with
 * the conversation's model, so the Kernel answers Pi's `beforeCompact` hook with a summary it asked this model for.
 *
 * The request is Pi's own: its prompts and its serialization of the replaced messages, copied below from pi-durable
 * 1.5.0 (harness/compaction.js), which exports neither. A summary that fails, or a model the Kernel does not have,
 * leaves the compaction to Pi, which summarizes with the conversation's model, with its retries: the compaction is
 * then only slower, never lost. A summary is kept in the compaction task's memo, so a compaction resumed after a
 * crash places the summary it already paid for.
 *
 * What it costs counts in the Key Pool's usage, through the Host's LLM Proxy, but not in the conversation's
 * `pi.usage`: a hook has no commit to record it in.
 */
export const COMPACTION_MODEL_EXTENSION = 'compaction-model'

/** The memo the summary is kept in, per compaction task. */
const MEMO = 'droi.compaction-summary'

export function compactionModelExtension(options: {
  /**
   * The compaction model now and its thinking level (absent: the model's default), undefined for the conversation's
   * own. Read at each compaction.
   */
  model: () => (ModelRef & { thinkingLevel?: string }) | undefined
  /** Hears why the compaction model wrote no summary. */
  warn?: (message: string) => void
}): Extension {
  return defineExtension({
    name: COMPACTION_MODEL_EXTENSION,
    hooks: [
      hook(CompactionTask, {
        beforeCompact: (compaction, api, context) =>
          summarize(options.model(), compaction, api, context, options.warn ?? (() => {})),
      }),
    ],
  })
}

async function summarize(
  ref: (ModelRef & { thinkingLevel?: string }) | undefined,
  compaction: { readonly messages: readonly Message[]; readonly instructions?: string },
  api: HookApi,
  context: Context,
  warn: (message: string) => void,
): Promise<{ summary: string } | undefined> {
  if (ref === undefined) return undefined
  const kept = await api.memo<string>(MEMO, context)
  if (kept !== undefined) return { summary: kept }
  const model = api.models.getModel(ref.provider, ref.modelId)
  const name = `${ref.provider}/${ref.modelId}`
  if (model === undefined) {
    warn(
      `the compaction model ${name} is not one the Kernel has; the conversation's own model summarizes`,
    )
    return undefined
  }
  let message: AssistantMessage
  try {
    message = await api.models.completeSimple(
      model,
      {
        messages: [
          {
            role: 'user',
            content: [
              { type: 'text', text: summaryPrompt(compaction.messages, compaction.instructions) },
            ],
            timestamp: Date.now(),
          },
        ],
        systemPrompt: SUMMARIZATION_SYSTEM_PROMPT,
      },
      {
        cacheRetention: 'none',
        // Pi's: 80% of the 16,384 tokens it reserves, within the model's own limit.
        maxTokens: Math.min(
          13_107,
          model.maxTokens > 0 ? model.maxTokens : Number.POSITIVE_INFINITY,
        ),
        headers: { [CALL_HEADER]: 'compaction' },
        ...(ref.thinkingLevel === undefined || ref.thinkingLevel === 'off'
          ? {}
          : { reasoning: ref.thinkingLevel as never }),
        ...(context.abortSignal === undefined ? {} : { signal: context.abortSignal }),
      },
    )
  } catch (error) {
    context.abortSignal?.throwIfAborted()
    warn(
      `the compaction model ${name} failed (${(error as Error).message}); the conversation's own model summarizes`,
    )
    return undefined
  }
  context.abortSignal?.throwIfAborted()
  const summary = summaryText(message)
  if (summary === undefined) {
    const why = message.errorMessage ?? message.stopReason
    warn(
      `the compaction model ${name} wrote no summary (${why}); the conversation's own model summarizes`,
    )
    return undefined
  }
  return { summary: await api.memo(MEMO, summary, context) }
}

// ── Pi's summary request, as pi-durable 1.5.0 builds it (harness/compaction.js) ──────────────────────────────────────

/** Longest tool result text a serialized summary source keeps. */
const TOOL_RESULT_MAX_CHARS = 2000

const SUMMARIZATION_SYSTEM_PROMPT = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const SUMMARIZATION_PROMPT = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work. If the conversation starts with an earlier summary, preserve its information and fold the newer messages into it.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

function summaryPrompt(messages: readonly Message[], instructions: string | undefined): string {
  const focus = instructions === undefined ? '' : `\n\nAdditional focus: ${instructions}`
  return `<conversation>\n${serializeConversation(messages)}\n</conversation>\n\n${SUMMARIZATION_PROMPT}${focus}`
}

/** The summary of a clean `stop` with text and no tool call; anything else is not a summary. */
function summaryText(message: AssistantMessage): string | undefined {
  if (
    message.stopReason !== 'stop' ||
    message.content.some((content) => content.type === 'toolCall')
  )
    return undefined
  const text = message.content
    .flatMap((content) => (content.type === 'text' ? [content.text] : []))
    .join('\n')
    .trim()
  return text.length === 0 ? undefined : text
}

export function serializeConversation(messages: readonly Message[]): string {
  const parts: string[] = []
  for (const message of messages) {
    if (message.role === 'user') {
      const text = contentText(message.content)
      if (text.length > 0) parts.push(`[User]: ${text}`)
    } else if (message.role === 'assistant') {
      const text = message.content.flatMap((content) =>
        content.type === 'text' ? [content.text] : [],
      )
      const calls = message.content.flatMap((content) =>
        content.type === 'toolCall'
          ? [
              `${content.name}(${Object.entries(content.arguments)
                .map(([key, value]) => `${key}=${JSON.stringify(value)}`)
                .join(', ')})`,
            ]
          : [],
      )
      if (text.length > 0) parts.push(`[Assistant]: ${text.join('\n')}`)
      if (calls.length > 0) parts.push(`[Assistant tool calls]: ${calls.join('; ')}`)
    } else if (message.role === 'toolResult') {
      const text = contentText(message.content)
      if (text.length > 0) parts.push(`[Tool result]: ${truncate(text, TOOL_RESULT_MAX_CHARS)}`)
    }
  }
  return parts.join('\n\n')
}

function contentText(content: string | readonly { type: string; text?: string }[]): string {
  if (typeof content === 'string') return content
  return content
    .flatMap((block) => (block.type === 'text' && block.text !== undefined ? [block.text] : []))
    .join('\n')
}

function truncate(text: string, maxChars: number): string {
  if (text.length <= maxChars) return text
  return `${text.slice(0, maxChars)}\n\n[... ${text.length - maxChars} more characters truncated]`
}
