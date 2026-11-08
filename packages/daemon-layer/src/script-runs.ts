// What a script run shows in the transcript. The Kernel's codemode tool runs a script
// that calls the conversation's other tools; buildTranscript hangs each call it made
// under it (transcript.ts). Its result is what the script said with text(), and the
// calls themselves are in its details.
import { SCRIPT_TOOL, toolResultImages, type ToolCall } from './transcript'

export type ScriptStatus = 'completed' | 'failed' | 'running' | 'stalled' | 'cancelled'

export interface ScriptRun {
  /** What the script said to the model. */
  output: string
  images: string[]
  /** Null while the call has no result. */
  status: ScriptStatus | null
  /** Why the run failed, or why the Kernel refused it. */
  error: string | null
  /** How many calls the script made. */
  stats: string | null
  /** Where the whole output is kept, when the harness spilled it. */
  logPath: string | null
  /** What the script returned; the Kernel folds it into the output. */
  value: string | null
}

export function isScriptLifecycle(call: ToolCall): boolean {
  return call.nested !== undefined
}

export function readScriptRun(call: ToolCall): ScriptRun {
  const result = call.result
  const run: ScriptRun = {
    output: '',
    images: toolResultImages(result),
    status: null,
    error: null,
    stats: null,
    logPath: null,
    value: null,
  }
  if (!result) return run
  const calls = call.nested?.length ?? 0
  if (calls > 0) run.stats = calls === 1 ? '1 call' : `${calls} calls`
  if (result.interrupted) {
    run.status = 'cancelled'
    run.output = result.text
  } else if (result.isError || result.blocked) {
    run.status = 'failed'
    run.error = result.text || result.notes || 'The script failed.'
  } else {
    run.status = 'completed'
    run.output = result.text
  }
  return run
}

export interface ScriptSource {
  script: string
  /** Long literals the program reads as `inputs.<name>`; the Kernel's scripts carry none. */
  inputs: Array<{ name: string; text: string }>
}

export function scriptSource(call: ToolCall): ScriptSource | null {
  if (call.use.name !== SCRIPT_TOOL) return null
  const script = call.use.input['code']
  if (typeof script !== 'string') return null
  return { script, inputs: [] }
}

/**
 * The tools a script calls, in the order they first appear: `read`, `bash`,
 * `memory___memory_search` (an MCP tool).
 */
export function scriptToolNames(script: string): string[] {
  const names: string[] = []
  for (const match of script.matchAll(/\btools\.([A-Za-z_$][\w$]*)\s*\(/g)) {
    const name = match[1]!
    if (!names.includes(name)) names.push(name)
  }
  return names
}

/** What a script row says after its name. */
export function scriptSummary(call: ToolCall): string {
  const source = scriptSource(call)
  return source ? scriptToolNames(source.script).join(' · ') : ''
}

/** The calls of a run of tool calls, with a script counted as the calls it made. */
export function leafCalls(calls: readonly ToolCall[]): ToolCall[] {
  return calls.flatMap((call) => (call.nested ? call.nested : [call]))
}
