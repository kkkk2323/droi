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
          const call = { use: block, result: results.get(block.id) ?? null }
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
    if (blocks.length === 0 && message.role === 'assistant') continue
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
  return a.use === b.use && a.result === b.result
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
      .map((part: unknown) =>
        typeof part === 'object' && part !== null && 'text' in part
          ? String((part as { text: unknown }).text)
          : '',
      )
      .join('\n')
  }
  return ''
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

/** Assistant entries followed by a user turn, plus the last one once the Daemon rests. */
export function turnEndIds(entries: readonly TranscriptEntry[], running: boolean): Set<string> {
  const ids = new Set<string>()
  entries.forEach((entry, index) => {
    if (entry.role !== 'assistant') return
    const next = entries[index + 1]
    if (next ? next.role === 'user' : !running) ids.add(entry.id)
  })
  return ids
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
