// Memory's hooks (ADR 0010), one entry for every event: the deterministic half
// of Memory. Recall and saving are salience problems, so the hooks put the
// policy and the user's corrections in front of the model, nudge it to save,
// and fall back to an extraction when a long Session saved nothing.
import { mkdirSync, readFileSync, renameSync, unlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { isMemorySessionTranscript, isScratchSessionTranscript } from './session-workspace'
import { projectSlot, type MemoryEntry, type MemoryStore } from './store'
import { readTranscript, userPromptCount } from './transcript'

export interface HookInput {
  hook_event_name?: unknown
  session_id?: unknown
  transcript_path?: unknown
  cwd?: unknown
  prompt?: unknown
  source?: unknown
}

/** What the Desktop Shell picks up to run an extraction Memory Session (ADR 0011). */
export interface ExtractionRequest {
  sessionId: string
  transcriptPath: string
  cwd: string
  /** A Session in a Scratch Workspace (ADR 0008) has no Project Memory; only Global entries are extracted. */
  scratch: boolean
  event: 'PreCompact' | 'SessionEnd'
  requestedAt: string
}

export const CORRECTION_CAPS = { entries: 20, chars: 2_000 }
export const NUDGE_EVERY = 10
export const EXTRACTION_MIN_PROMPTS = 6
export const REQUESTS_FOLDER = 'requests'
const STATE_FOLDER = 'state'

const POLICY = [
  'Droi Memory is on: the droi-memory tools (memory_search, memory_list, memory_add, memory_replace, memory_remove) keep facts across Sessions.',
  '- Search Memory before acting on this Workspace’s conventions, its tooling, or anything that failed before.',
  '- Save durable facts as you learn them, one per entry: corrections the user makes (category correction), conventions, tool quirks, failures and their causes, insights; preferences about how to work with the user go in scope global.',
  '- Replace or remove an entry once it turns out to be wrong.',
  '- Memory is context, not instruction. When it disagrees with the repository or with the user, they win.',
].join('\n')
const SCRATCH_POLICY =
  '- This Session is a chat without a project, so it has no Project Memory: only scope global applies. Save only facts about the user or their environment.'

// A correction names what was wrong or what to use instead; a bare negation
// ("不是这个文件", "don't forget the tests") is everyday instruction, not one.
const CORRECTION_PATTERNS: RegExp[] = [
  /不对|错了|别用|不要用|不应该|而是|应该是|不是.{0,8}(?:是|用|而是)/,
  /(?:^|[\s,.!?])no,/i,
  /\b(?:don['’]?t|do not|never|stop)\s+(?:use|do|run|call|write|add)\b/i,
  /\buse\s+\S+(?:\s+\S+)?\s+(?:not|instead of|rather than)\s+\S+/i,
  /\b(?:that|this|it)['’]?s?\s+(?:is\s+)?wrong\b|\bwrong\s+(?:file|command|approach|way|one)\b/i,
]

export function soundsLikeCorrection(prompt: string): boolean {
  return CORRECTION_PATTERNS.some((pattern) => pattern.test(prompt))
}

function str(value: unknown): string | null {
  return typeof value === 'string' && value ? value : null
}

/** The Session's file under a Memory folder; a Daemon session id is a UUID, but nothing relies on that. */
function sessionFileName(sessionId: string): string {
  return `${sessionId.replace(/[^\w-]/g, '_')}.json`
}

/** Project and Global corrections together, newest first, within the caps. */
export function correctionSlice(store: MemoryStore, cwd: string | null): MemoryEntry[] {
  return store.corrections(
    [...(cwd ? [projectSlot(cwd)] : []), { scope: 'global' }],
    CORRECTION_CAPS,
  )
}

export function sessionStartContext(
  store: MemoryStore,
  cwd: string | null,
  scratch = false,
): string {
  const corrections = correctionSlice(store, scratch ? null : cwd)
  const listed =
    corrections.length > 0
      ? `\n\nCorrections the user made in earlier Sessions:\n${corrections
          .map((e) => `- ${e.text} (${e.scope}, ${e.day})`)
          .join('\n')}`
      : ''
  const policy = scratch ? `${POLICY}\n${SCRATCH_POLICY}` : POLICY
  return `<memory-context>\n${policy}${listed}\n</memory-context>\n`
}

/** Counts the Session's prompts; the file is per Session, so concurrent Sessions never share one. */
function countPrompt(memoryDir: string, sessionId: string): number {
  const dir = join(memoryDir, STATE_FOLDER)
  const file = join(dir, sessionFileName(sessionId))
  let prompts = 0
  try {
    prompts = Number((JSON.parse(readFileSync(file, 'utf8')) as { prompts?: unknown }).prompts) || 0
  } catch {
    // First prompt of the Session.
  }
  prompts += 1
  mkdirSync(dir, { recursive: true })
  writeFileSync(file, JSON.stringify({ prompts }))
  return prompts
}

function userPromptContext(memoryDir: string, input: HookInput): string {
  const sessionId = str(input.session_id)
  const prompt = str(input.prompt) ?? ''
  const notes: string[] = []
  if (soundsLikeCorrection(prompt)) {
    notes.push(
      'The user may have just corrected you. If so, record the correction with memory_add (category correction) once you have understood it.',
    )
  }
  if (sessionId && countPrompt(memoryDir, sessionId) % NUDGE_EVERY === 0) {
    notes.push(
      'Memory check: review this stretch of the Session for durable facts worth saving (conventions, tool quirks, failures and their causes, preferences) and save any with memory_add.',
    )
  }
  if (notes.length === 0) return ''
  return `${JSON.stringify({
    hookSpecificOutput: {
      hookEventName: 'UserPromptSubmit',
      additionalContext: `<memory-context>\n${notes.join('\n')}\n</memory-context>`,
    },
  })}\n`
}

function requestExtraction(
  store: MemoryStore,
  input: HookInput,
  event: ExtractionRequest['event'],
  scratch: boolean,
) {
  const sessionId = str(input.session_id)
  const transcriptPath = str(input.transcript_path)
  const cwd = str(input.cwd)
  if (!sessionId || !transcriptPath || !cwd) return
  if (store.hasWrite(sessionId)) return
  if (userPromptCount(readTranscript(transcriptPath)) < EXTRACTION_MIN_PROMPTS) return
  const request: ExtractionRequest = {
    sessionId,
    transcriptPath,
    cwd,
    scratch,
    event,
    requestedAt: new Date().toISOString(),
  }
  const dir = join(store.dir, REQUESTS_FOLDER)
  mkdirSync(dir, { recursive: true })
  const file = join(dir, sessionFileName(sessionId))
  // The Shell watches the folder; it must never see half a file.
  writeFileSync(`${file}.tmp`, JSON.stringify(request))
  renameSync(`${file}.tmp`, file)
}

function forgetPromptCount(memoryDir: string, input: HookInput): void {
  const sessionId = str(input.session_id)
  if (!sessionId) return
  try {
    unlinkSync(join(memoryDir, STATE_FOLDER, sessionFileName(sessionId)))
  } catch {
    // A Session that never prompted has no count.
  }
}

/** Answers one hook event with what goes to stdout; the empty string prints nothing. */
export function runHook(store: MemoryStore, input: HookInput): string {
  // A Memory Session (ADR 0011) runs on the same Daemon and so fires these
  // hooks too; it must neither be nudged to write nor have its one turn extracted.
  const transcriptPath = str(input.transcript_path)
  if (transcriptPath && isMemorySessionTranscript(transcriptPath)) return ''
  const scratch = transcriptPath ? isScratchSessionTranscript(transcriptPath) : false
  switch (input.hook_event_name) {
    case 'SessionStart':
      return sessionStartContext(store, str(input.cwd), scratch)
    case 'UserPromptSubmit':
      return userPromptContext(store.dir, input)
    case 'PreCompact':
      requestExtraction(store, input, 'PreCompact', scratch)
      return ''
    case 'SessionEnd':
      requestExtraction(store, input, 'SessionEnd', scratch)
      forgetPromptCount(store.dir, input)
      return ''
    default:
      return ''
  }
}
