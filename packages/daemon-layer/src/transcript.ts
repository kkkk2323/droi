// Turn a Kernel conversation's entries into what the transcript renders (the Go window's
// pkg/transcript BuildView, for the phone): a user entry opens a turn, and the answers
// after it join it, since one answer spans several entries once a tool round hands the
// run to a new generation. A tool call carries its result, read from the tool-result entry
// that completes it; a codemode call carries the calls its script made. The answer being
// written (pi.live's generation) closes the transcript while the run lasts.
import type { Entry, LiveDoc, PiContent, PiMessage } from './kernel-wire'

export interface ToolUse {
  id: string
  name: string
  input: Record<string, unknown>
}

export interface ToolResult {
  /** The output as the row shows it: the harness's rendering of its diagnostics left out. */
  text: string
  images: string[]
  isError: boolean
  /** The run was stopped, or a restart cut the call off: it did not fail. */
  interrupted: boolean
  /** A rule or a person refused it before it ran. */
  blocked: boolean
  /** The harness's diagnostics: a truncation, where the full output is, an exit code. */
  notes: string
  /** The tool's own details: an edit's diff, a script's calls. */
  details: unknown
}

export interface ToolCall {
  use: ToolUse
  /** Null while the call runs. */
  result: ToolResult | null
  /** Set on a codemode call: the calls its script made, in order. */
  nested?: ToolCall[]
}

export type TranscriptBlock =
  | { kind: 'text'; id: string; text: string }
  | { kind: 'image'; id: string; src: string }
  | { kind: 'thinking'; id: string; text: string; durationMs: number | undefined }
  /** A run of tool calls with nothing said in between; rendered as one cluster. */
  | { kind: 'tools'; id: string; calls: ToolCall[] }

/** The Kernel's tool that runs a script calling the conversation's other tools. */
export const SCRIPT_TOOL = 'codemode'

export interface TranscriptEntry {
  id: string
  role: 'user' | 'assistant'
  blocks: TranscriptBlock[]
  createdAt: number
  isError: boolean
  /** A line the Kernel or the computer left in the transcript, which nothing answers. */
  note?: boolean
  /** The answer being written now: it changes until its entry is stored. */
  streaming?: boolean
}

const STOPPED = 'Stopped'
const NO_ANSWER = 'The model could not answer this turn.'
const INTERRUPTED = 'Interrupted'

/** The transcript of a conversation's entries and its pi.live. */
export function buildTranscript(
  entries: readonly Entry[],
  live?: LiveDoc | null,
): TranscriptEntry[] {
  const results = new Map<string, ToolResult>()
  const scripts = new Map<string, ToolCall[]>()
  for (const entry of entries) {
    if (entry.kind !== 'pi.tool-result') continue
    const diagnostics = diagnosticsOf(entry)
    for (const m of entry.model ?? []) {
      if (m.role !== 'toolResult' || !m.toolCallId) continue
      results.set(m.toolCallId, resultOf(m, diagnostics))
      if (m.toolName === SCRIPT_TOOL)
        scripts.set(m.toolCallId, codemodeNested(m.toolCallId, m.details))
    }
  }
  const liveScripts = liveNested(live)
  const scriptCalls = (id: string) => scripts.get(id) ?? liveScripts.get(id) ?? []

  const out: TranscriptEntry[] = []
  // Set by what moves the context on (a reset, a compaction) and by a note: the answer after
  // one opens its own row rather than joining the one before.
  let boundary = false
  for (const entry of entries) {
    switch (entry.kind) {
      case 'pi.user': {
        const blocks = blocksOf(entry, results, scriptCalls, false)
        if (blocks.length === 0) {
          // A handoff or a written submission: the model reads it and the transcript shows nothing.
          boundary = true
          continue
        }
        out.push({
          id: String(entry.id),
          role: 'user',
          blocks,
          createdAt: createdAt(entry),
          isError: false,
        })
        boundary = false
        break
      }
      case 'pi.assistant': {
        let blocks = blocksOf(entry, results, scriptCalls, true)
        const { failed, stopped } = answerEnd(entry.model ?? [])
        if (blocks.length === 0) {
          // A refused or cancelled request can end with nothing; the turn did happen.
          const said = emptyAnswerSentence(entry.model ?? [], failed, stopped)
          if (said === null) continue
          blocks = [{ kind: 'text', id: `${entry.id}:0:0`, text: said }]
        }
        const last = out[out.length - 1]
        if (last && last.role === 'assistant' && !last.note && !boundary) {
          joinAnswer(last, blocks, createdAt(entry), failed)
        } else {
          out.push({
            id: String(entry.id),
            role: 'assistant',
            blocks,
            createdAt: createdAt(entry),
            isError: failed,
          })
        }
        boundary = false
        break
      }
      case 'pi.reset':
      case 'pi.compaction':
        boundary = true
        break
      case 'droi.note': {
        const text = typeof entry.data?.['text'] === 'string' ? (entry.data['text'] as string) : ''
        if (!text) continue
        out.push({
          id: String(entry.id),
          role: 'assistant',
          note: true,
          blocks: [{ kind: 'text', id: `${entry.id}:0:0`, text }],
          createdAt: createdAt(entry),
          isError: false,
        })
        boundary = true
        break
      }
    }
  }
  const writing = streamingEntry(live, results, scriptCalls)
  if (writing) {
    const last = out[out.length - 1]
    if (last && last.role === 'assistant' && !last.note && !boundary) {
      // The row being written is the turn's own: a copy, so the stored row's object stays as it was.
      const joined: TranscriptEntry = { ...last, blocks: last.blocks.slice(), streaming: true }
      joinAnswer(joined, writing.blocks, writing.createdAt, false)
      out[out.length - 1] = joined
    } else out.push(writing)
  }
  const failure = runFailureEntry(live, out)
  if (failure) out.push(failure)
  return out
}

