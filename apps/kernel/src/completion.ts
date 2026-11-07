import type { AssistantMessage, MutableModels } from '@earendil-works/pi-ai'
import { compile } from '@earendil-works/pi-schema'
import { Code, RpcError, invalidParams, optionalNumber, optionalString } from './protocol/rpc.ts'

/**
 * One model call with no conversation behind it — the protocol's `model.complete`.
 *
 * It exists for the work a Host does with a model of its own: naming a conversation, and Memory's
 * summaries (ADR 0042). Those need a request and an answer, not a transcript, an archive or a record a
 * client can open, so nothing here touches a conversation, an entry or a task. The whole method is this
 * file: parse, call, check, answer.
 *
 * The call goes through the same `Models` the harness uses, so the provider, its API dialect and its key
 * are the ones the kernel already has. That is why there is no streaming: the caller waits for a small
 * answer, and a stream would only add a way for it to be cut off half-way.
 */

/**
 * The header that names this call. It is not part of the protocol: it exists so a scripted model can
 * answer a title request differently from a conversation's, and the Host's LLM proxy removes every
 * `X-Droi-*` header before the request reaches Factory.
 */
export const CALL_HEADER = 'X-Droi-Call'

/** The longest deadline a caller may ask for; anything longer is a bug on the other side. */
export const MAX_TIMEOUT_MS = 600_000
/** What a call that names no deadline gets. */
export const DEFAULT_TIMEOUT_MS = 120_000

/** The thinking levels the protocol names, in the words pi-ai uses for a request. */
const THINKING_LEVELS = new Set(['off', 'low', 'medium', 'high'])

/**
 * A provider's own words for "the model declined". pi-ai reports a refusal as an error whose message
 * says so — Anthropic's `stop_reason: "refusal"` becomes "The model refused to complete the request" —
 * so the sentence is the only signal there is, and matching it is what keeps `-32018` distinct from a
 * provider that failed.
 */
const REFUSAL = /\brefus(?:e|ed|al)\b/i

/** What `model.complete` answers. */
export type CompletionResult = {
  model: { provider: string; modelId: string }
  text: string
  json?: unknown
  usage: {
    input: number
    output: number
    totalTokens: number
    /** Dollars for this call, when the model's catalog states a price. */
    cost?: number
  }
}

export type ModelRef = { provider: string; modelId: string }

/**
 * Answer `model.complete`.
 *
 * Throws `RpcError` for everything the protocol names: an unknown model, an answer that does not match
 * the schema, a provider that failed, a refusal, a deadline, and a `maxTokens` above what the model
 * itself takes.
 */
export async function completeModel(
  models: MutableModels,
  params: Record<string, unknown>,
): Promise<CompletionResult> {
  const ref = parseModel(params['model'])
  const input = parseInput(params['input'])
  const system = optionalString(params, 'system')
  const schema = parseSchema(params['schema'])
  const thinkingLevel = optionalString(params, 'thinkingLevel')
  if (thinkingLevel !== undefined && !THINKING_LEVELS.has(thinkingLevel)) {
    throw invalidParams([
      { path: 'thinkingLevel', message: 'one of off, low, medium or high is required' },
    ])
  }
  const timeoutMs = optionalNumber(params, 'timeoutMs') ?? DEFAULT_TIMEOUT_MS
  if (timeoutMs <= 0 || timeoutMs > MAX_TIMEOUT_MS) {
    throw invalidParams([
      {
        path: 'timeoutMs',
        message: `a number of milliseconds up to ${MAX_TIMEOUT_MS} is required`,
      },
    ])
  }
  const model = models.getModel(ref.provider, ref.modelId)
  if (model === undefined) throw modelNotFound(ref)
  // The model's own ceiling, not an arbitrary number: a caller that asks for more than the model takes
  // would otherwise get a truncated answer reported as a success.
  const maxTokens = optionalNumber(params, 'maxTokens')
  if (maxTokens !== undefined && maxTokens > model.maxTokens) {
    throw new RpcError(
      Code.budgetExceeded,
      `maxTokens is above what ${ref.provider}/${ref.modelId} takes`,
      { maxTokens: model.maxTokens },
    )
  }

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(new Error('deadline')), timeoutMs)
  let message: AssistantMessage
  try {
    message = await models.completeSimple(
      model,
      {
        messages: [input],
        ...(system === undefined ? {} : { systemPrompt: system }),
      },
      {
        signal: controller.signal,
        // The one header that says who is calling. pi-ai sends every request as a stream, so a test
        // harness cannot tell this call from a conversation's request any other way; the Host's proxy
        // strips `X-Droi-*` before the request leaves the machine.
        headers: { [CALL_HEADER]: 'model.complete' },
        ...(maxTokens === undefined ? {} : { maxTokens }),
        ...(thinkingLevel === undefined || thinkingLevel === 'off'
          ? {}
          : { reasoning: thinkingLevel as 'low' | 'medium' | 'high' }),
      },
    )
  } catch (error) {
    if (controller.signal.aborted) throw deadline(timeoutMs)
    throw failed(ref.provider, error)
  } finally {
    clearTimeout(timer)
  }
  // A provider reports an aborted request as a message rather than a rejection, so the deadline is
  // checked here too: an abort nobody else could have asked for is this call's own timer.
  if (controller.signal.aborted || message.stopReason === 'aborted') throw deadline(timeoutMs)
  if (message.stopReason === 'error') {
    const said = message.errorMessage ?? ''
    if (REFUSAL.test(said)) {
      throw new RpcError(Code.modelRefused, said === '' ? 'the model refused to answer' : said, {
        reason: said,
      })
    }
    throw new RpcError(Code.modelCallFailed, said === '' ? 'the model call failed' : said, {
      provider: ref.provider,
    })
  }

  const text = textOf(message)
  const usage = {
    input: message.usage.input,
    output: message.usage.output,
    totalTokens: message.usage.totalTokens,
    // One number, in the shape the contract states and the Go client decodes. pi-ai splits the cost by
    // direction; this method reports what the call cost.
    ...(message.usage.cost.total > 0 ? { cost: message.usage.cost.total } : {}),
  }
  if (schema === undefined) {
    return { model: { provider: ref.provider, modelId: ref.modelId }, text, usage }
  }
  return {
    model: { provider: ref.provider, modelId: ref.modelId },
    text,
    json: parseAnswer(schema, text),
    usage,
  }
}

