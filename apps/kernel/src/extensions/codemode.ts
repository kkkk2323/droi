import type { Context, JsonValue } from '@earendil-works/chord'
import { NestedCallDoc, ToolTask, hook } from '@earendil-works/pi-durable'
import type {
  Extension,
  HookApi,
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

/** A nested call as the card reads it: Pi's row, with what the call answered. */
export type NestedCallWithOutput = CodemodeNestedCall & { output?: string; truncated?: true }

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
 * codemode call settles, and its first {@link NESTED_OUTPUT_BYTES} bytes go into `details.calls[i].output`.
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
  if (details === undefined || !Array.isArray(details.calls) || details.calls.length === 0) {
    return undefined
  }
  const calls = await Promise.all(
    details.calls.map(async (row): Promise<NestedCallWithOutput> => {
      const nested = await api.snapshot(NestedCallDoc, api.taskId, `c${row.id}`, context)
      const answer = nested?.result as NestedToolExecutionResult | undefined
      if (answer === undefined) return row
      const text = outputText(answer)
      if (text === '') return row
      const cut = cutBytes(text, NESTED_OUTPUT_BYTES)
      return { ...row, output: cut, ...(cut.length < text.length ? { truncated: true } : {}) }
    }),
  )
  return { ...result, details: { ...details, calls } as unknown as JsonValue }
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
