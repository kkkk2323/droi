import type { Context, JsonValue } from '@earendil-works/chord'
import { LiveDoc, NestedCallDoc, ToolTask, hook } from '@earendil-works/pi-durable'
import type {
  Extension,
  HookApi,
  NestedToolSlot,
  NestedToolExecutionResult,
  ToolExecutionResult,
  ToolHookCall,
} from '@earendil-works/pi-durable'
import {
  CODEMODE_TOOL_NAME,
  createCodemodeExtension,
  type CodemodeNamespace,
  type CodemodeNestedCall,
  type CodemodeToolDetails,
} from '@earendil-works/pi-durable/codemode'
import type { CodemodeSandboxBase } from '@earendil-works/pi-codemode'

/** The most of one nested call's output a script's card carries. */
export const NESTED_OUTPUT_BYTES = 4096

/**
 * A nested call as the card reads it: Pi's row, the arguments it ran with (Pi's row keeps only the first 200
 * characters of them), and what the call answered.
 */
export type NestedCallWithOutput = CodemodeNestedCall & {
  arguments?: JsonValue
  output?: string
  truncated?: true
}

/** Pi's row keeps this much of a call's arguments as JSON (pi-durable codemode's ARGS_PREVIEW_CHARS). */
const ARGS_PREVIEW_CHARS = 200

/** Pi's prefix of the tools behind `models.*` (pi-durable codemode's CODEMODE_INTERNAL_PREFIX). */
const INTERNAL_PREFIX = 'pi.codemode.'

/**
 * Codemode (design/m3-kernel.md §6): the `codemode` tool, which runs a model-written script that calls the
 * conversation's tools, on by default for every conversation. `conversation.configure {extensions: {remove:
 * ['codemode']}}` turns it off for one.
 *
 * `namespaces` is the MCP servers' list, which `McpServers` keeps current: Pi reads it each time it renders the
 * section or starts a script, so a server that connects later is grouped as its own.
 *
 * Pi's result lists each call the script made with its status, but not what it answered; the card shows both. So
 * before the result is committed, each call's answer is read from its `NestedCallDoc`, which lives until the
 * codemode call settles, and its first {@link NESTED_OUTPUT_BYTES} bytes go into `details.calls[i].output`; the
 * arguments each call ran with go into `details.calls[i].arguments`. A script the user stops ends with a result the
 * Harness writes, which lists no calls: its rows are rebuilt from the calls' `pi.live` slots, so the card keeps them.
 */
export function codemodeExtension(options: {
  sandbox: CodemodeSandboxBase
  namespaces: readonly CodemodeNamespace[]
}): Extension {
  const base = createCodemodeExtension({
    sandbox: options.sandbox,
    namespaces: options.namespaces,
    models: false,
  })
  return {
    ...base,
    hooks: [...(base.hooks ?? []), hook(ToolTask, { afterTool: withNestedOutput })],
  }
}

