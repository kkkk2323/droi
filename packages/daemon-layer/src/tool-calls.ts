// What the transcript shows for a tool call, whichever Client draws it: a
// one-line summary, the input, and the result read as a diff, a status or
// plain text, with any pictures it handed back. The names are the Kernel's
// tools': bash, read, edit, write, codemode, and an MCP server's as
// `<server>___<tool>`.
import { leafCalls } from './script-runs'
import { toolResultImages, toolResultText, type ToolCall } from './transcript'

export interface DiffLine {
  type: 'unchanged' | 'added' | 'removed'
  content: string
  /** Line numbers in the old and new file; a removed line has no new one. */
  old: number | null
  new: number | null
}

export interface DiffResult {
  lines: DiffLine[]
  added: number
  removed: number
}

/**
 * An edit's diff, as Pi's edit tool keeps it in the result's details: one line per row,
 * `+12 text` added, `-12 text` removed, ` 12 text` kept, and `    ...` where unchanged
 * lines are left out.
 */
export function parseEditDiff(details: unknown): DiffResult | null {
  const diff = (details as { diff?: unknown } | null | undefined)?.diff
  if (typeof diff !== 'string' || !diff) return null
  const lines: DiffLine[] = []
  for (const raw of diff.split('\n')) {
    const sign = raw[0]
    const match = /^[+\- ]\s*(\d+) ?(.*)$/.exec(raw)
    if (!match) {
      if (raw.trim() === '...')
        lines.push({ type: 'unchanged', content: '…', old: null, new: null })
      continue
    }
    const n = Number(match[1])
    const content = match[2] ?? ''
    if (sign === '+') lines.push({ type: 'added', content, old: null, new: n })
    else if (sign === '-') lines.push({ type: 'removed', content, old: n, new: null })
    else lines.push({ type: 'unchanged', content, old: n, new: null })
  }
  if (lines.length === 0) return null
  return {
    lines,
    added: lines.filter((l) => l.type === 'added').length,
    removed: lines.filter((l) => l.type === 'removed').length,
  }
}

export interface StatusResult {
  success: boolean
  /** The Daemon's own words when it gave any (`message` or `error`). */
  message: string | null
}

/**
 * Create and friends answer with a small JSON object such as
 * `{"success":true,"file_path":"..."}`. Its path is already on the row, so
 * the result reads as a status line rather than as JSON. Anything with more
 * to say is shown as it came.
 */
export function parseStatusResult(text: string): StatusResult | null {
  if (!text.startsWith('{') || !text.includes('"success"')) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const object = parsed as Record<string, unknown>
  if (typeof object['success'] !== 'boolean') return null
  const message = [object['message'], object['error']].find(
    (v): v is string => typeof v === 'string' && v.trim() !== '',
  )
  const known = new Set(['success', 'file_path', 'path', 'message', 'error'])
  if (Object.keys(object).some((key) => !known.has(key))) return null
  return { success: object['success'], message: message ?? null }
}

/**
 * A write answers with a sentence, so the file it wrote is read from the
 * call's own input and shown as all-added lines.
 */
export function createdFileDiff(call: ToolCall): DiffResult | null {
  const content = call.use.input['content']
  if (call.use.name !== 'write' || typeof content !== 'string') return null
  const text = content.endsWith('\n') ? content.slice(0, -1) : content
  const lines: DiffLine[] = text
    .split('\n')
    .map((line, index) => ({ type: 'added', content: line, old: null, new: index + 1 }))
  return { lines, added: lines.length, removed: 0 }
}

export interface ToolName {
  /** The MCP Server the tool belongs to; null for the Kernel's own tools. */
  server: string | null
  tool: string
}

/** The Kernel names an MCP tool `<server>___<tool>`; the row shows the two apart. */
export function toolDisplayName(name: string): ToolName {
  const at = name.indexOf('___')
  if (at <= 0 || at + 3 >= name.length) return { server: null, tool: name }
  return { server: name.slice(0, at), tool: name.slice(at + 3) }
}

/** One piece of a row's summary: a bare value for a well-known input, else `key: value`. */
export interface SummaryPart {
  key: string | null
  value: string
}

// One input that says what the call is about, shown on its own without its name.
const HEADLINE_KEYS = [
  'summary',
  'command',
  'file_path',
  'path',
  'pattern',
  'url',
  'query',
  'skill',
]
const FALLBACK_PARTS = 3
const FALLBACK_VALUE_LENGTH = 60

