// What a Script run shows in the transcript. The Daemon's Script tool runs a
// program that calls other tools; each of those calls arrives as its own
// tool call carrying `scriptExecution`, and buildTranscript hangs it under the
// Script (or the WaitForScript that was watching the run when it was made).
// The Script's own result is what the model read: the program's text()
// output, a closing line with the run's figures, and a status as JSON.
import {
  SCRIPT_TOOL,
  WAIT_FOR_SCRIPT_TOOL,
  toolResultImages,
  type ToolCall,
  type ToolResultBlock,
} from './transcript'

export type ScriptStatus = 'completed' | 'failed' | 'running' | 'stalled' | 'cancelled'

const STATUSES: readonly string[] = ['completed', 'failed', 'running', 'stalled', 'cancelled']

export interface ScriptRun {
  /** What the program said to the model with text(), as it was cut for the model. */
  output: string
  images: string[]
  /** Null while the call has no result, or when the result is not a run's (a refused call). */
  status: ScriptStatus | null
  /** Why the run failed, or what the Daemon said when it refused the call. */
  error: string | null
  /** The closing line's figures: `3 calls · 86 B in sandbox · 33 B emitted (38%)`. */
  stats: string | null
  /** The log file holding every nested result whole. */
  logPath: string | null
  /** What the program returned, when it returned anything. */
  value: string | null
}

export function isScriptLifecycle(call: ToolCall): boolean {
  return call.nested !== undefined
}

const CLOSING_LINE = /^\[Script (?:completed|failed)[^\n]*\]$/

export function readScriptRun(call: ToolCall): ScriptRun {
  const run: ScriptRun = {
    output: '',
    images: toolResultImages(call.result),
    status: null,
    error: null,
    stats: null,
    logPath: null,
    value: null,
  }
  const parts = textParts(call.result)
  if (parts.length === 0) return run

  const envelope = parseEnvelope(parts[parts.length - 1]!)
  if (!envelope) {
    // Not a run's answer: the Daemon refused the call before it started.
    if (call.result?.isError === true) {
      run.status = 'failed'
      run.error = parts.join('\n')
    } else run.output = parts.join('\n')
    return run
  }
  parts.pop()
  run.status = envelope.status
  if (typeof envelope.error === 'string') run.error = envelope.error
  if (envelope.result !== undefined && envelope.result !== null) {
    run.value =
      typeof envelope.result === 'string'
        ? envelope.result
        : JSON.stringify(envelope.result, null, 2)
  } else if (typeof envelope.resultPath === 'string') {
    run.value = `Saved to ${envelope.resultPath}`
  }

  const closing = parts.findIndex((part) => CLOSING_LINE.test(part.trim()))
  if (closing >= 0) {
    const figures = readClosingLine(parts[closing]!.trim())
    run.stats = figures.stats
    run.logPath = figures.logPath
    parts.splice(closing, 1)
  }
  run.output = parts.join('\n')
  return run
}

interface Envelope {
  status: ScriptStatus
  error?: unknown
  result?: unknown
  resultPath?: unknown
}

function parseEnvelope(text: string): Envelope | null {
  const trimmed = text.trim()
  if (!trimmed.startsWith('{') || !trimmed.includes('"toolCallId"')) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const status = (parsed as { status?: unknown }).status
  if (typeof status !== 'string' || !STATUSES.includes(status)) return null
  return parsed as Envelope
}

// `[Script completed · 3 calls · 86 B in sandbox · 33 B emitted (38%) · retained: r1 …, r2 … · log: /path]`
function readClosingLine(line: string): { stats: string | null; logPath: string | null } {
  const fields = line.slice(1, -1).split(' · ')
  fields.shift()
  let logPath: string | null = null
  const kept: string[] = []
  for (const field of fields) {
    if (field.startsWith('log: ')) logPath = field.slice(5)
    // The handles name results for the model to look up again; they mean nothing on screen.
    else if (!field.startsWith('retained: ')) kept.push(field)
  }
  return { stats: kept.length > 0 ? kept.join(' · ') : null, logPath }
}

function textParts(result: ToolResultBlock | null): string[] {
  const content: unknown = result?.content
  if (typeof content === 'string') return content ? [content] : []
  if (!Array.isArray(content)) return []
  return content
    .filter(
      (part: unknown): part is { type: 'text'; text: string } =>
        typeof part === 'object' &&
        part !== null &&
        (part as { type?: unknown }).type === 'text' &&
        typeof (part as { text?: unknown }).text === 'string',
    )
    .map((part) => part.text)
}

export interface ScriptSource {
  script: string
  /** Long literals the program reads as `inputs.<name>`. */
  inputs: Array<{ name: string; text: string }>
}

export function scriptSource(call: ToolCall): ScriptSource | null {
  if (call.use.name !== SCRIPT_TOOL) return null
  const script = call.use.input['script']
  if (typeof script !== 'string') return null
  const raw = call.use.input['inputs']
  const inputs =
    typeof raw === 'object' && raw !== null
      ? Object.entries(raw as Record<string, unknown>)
          .filter((entry): entry is [string, string] => typeof entry[1] === 'string')
          .map(([name, text]) => ({ name, text }))
      : []
  return { script, inputs }
}

/**
 * The tools a program calls, in the order they first appear: `Read`,
 * `Execute`, `memory_search` (an MCP tool, `tools.droi_memory.memory_search`).
 */
export function scriptToolNames(script: string): string[] {
  const names: string[] = []
  for (const match of script.matchAll(
    /\btools\.([A-Za-z_$][\w$]*)(?:\.([A-Za-z_$][\w$]*))?\s*\(/g,
  )) {
    const name = match[2] ?? match[1]!
    if (!names.includes(name)) names.push(name)
  }
  return names
}

/** What a Script or WaitForScript row says after its name. */
export function scriptSummary(call: ToolCall): string {
  if (call.use.name === WAIT_FOR_SCRIPT_TOOL) {
    return call.use.input['kill'] === true ? 'stopped' : 'continued'
  }
  const source = scriptSource(call)
  return source ? scriptToolNames(source.script).join(' · ') : ''
}

/** The calls of a run of tool calls, with a Script counted as the calls it made. */
export function leafCalls(calls: readonly ToolCall[]): ToolCall[] {
  return calls.flatMap((call) => (call.nested ? call.nested : [call]))
}