async function withNestedOutput(
  call: ToolHookCall,
  result: ToolExecutionResult,
  api: HookApi,
  context: Context,
): Promise<ToolExecutionResult | undefined> {
  if (call.name !== CODEMODE_TOOL_NAME || call.parent !== undefined) return undefined
  const details = result.details as CodemodeToolDetails | undefined
  // The script's slots are still in `pi.live` here: they go when the call settles, after this hook.
  const slots = new Map<number, NestedToolSlot>()
  const live = await api.snapshot(LiveDoc, api.conversationId, context)
  const prefix = `${call.id}/c`
  for (const slot of live?.nestedTools ?? []) {
    if (slot.parentCallId !== call.id || !slot.callId.startsWith(prefix)) continue
    const id = Number(slot.callId.slice(prefix.length))
    if (Number.isSafeInteger(id)) slots.set(id, slot)
  }
  // A stopped script's result is the Harness's own and lists no calls; the slots still say what it made.
  const stopped = !Array.isArray(details?.calls)
  const rows: CodemodeNestedCall[] = stopped
    ? [...slots.entries()].sort(([a], [b]) => a - b).map(([id, slot]) => rowOf(id, slot))
    : details.calls
  if (rows.length === 0) return undefined
  const calls = await Promise.all(
    rows.map(async (row): Promise<NestedCallWithOutput> => {
      const slot = slots.get(row.id)
      let withArguments: NestedCallWithOutput =
        slot === undefined ? row : { ...row, arguments: cutStrings(slot.arguments) }
      const nested = await api.snapshot(NestedCallDoc, api.taskId, `c${row.id}`, context)
      const answer = nested?.result as NestedToolExecutionResult | undefined
      if (answer === undefined) return withArguments
      // The stop reached the call too, which ended with its own error: it was cut off, as Pi calls a script's call
      // still running when the script ends.
      if (stopped && answer.diagnostics.some((diagnostic) => diagnostic.code === 'aborted')) {
        const { error: _error, ...rest } = withArguments
        withArguments = { ...rest, status: 'cancelled' }
      }
      const text = outputText(answer)
      if (text === '') return withArguments
      const cut = cutBytes(text, NESTED_OUTPUT_BYTES)
      return {
        ...withArguments,
        output: cut,
        ...(cut.length < text.length ? { truncated: true } : {}),
      }
    }),
  )
  return { ...result, details: { ...details, calls } as unknown as JsonValue }
}

/** Pi's row for a call, from its slot, as Pi writes it for a script that ends: one still running was cancelled. */
function rowOf(id: number, slot: NestedToolSlot): CodemodeNestedCall {
  const args = JSON.stringify(slot.arguments)
  const summary = slot.summary
  const status = slot.status !== 'done' ? 'cancelled' : summary?.isError === true ? 'error' : 'ok'
  return {
    id,
    name: slot.name.startsWith(INTERNAL_PREFIX)
      ? slot.name.slice(INTERNAL_PREFIX.length)
      : slot.name,
    args: args.length > ARGS_PREVIEW_CHARS ? `${args.slice(0, ARGS_PREVIEW_CHARS - 3)}...` : args,
    status,
    ...(summary?.durationMs === undefined ? {} : { durationMs: summary.durationMs }),
    ...(status === 'error' && summary?.error ? { error: summary.error } : {}),
  } as CodemodeNestedCall
}

/** `value` with each string in it cut to {@link NESTED_OUTPUT_BYTES}: a file a call wrote is not the card's to keep. */
function cutStrings(value: JsonValue): JsonValue {
  if (typeof value === 'string') return cutBytes(value, NESTED_OUTPUT_BYTES)
  if (Array.isArray(value)) return value.map(cutStrings)
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, cutStrings(item as JsonValue)]),
    )
  }
  return value
}
/** What a nested call answered, as text: its output for programs, or, when it has none, why it failed. */
function outputText(result: NestedToolExecutionResult): string {
  const value = result.structuredOutput
  if (value !== undefined) return valueText(value)
  return result.diagnostics
    .map((diagnostic) => diagnostic.message)
    .filter((message) => message !== '')
    .join('\n')
}

function valueText(value: JsonValue): string {
  if (typeof value === 'string') return value
  if (Array.isArray(value) && value.every(isContent)) {
    return (value as unknown as { type: string; text?: unknown }[])
      .map((item) => (item.type === 'text' ? String(item.text ?? '') : '[image]'))
      .join('\n')
  }
  if (isContent(value) && value.type === 'image') return '[image]'
  return JSON.stringify(value, null, 2)
}

function isContent(value: unknown): value is { type: string; text?: unknown } {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const type = (value as { type?: unknown }).type
  return type === 'text' || type === 'image'
}

/** `text` cut to at most `limit` UTF-8 bytes, never inside a character. */
function cutBytes(text: string, limit: number): string {
  if (Buffer.byteLength(text, 'utf8') <= limit) return text
  return Buffer.from(text, 'utf8')
    .subarray(0, limit)
    .toString('utf8')
    .replace(/\uFFFD+$/, '')
}