function blocksOf(
  entry: Entry,
  results: ReadonlyMap<string, ToolResult>,
  scriptCalls: (id: string) => ToolCall[],
  answer: boolean,
): TranscriptBlock[] {
  return blocksOfMessages(String(entry.id), entry.model ?? [], results, scriptCalls, answer)
}

function blocksOfMessages(
  id: string,
  messages: readonly PiMessage[],
  results: ReadonlyMap<string, ToolResult>,
  scriptCalls: (id: string) => ToolCall[],
  answer: boolean,
): TranscriptBlock[] {
  const blocks: TranscriptBlock[] = []
  const stopped = answer && answerEnd(messages).stopped
  messages.forEach((m, mi) => {
    contentOf(m).forEach((c, i) => {
      // The identity carries the message as well as the block: one entry may hold several.
      const blockId = `${id}:${mi}:${i}`
      if (c.type === 'text') {
        const text = (c as { text?: unknown }).text
        if (typeof text === 'string' && text) blocks.push({ kind: 'text', id: blockId, text })
        return
      }
      if (c.type === 'image') {
        const { data, mimeType } = c as { data?: unknown; mimeType?: unknown }
        if (typeof data === 'string' && typeof mimeType === 'string')
          blocks.push({ kind: 'image', id: blockId, src: dataUrl(mimeType, data) })
        return
      }
      if (!answer) return
      if (c.type === 'thinking') {
        const { thinking, durationMs } = c as { thinking?: unknown; durationMs?: unknown }
        if (typeof thinking === 'string' && thinking)
          blocks.push({
            kind: 'thinking',
            id: blockId,
            text: thinking,
            durationMs: typeof durationMs === 'number' ? durationMs : undefined,
          })
        return
      }
      if (c.type === 'toolCall') {
        const t = c as { id?: unknown; name?: unknown; arguments?: unknown }
        const callId = typeof t.id === 'string' ? t.id : blockId
        const name = typeof t.name === 'string' ? t.name : ''
        const call: ToolCall = {
          use: { id: callId, name, input: inputOf(t.arguments) },
          result: results.get(callId) ?? (stopped ? interrupted() : null),
        }
        if (name === SCRIPT_TOOL) call.nested = scriptCalls(callId)
        const last = blocks[blocks.length - 1]
        if (last?.kind === 'tools') last.calls.push(call)
        else blocks.push({ kind: 'tools', id: blockId, calls: [call] })
      }
    })
  })
  return blocks
}

function contentOf(m: PiMessage): PiContent[] {
  // A message whose sender had only text is stored with its content as a bare string.
  if (typeof m.content === 'string') return m.content ? [{ type: 'text', text: m.content }] : []
  return Array.isArray(m.content) ? m.content : []
}

function inputOf(args: unknown): Record<string, unknown> {
  return typeof args === 'object' && args !== null && !Array.isArray(args)
    ? (args as Record<string, unknown>)
    : {}
}

/** How an answer ended, read from the last message that says. */
export function answerEnd(messages: readonly PiMessage[]): { failed: boolean; stopped: boolean } {
  for (let i = messages.length - 1; i >= 0; i--) {
    switch (messages[i]!.stopReason) {
      case 'error':
        return { failed: true, stopped: false }
      case 'aborted':
        return { failed: false, stopped: true }
      case 'stop':
      case 'toolUse':
      case 'length':
        return { failed: false, stopped: false }
    }
    if (messages[i]!.errorMessage) return { failed: true, stopped: false }
  }
  return { failed: false, stopped: false }
}

