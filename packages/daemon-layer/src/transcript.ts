// Turn the SDK's flat message list into what the transcript renders: user
// turns, assistant turns whose tool calls carry their results, and nothing
// for bare tool-result messages, which only exist to complete a tool call.
import type { ContentBlock, FactoryDroidMessage } from '@factory/droid-sdk'

export type ToolUseBlock = Extract<ContentBlock, { type: 'tool_use' }>
export type ToolResultBlock = Extract<ContentBlock, { type: 'tool_result' }>
export type TextBlock = Extract<ContentBlock, { type: 'text' }>
export type ThinkingBlock = Extract<ContentBlock, { type: 'thinking' }>

export interface ToolCall {
  use: ToolUseBlock
  result: ToolResultBlock | null
  /**
   * Set on a Script or WaitForScript call: the calls its run made while this
   * call was the latest one watching it (see script-runs.ts).
   */
  nested?: ToolCall[]
}

export type TranscriptBlock =
  | { kind: 'text'; id: string; text: string }
  | { kind: 'image'; id: string; src: string }
  | { kind: 'thinking'; id: string; text: string; durationMs: number | undefined }
  /** A run of tool calls with nothing said in between; rendered as one cluster. */
  | { kind: 'tools'; id: string; calls: ToolCall[] }
  /** A Task call: work handed to a subagent, shown on its own (see subagents.ts). */
  | { kind: 'subagent'; id: string; call: ToolCall }

/** The tool that starts a subagent. */
export const TASK_TOOL = 'Task'

/** The tool that runs a program calling other tools, and the one that waits on a run past its first minute. */
export const SCRIPT_TOOL = 'Script'
export const WAIT_FOR_SCRIPT_TOOL = 'WaitForScript'

/** The run a Script or WaitForScript call is about: the Script's own tool use id. */
export function scriptRunOf(use: ToolUseBlock): string | null {
  if (use.scriptExecution) return null
  if (use.name === SCRIPT_TOOL) return use.id
  const watched = use.input['toolCallId']
  if (use.name === WAIT_FOR_SCRIPT_TOOL && typeof watched === 'string') return watched
  return null
}

export interface TranscriptEntry {
  id: string
  role: 'user' | 'assistant'
  blocks: TranscriptBlock[]
  createdAt: number
  isError: boolean
}

export function buildTranscript(messages: readonly FactoryDroidMessage[]): TranscriptEntry[] {
  const results = new Map<string, ToolResultBlock>()
  for (const message of messages) {
    if (message.role !== 'tool') continue
    for (const block of message.content) {
      if (block.type === 'tool_result') results.set(block.toolUseId, block)
    }
  }

  // A call a Script made goes under the latest call watching its run, so the
  // calls a WaitForScript saw show where that wait sits in the turn.
  const watching = new Map<string, ToolCall>()
  const entries: TranscriptEntry[] = []
  for (const message of messages) {
    if (message.role !== 'user' && message.role !== 'assistant') continue
    if (message.isUserVisible === false) continue
    const blocks: TranscriptBlock[] = []
    message.content.forEach((block, index) => {
      const id = `${message.id}:${index}`
      switch (block.type) {
        case 'text':
          if (block.text) blocks.push({ kind: 'text', id, text: block.text })
          break
        case 'image':
          if (block.source.type === 'base64')
            blocks.push({ kind: 'image', id, src: dataUrl(block.source) })
          break
        case 'thinking':
          if (block.thinking)
            blocks.push({
              kind: 'thinking',
              id,
              text: block.thinking,
              durationMs: block.durationMs,
            })
          break
        case 'tool_use': {
          const call: ToolCall = { use: block, result: results.get(block.id) ?? null }
          const owner = block.scriptExecution
            ? watching.get(block.scriptExecution.outerToolUseId)
            : undefined
          if (owner) {
            owner.nested!.push(call)
            break
          }
          const run = scriptRunOf(block)
          if (run) {
            call.nested = []
            watching.set(run, call)
          }
          if (block.name === TASK_TOOL) {
            blocks.push({ kind: 'subagent', id, call })
            break
          }
          const last = blocks[blocks.length - 1]
          if (last?.kind === 'tools') last.calls.push(call)
          else blocks.push({ kind: 'tools', id, calls: [call] })
          break
        }
        default:
          break
      }
    })
    // A user message with nothing to show is a record, not a turn: the
    // Daemon persists each hook run as one (`hookEventName`, no content), and
    // a message that was only a system reminder loses its text on the way.
    if (blocks.length === 0) continue
    const previous = entries[entries.length - 1]
    // One turn arrives as several assistant messages (reasoning, tool calls,
    // text); shown as one entry so nothing splits it. Tool runs join up too.
    if (message.role === 'assistant' && previous?.role === 'assistant') {
      for (const block of blocks) {
        const last = previous.blocks[previous.blocks.length - 1]
        if (block.kind === 'tools' && last?.kind === 'tools') last.calls.push(...block.calls)
        else previous.blocks.push(block)
      }
      previous.createdAt = message.createdAt
      previous.isError = previous.isError || message.isError === true
      continue
    }
    entries.push({
      id: message.id,
      role: message.role,
      blocks,
      createdAt: message.createdAt,
      isError: message.isError === true,
    })
  }
  return entries
}

// The SDK hands out the same source object for an image on every read; the
// same string back keeps entries comparable without scanning megabytes.
const dataUrls = new WeakMap<object, string>()

function dataUrl(source: { mediaType: string; data: string }): string {
  let url = dataUrls.get(source)
  if (url === undefined) {
    url = `data:${source.mediaType};base64,${source.data}`
    dataUrls.set(source, url)
  }
  return url
}

