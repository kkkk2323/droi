// What the transcript shows for a tool call, whichever Client draws it: a
// one-line summary, the input, and the result read as a diff, a status or
// plain text.
import { toolResultText, type ToolCall } from './transcript'

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

export function toolSummary(call: ToolCall): string {
  const input = call.use.input
  for (const key of ['summary', 'command', 'file_path', 'path', 'pattern', 'url', 'query']) {
    const value = input[key]
    if (typeof value === 'string' && value.trim()) return firstLine(value)
  }
  return ''
}

function firstLine(text: string): string {
  const line = text.split('\n')[0] ?? ''
  return line.length > 120 ? `${line.slice(0, 117)}…` : line
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

/** The result's text and how it reads, for the row and its detail. */
export function readToolResult(call: ToolCall) {
  const result = toolResultText(call.result)
  const isError = call.result?.isError === true
  const diff = parseDiffResult(result) ?? (isError ? null : createdFileDiff(call))
  return {
    text: result,
    pending: call.result === null,
    isError,
    diff,
    status: diff ? null : parseStatusResult(result),
  }
}