function emptyAnswerSentence(
  messages: readonly PiMessage[],
  failed: boolean,
  stopped: boolean,
): string | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const said = messages[i]!.errorMessage
    if (said) return said
  }
  if (failed) return NO_ANSWER
  if (stopped) return STOPPED
  return null
}

function joinAnswer(
  target: TranscriptEntry,
  blocks: TranscriptBlock[],
  at: number,
  failed: boolean,
): void {
  for (const block of blocks) {
    const last = target.blocks[target.blocks.length - 1]
    if (block.kind === 'tools' && last?.kind === 'tools') {
      target.blocks[target.blocks.length - 1] = { ...last, calls: [...last.calls, ...block.calls] }
    } else target.blocks.push(block)
  }
  target.createdAt = at
  target.isError = target.isError || failed
}

function createdAt(entry: Entry): number {
  const messages = entry.model ?? []
  const last = messages[messages.length - 1]
  if (last && typeof last.timestamp === 'number') return last.timestamp
  const at = entry.data?.['at']
  return typeof at === 'number' ? at : 0
}

interface Diagnostic {
  severity?: string
  code?: string
  message?: string
}

function diagnosticsOf(entry: Entry): Diagnostic[] {
  const list = entry.data?.['diagnostics']
  return Array.isArray(list) ? (list as Diagnostic[]) : []
}

/** Pi's rendering of the diagnostics for the model, which closes a result's content. */
function renderedDiagnostics(list: readonly Diagnostic[]): string {
  if (list.length === 0) return ''
  return `<harness>\n${list.map((d) => `[${d.severity}] ${d.message}`).join('\n')}\n</harness>`
}

function resultOf(m: PiMessage, diagnostics: readonly Diagnostic[]): ToolResult {
  const output: string[] = []
  const images: string[] = []
  for (const c of contentOf(m)) {
    if (c.type === 'text') {
      const text = (c as { text?: unknown }).text
      if (typeof text === 'string' && text) output.push(text)
    } else if (c.type === 'image') {
      const { data, mimeType } = c as { data?: unknown; mimeType?: unknown }
      if (typeof data === 'string' && typeof mimeType === 'string')
        images.push(dataUrl(mimeType, data))
    }
  }
  // Pi renders the diagnostics as the content's last item exactly when there are some.
  if (output.length > 0 && output[output.length - 1] === renderedDiagnostics(diagnostics))
    output.pop()
  const ending = diagnostics[diagnostics.length - 1]?.code
  const interruptedEnd = ending === 'aborted' || ending === 'interrupted' || ending === 'abandoned'
  const blocked = ending === 'blocked'
  return {
    text: withoutTrailingNewlines(output.join('\n')),
    images,
    isError: !interruptedEnd && !blocked && m.isError === true,
    interrupted: interruptedEnd,
    blocked,
    notes: diagnostics
      .map((d) => d.message ?? '')
      .filter(Boolean)
      .join('\n'),
    details: m.details,
  }
}

function interrupted(): ToolResult {
  return {
    text: INTERRUPTED,
    images: [],
    isError: false,
    interrupted: true,
    blocked: false,
    notes: '',
    details: undefined,
  }
}

function withoutTrailingNewlines(text: string): string {
  return text.replace(/[\r\n]+$/, '')
}

/** The calls a finished script made, from its result's details (the Kernel's `details.calls`). */
function codemodeNested(parent: string, details: unknown): ToolCall[] {
  const calls = (details as { calls?: unknown } | undefined)?.calls
  if (!Array.isArray(calls)) return []
  return calls.map((raw) => {
    const c = raw as {
      id?: unknown
      name?: unknown
      args?: unknown
      arguments?: unknown
      status?: unknown
      error?: unknown
      output?: unknown
      truncated?: unknown
    }
    const text =
      typeof c.output === 'string' && c.output
        ? c.output
        : typeof c.error === 'string'
          ? c.error
          : ''
    const status = c.status
    const result: ToolResult | null =
      status === 'running'
        ? null
        : {
            text: withoutTrailingNewlines(text),
            images: [],
            isError: status === 'error',
            interrupted: status === 'cancelled',
            blocked: false,
            notes: c.truncated === true ? 'The output is cut here; the script read all of it.' : '',
            details: undefined,
          }
    return {
      use: {
        id: `${parent}/c${String(c.id)}`,
        name: typeof c.name === 'string' ? c.name : '',
        input: rowArguments(c.arguments, c.args),
      },
      result,
    }
  })
}