function sameCalls(a: readonly ToolCall[], b: readonly ToolCall[]): boolean {
  return a.length === b.length && a.every((call, i) => sameCall(call, b[i]!))
}

function sameCall(a: ToolCall, b: ToolCall): boolean {
  if (a.use !== b.use || a.result !== b.result) return false
  if (!a.nested || !b.nested) return a.nested === b.nested
  return sameCalls(a.nested, b.nested)
}

function sameBlock(a: TranscriptBlock, b: TranscriptBlock): boolean {
  if (a.kind !== b.kind || a.id !== b.id) return false
  switch (a.kind) {
    case 'text':
      return a.text === (b as typeof a).text
    case 'image':
      return a.src === (b as typeof a).src
    case 'thinking':
      return a.text === (b as typeof a).text && a.durationMs === (b as typeof a).durationMs
    case 'tools':
      return sameCalls(a.calls, (b as typeof a).calls)
    case 'subagent':
      return sameCall(a.call, (b as typeof a).call)
  }
}

function sameEntry(a: TranscriptEntry, b: TranscriptEntry): boolean {
  return (
    a.role === b.role &&
    a.createdAt === b.createdAt &&
    a.isError === b.isError &&
    a.blocks.length === b.blocks.length &&
    a.blocks.every((block, i) => sameBlock(block, b.blocks[i]!))
  )
}

/**
 * A freshly built transcript that keeps the previous build's entry objects
 * wherever nothing changed, so rows can skip rendering. The SDK rebuilds every
 * message on each read; while a turn streams, only its last entry is new.
 */
export function reuseUnchanged(
  previous: readonly TranscriptEntry[],
  next: TranscriptEntry[],
): readonly TranscriptEntry[] {
  if (previous.length === 0) return next
  const byId = new Map(previous.map((entry) => [entry.id, entry]))
  let changed = previous.length !== next.length
  const entries = next.map((entry, i) => {
    const old = byId.get(entry.id)
    if (old && sameEntry(old, entry)) {
      if (previous[i] !== old) changed = true
      return old
    }
    changed = true
    return entry
  })
  return changed ? entries : previous
}

export function toolResultText(result: ToolResultBlock | null): string {
  if (!result) return ''
  const content: unknown = result.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .filter(
        (part: unknown): part is { text: unknown } =>
          typeof part === 'object' && part !== null && 'text' in part,
      )
      .map((part) => String(part.text))
      .join('\n')
  }
  return ''
}

/** The pictures a tool handed back (a Read of an image file, a screenshot), as data URLs. */
export function toolResultImages(result: ToolResultBlock | null): string[] {
  const content = result?.content
  if (!Array.isArray(content)) return []
  const images: string[] = []
  for (const part of content) {
    if (part.type === 'image' && part.source.type === 'base64') images.push(dataUrl(part.source))
  }
  return images
}

// Building a formatter is the slow part of toLocaleTimeString; a turn's
// timestamp renders every time its row mounts.
const TIME = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' })
const DAY = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' })

/** A turn's time, with the day in front when it was not today. */
export function formatTimestamp(ms: number, now = new Date()): string {
  const date = new Date(ms)
  const time = TIME.format(date)
  if (date.toDateString() === now.toDateString()) return time
  return `${DAY.format(date)} ${time}`
}

/** When a turn ended, and when the user's message that started it was sent. */
export interface TurnEnd {
  endedAt: number
  startedAt: number | null
}

/**
 * The assistant entries that close a turn, by id. A turn closes with a reply:
 * an assistant entry whose last block is text, followed by a user message.
 * One that stops on tool calls and is followed by a user message was steered
 * mid-turn, not closed; the turn goes on in the next entry. The last entry
 * closes once the Daemon rests, however it ends (cancelled, say).
 */
export function turnEnds(
  entries: readonly TranscriptEntry[],
  running: boolean,
): Map<string, TurnEnd> {
  const ends = new Map<string, TurnEnd>()
  let startedAt: number | null = null
  entries.forEach((entry, index) => {
    if (entry.role === 'user') {
      startedAt ??= entry.createdAt
      return
    }
    const next = entries[index + 1]
    const replied = entry.blocks[entry.blocks.length - 1]?.kind === 'text'
    const closes = next ? next.role === 'user' && replied : !running
    if (!closes) return
    ends.set(entry.id, { endedAt: entry.createdAt, startedAt })
    startedAt = null
  })
  return ends
}

/** `23:13`, or `23:13 · took 4m 12s` when the turn's start is known. */
export function formatTurnEnd(end: TurnEnd, now = new Date()): string {
  const time = formatTimestamp(end.endedAt, now)
  if (end.startedAt === null) return time
  const took = end.endedAt - end.startedAt
  return took >= 1_000 ? `${time} · took ${formatDuration(took)}` : time
}

/** `640 ms`, `12s`, `4m 12s`, `1h 03m`. */
export function formatDuration(ms: number): string {
  if (ms < 1_000) return `${Math.round(ms)} ms`
  const seconds = Math.round(ms / 1_000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`
  return `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m`
}

const WORKING_LABELS: Record<string, string> = {
  idle: '',
  thinking: 'Thinking',
  streaming_assistant_message: 'Responding',
  waiting_for_tool_confirmation: 'Waiting for your approval',
  executing_tool: 'Running a tool',
  compacting_conversation: 'Compacting',
}

/** What the activity row under the transcript says for a working state. */
export function workingLabel(workingState: string): string {
  return WORKING_LABELS[workingState] ?? workingState
}