/** `{provider, modelId}` as `agent.model` takes it; `id` is accepted for an older caller's spelling. */
function parseModel(value: unknown): ModelRef {
  if (typeof value !== 'object' || value === null) {
    throw invalidParams([{ path: 'model', message: 'provider and modelId are required' }])
  }
  const model = value as { provider?: unknown; modelId?: unknown; id?: unknown }
  const modelId = model.modelId ?? model.id
  if (typeof model.provider !== 'string' || model.provider === '') {
    throw invalidParams([{ path: 'model.provider', message: 'a provider id is required' }])
  }
  if (typeof modelId !== 'string' || modelId === '') {
    throw invalidParams([{ path: 'model.modelId', message: 'a model id is required' }])
  }
  return { provider: model.provider, modelId }
}

/** A string, or text blocks. Images are not accepted: no caller of this method needs one, and each
 * dialect would carry them differently. */
function parseInput(value: unknown): {
  role: 'user'
  content: string | { type: 'text'; text: string }[]
  timestamp: number
} {
  const timestamp = Date.now()
  if (typeof value === 'string') return { role: 'user', content: value, timestamp }
  if (!Array.isArray(value)) {
    throw invalidParams([
      {
        path: 'input',
        message: 'a string, or a list of {"type":"text","text":...} blocks, is required',
      },
    ])
  }
  const content = value.map((block: unknown, index: number) => {
    const typed = block as { type?: unknown; text?: unknown } | null
    if (
      typeof typed !== 'object' ||
      typed === null ||
      typed.type !== 'text' ||
      typeof typed.text !== 'string'
    ) {
      throw invalidParams([
        { path: `input[${index}]`, message: 'only a {"type":"text","text":...} block is accepted' },
      ])
    }
    return { type: 'text' as const, text: typed.text }
  })
  return { role: 'user', content, timestamp }
}

/**
 * The schema to check the answer against, compiled once for this call. A schema this build cannot read
 * is the caller's mistake (`-32602`), not a failed model call.
 */
function parseSchema(
  value: unknown,
): ((answer: unknown) => { path: string; message: string }[]) | undefined {
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'object') {
    throw invalidParams([{ path: 'schema', message: 'a JSON Schema object is required' }])
  }
  let validator: ReturnType<typeof compile>
  try {
    validator = compile(value as Record<string, unknown>)
  } catch (error) {
    throw invalidParams([
      {
        path: 'schema',
        message: error instanceof Error ? error.message : 'this is not a JSON Schema',
      },
    ])
  }
  return (answer) =>
    validator.errors(answer).map((issue) => ({ path: issue.path, message: issue.message }))
}

/**
 * The answer as the schema describes it: parsed, then checked. The text travels with the issues, because
 * a caller that cannot use the answer still wants to see what the model said — and a refusal to parse is
 * never retried here.
 */
function parseAnswer(
  check: (answer: unknown) => { path: string; message: string }[],
  text: string,
): unknown {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new RpcError(Code.modelOutputInvalid, "the model's answer is not JSON", {
      issues: [{ path: '', message: 'the answer is not JSON' }],
      text,
    })
  }
  const issues = check(parsed)
  if (issues.length > 0) {
    throw new RpcError(
      Code.modelOutputInvalid,
      `the model's answer does not match the schema: ${issues[0]?.message ?? 'invalid'}`,
      { issues, text },
    )
  }
  return parsed
}

/** The answer's text blocks, joined. A model that answered only by thinking leaves this empty. */
function textOf(message: AssistantMessage): string {
  const parts: string[] = []
  for (const block of message.content) {
    if (block.type === 'text') parts.push(block.text)
  }
  return parts.join('')
}

function modelNotFound(ref: ModelRef): RpcError {
  return new RpcError(Code.modelNotFound, `no such model: ${ref.provider}/${ref.modelId}`, {
    model: `${ref.provider}/${ref.modelId}`,
  })
}

function deadline(timeoutMs: number): RpcError {
  return new RpcError(Code.timeout, `the model did not answer within ${timeoutMs} ms`, {
    method: 'model.complete',
    timeoutMs,
  })
}

function failed(provider: string, error: unknown): RpcError {
  return new RpcError(
    Code.modelCallFailed,
    error instanceof Error ? error.message : 'the model call failed',
    { provider },
  )
}