/** The calls running scripts are making now, by the script's call id. */
function liveNested(live: LiveDoc | null | undefined): Map<string, ToolCall[]> {
  const out = new Map<string, ToolCall[]>()
  for (const n of live?.nestedTools ?? []) {
    const result: ToolResult | null =
      n.status === 'done' || n.summary?.isError
        ? {
            text: withoutTrailingNewlines(n.output ?? n.summary?.error ?? ''),
            images: [],
            isError: n.summary?.isError === true,
            interrupted: false,
            blocked: false,
            notes: '',
            details: undefined,
          }
        : null
    const list = out.get(n.parentCallId) ?? []
    list.push({ use: { id: n.callId, name: n.name, input: inputOf(n.arguments) }, result })
    out.set(n.parentCallId, list)
  }
  return out
}

/** A call's arguments as a row shows them: the whole object, or Pi's preview of them (cut to 200 characters). */
function rowArguments(args: unknown, preview: unknown): Record<string, unknown> {
  if (typeof args === 'object' && args !== null && !Array.isArray(args))
    return args as Record<string, unknown>
  if (typeof preview === 'string') {
    try {
      return inputOf(JSON.parse(preview))
    } catch {
      return { arguments: preview }
    }
  }
  return {}
}

/** The answer pi.live is writing now, while its run lasts. */
function streamingEntry(
  live: LiveDoc | null | undefined,
  results: ReadonlyMap<string, ToolResult>,
  scriptCalls: (id: string) => ToolCall[],
): TranscriptEntry | null {
  const message = (live?.generation as { message?: PiMessage } | undefined)?.message
  if (!live?.run || !message) return null
  const blocks = blocksOfMessages('live', [message], results, scriptCalls, true)
  if (blocks.length === 0) return null
  return {
    id: `live:${live.run.taskId}`,
    role: 'assistant',
    blocks,
    createdAt: typeof message.timestamp === 'number' ? message.timestamp : Date.now(),
    isError: false,
    streaming: true,
  }
}

/** Why the last run ended without an answer, when it did and the transcript does not say so already. */
function runFailureEntry(
  live: LiveDoc | null | undefined,
  entries: readonly TranscriptEntry[],
): TranscriptEntry | null {
  const f = live?.runFailure
  if (!f || live?.run) return null
  if (entries[entries.length - 1]?.isError) return null
  let text: string
  switch (f.reason) {
    case 'aborted':
    case 'reset':
    case 'nothing_to_answer':
      return null
    case 'model_error':
      text = f.detail || NO_ANSWER
      break
    case 'no_model':
      text = 'No model is set for this session. Choose one under the composer.'
      break
    case 'turn_limit':
      text = `The run stopped after ${f.detail}, the most this session takes in one run.`
      break
    default:
      text = `The run stopped: ${f.detail || f.reason}`
  }
  const id = `failure:${f.taskId}`
  return {
    id,
    role: 'assistant',
    blocks: [{ kind: 'text', id: `${id}:0:0`, text }],
    createdAt: 0,
    isError: true,
  }
}

function dataUrl(mediaType: string, data: string): string {
  return `data:${mediaType};base64,${data}`
}

function sameResult(a: ToolResult | null, b: ToolResult | null): boolean {
  if (a === b) return true
  if (!a || !b) return false
  return (
    a.text === b.text &&
    a.isError === b.isError &&
    a.interrupted === b.interrupted &&
    a.blocked === b.blocked &&
    a.notes === b.notes &&
    a.images.length === b.images.length &&
    a.images.every((src, i) => src === b.images[i]) &&
    JSON.stringify(a.details) === JSON.stringify(b.details)
  )
}

function sameCalls(a: readonly ToolCall[], b: readonly ToolCall[]): boolean {
  return a.length === b.length && a.every((call, i) => sameCall(call, b[i]!))
}

function sameCall(a: ToolCall, b: ToolCall): boolean {
  if (a.use.id !== b.use.id || a.use.name !== b.use.name) return false
  if (JSON.stringify(a.use.input) !== JSON.stringify(b.use.input)) return false
  if (!sameResult(a.result, b.result)) return false
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
  }
}

function sameEntry(a: TranscriptEntry, b: TranscriptEntry): boolean {
  return (
    a.role === b.role &&
    a.createdAt === b.createdAt &&
    a.isError === b.isError &&
    a.note === b.note &&
    a.streaming === b.streaming &&
    a.blocks.length === b.blocks.length &&
    a.blocks.every((block, i) => sameBlock(block, b.blocks[i]!))
  )
}

/**
 * A freshly built transcript that keeps the previous build's entry objects
 * wherever nothing changed, so rows can skip rendering. While a turn streams,
 * only its last entry is new.
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

export function toolResultText(result: ToolResult | null): string {
  return result?.text ?? ''
}

/** The pictures a tool handed back (a read of an image file, a screenshot), as data URLs. */
export function toolResultImages(result: ToolResult | null): string[] {
  return result?.images ?? []
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
