// What the transcript shows for a tool call, whichever Client draws it: a
// one-line summary, the input, and the result read as a diff, a status or
// plain text, with any pictures it handed back.
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
 * The Daemon's Edit / Create results are JSON with `diffLines`; anything
 * else (plain text, errors, other tools) is shown as it came.
 */
export function parseDiffResult(text: string): DiffResult | null {
  if (!text.startsWith('{') || !text.includes('"diffLines"')) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const raw = (parsed as { diffLines?: unknown }).diffLines
  if (!Array.isArray(raw)) return null
  const lines: DiffLine[] = []
  for (const entry of raw) {
    if (typeof entry !== 'object' || entry === null) continue
    const e = entry as { type?: unknown; content?: unknown; lineNumber?: unknown }
    if (e.type !== 'unchanged' && e.type !== 'added' && e.type !== 'removed') continue
    const numbers = (e.lineNumber ?? {}) as { old?: unknown; new?: unknown }
    lines.push({
      type: e.type,
      content: typeof e.content === 'string' ? e.content : '',
      old: typeof numbers.old === 'number' ? numbers.old : null,
      new: typeof numbers.new === 'number' ? numbers.new : null,
    })
  }
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
 * Create answers with a bare status, so the file it wrote is read from the
 * call's own input and shown as all-added lines.
 */
export function createdFileDiff(call: ToolCall): DiffResult | null {
  const content = call.use.input['content']
  if (call.use.name !== 'Create' || typeof content !== 'string') return null
  const text = content.endsWith('\n') ? content.slice(0, -1) : content
  const lines: DiffLine[] = text
    .split('\n')
    .map((line, index) => ({ type: 'added', content: line, old: null, new: index + 1 }))
  return { lines, added: lines.length, removed: 0 }
}

export interface ToolName {
  /** The MCP Server the tool belongs to; null for the Daemon's own tools. */
  server: string | null
  tool: string
}

/** The Daemon names an MCP tool `<server>___<tool>`; the row shows the two apart. */
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
 * What a permission Prompt shows of the tool it asks about: the Daemon's
 * details (the full command, the file) first, else the tool's input.
 */
export function permissionDetail(details: unknown, input: Record<string, unknown>): string {
  const d = (details ?? {}) as Record<string, unknown>
  if (typeof d['fullCommand'] === 'string') return `$ ${d['fullCommand']}`
  if (typeof d['filePath'] === 'string') return d['filePath']
  if (typeof input['command'] === 'string') return `$ ${input['command']}`
  return JSON.stringify(input)
}

export interface ScriptPermissionCall {
  /** Where the call sits in the program, 1-based. */
  line: number
  tool: string
  detail: string
  /** The Daemon's impact level for this one call, when it gave one (`low`, `medium`, `high`). */
  impact: string | null
}

export interface ScriptPermission {
  /** The highest impact among the calls. */
  impact: string | null
  calls: ScriptPermissionCall[]
}

/**
 * A Script asks once for the calls it can see in its own source, each with
 * the details its direct call would have asked with. Calls whose input is
 * only known at run time are asked about one by one as they come.
 */
export function scriptPermission(details: unknown): ScriptPermission | null {
  const d = (details ?? {}) as Record<string, unknown>
  if (d['type'] !== 'script' || !Array.isArray(d['calls'])) return null
  const calls: ScriptPermissionCall[] = []
  for (const raw of d['calls'] as unknown[]) {
    if (typeof raw !== 'object' || raw === null) continue
    const entry = raw as Record<string, unknown>
    const confirmation = (entry['confirmation'] ?? {}) as Record<string, unknown>
    const input = (entry['toolInput'] ?? {}) as Record<string, unknown>
    calls.push({
      line: typeof entry['line'] === 'number' ? entry['line'] : 0,
      tool: toolDisplayName(String(entry['toolName'] ?? '')).tool,
      detail: permissionDetail(confirmation, input),
      impact: typeof confirmation['impactLevel'] === 'string' ? confirmation['impactLevel'] : null,
    })
  }
  return {
    impact: typeof d['impactLevel'] === 'string' ? d['impactLevel'] : null,
    calls,
  }
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
  const diff = parseDiffResult(result) ?? (isError ? null : createdFileDiff(call))
  return {
    text: result,
    images: toolResultImages(call.result),
    pending: call.result === null,
    isError,
    diff,
    status: diff ? null : parseStatusResult(result),
  }
}