/**
 * What the row says after the tool's name. A headline input (the command,
 * the file, the search) stands alone; any other tool, MCP tools above all,
 * shows its first few short inputs as `key: value` pairs so a call to
 * `memory_list` reads `scope: project` rather than nothing.
 */
export function toolSummaryParts(call: ToolCall): SummaryPart[] {
  const input = call.use.input
  for (const key of HEADLINE_KEYS) {
    const value = input[key]
    if (typeof value === 'string' && value.trim()) return [{ key: null, value: firstLine(value) }]
  }
  const parts: SummaryPart[] = []
  for (const [key, value] of Object.entries(input)) {
    if (parts.length === FALLBACK_PARTS) break
    if (typeof value === 'string') {
      if (value.trim()) parts.push({ key, value: firstLine(value, FALLBACK_VALUE_LENGTH) })
    } else if (typeof value === 'number' || typeof value === 'boolean') {
      parts.push({ key, value: String(value) })
    }
  }
  return parts
}

export function toolSummary(call: ToolCall): string {
  return toolSummaryParts(call)
    .map((part) => (part.key ? `${part.key}: ${part.value}` : part.value))
    .join(' · ')
}

function firstLine(text: string, max = 120): string {
  const line = text.split('\n')[0] ?? ''
  return line.length > max ? `${line.slice(0, max - 3)}…` : line
}

export function truncateLines(text: string, max: number): string {
  const lines = text.split('\n')
  if (lines.length <= max) return text
  return `${lines.slice(0, max).join('\n')}\n… ${lines.length - max} more lines`
}

/** The call's input as the row's detail shows it: the command, the path, or the raw input. */
export function toolInputText(call: ToolCall): string {
  const input = call.use.input
  const command = typeof input['command'] === 'string' ? input['command'] : null
  if (command) return `$ ${command}`
  const path =
    typeof input['file_path'] === 'string'
      ? input['file_path']
      : typeof input['path'] === 'string'
        ? input['path']
        : null
  return path ?? JSON.stringify(input, null, 2)
}

/**
 * What an approval card shows of the call it asks about: the command, the file, or the
 * script, else the arguments.
 */
export function approvalDetail(tool: string, args: Record<string, unknown>): string {
  const command = args['command']
  if (
    (tool === 'bash' || tool === 'powershell' || tool === 'bash_background') &&
    typeof command === 'string'
  ) {
    return `$ ${command}`
  }
  const code = args['code']
  if (tool === 'codemode' && typeof code === 'string') return code
  const path = args['path']
  if (typeof path === 'string') return path
  return JSON.stringify(args, null, 2)
}

/**
 * What a tool cluster's header says: the one tool's name or how many ran,
 * with a Script counted as the calls it made. Pending while any call, or any
 * call a Script made, is still to answer.
 */
export function clusterLabel(calls: readonly ToolCall[]): { label: string; pending: boolean } {
  const leaves = leafCalls(calls)
  const pending = calls.some((c) => c.result === null) || leaves.some((c) => c.result === null)
  const only = leaves.length === 1 ? leaves[0] : leaves.length === 0 ? calls[0] : undefined
  const what = only
    ? toolDisplayName(only.use.name).tool
    : leaves.length === 0
      ? 'a tool'
      : `${leaves.length} tools`
  return { label: pending ? `Running ${what}` : `Used ${what}`, pending }
}

/** The result's text and how it reads, for the row and its detail. */
export function readToolResult(call: ToolCall) {
  const result = toolResultText(call.result)
  const isError = call.result?.isError === true
  const done = call.result !== null && !isError && !call.result.interrupted && !call.result.blocked
  const diff = done ? (parseEditDiff(call.result?.details) ?? createdFileDiff(call)) : null
  return {
    text: result,
    images: toolResultImages(call.result),
    pending: call.result === null,
    isError,
    /** Stopped before it finished: not a failure. */
    interrupted: call.result?.interrupted === true,
    /** Refused before it ran. */
    blocked: call.result?.blocked === true,
    /** The harness's notes: a truncation, the full output's path, an exit code. */
    notes: call.result?.notes ?? '',
    diff,
    status: diff ? null : parseStatusResult(result),
  }
}
