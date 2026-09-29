// What the Desktop Shell asks of Memory Sessions (ADR 0011): consolidating a
// Memory that grew large, one category slice at a time, and extracting entries
// from a finished Session that saved none. The model's answer is validated
// here; a wrong answer is a rejected slice or a skipped entry, never lost data.
import { existsSync } from 'node:fs'
import { homedir } from 'node:os'
import { basename } from 'node:path'
import { exportMarkdown } from '../../memory/markdown'
import {
  CATEGORIES,
  isCategory,
  projectSlot,
  type Category,
  type MemoryEntry,
  type MemorySlot,
  type MemoryStore,
  type SliceChanges,
} from '../../memory/store'
import { readTranscript, type TranscriptTurn } from '../../memory/transcript'
import type { ExtractionRequest } from '../../memory/hook'
import type { RunMemorySession } from './memory-session'

/** A slice the model sees at once; a larger category is split. */
export const SLICE_CHARS = 40_000
/** The transcript an extraction sends, from the end: the latest turns matter most. */
export const TRANSCRIPT_CHARS = 60_000

export interface MemoryWorkOptions {
  store: MemoryStore
  run: RunMemorySession
  modelId: string
  prompt: string
}

export interface SliceOutcome {
  category: Category
  entries: number
  ok: boolean
  reason: string | null
}

const stringArray = { type: 'array', items: { type: 'string' } }

export const CONSOLIDATION_SCHEMA = {
  type: 'object',
  properties: {
    keep: stringArray,
    rewrite: {
      type: 'array',
      items: {
        type: 'object',
        properties: { id: { type: 'string' }, text: { type: 'string' } },
        required: ['id', 'text'],
        additionalProperties: false,
      },
    },
    remove: stringArray,
    merge: {
      type: 'array',
      items: {
        type: 'object',
        properties: { ids: stringArray, text: { type: 'string' } },
        required: ['ids', 'text'],
        additionalProperties: false,
      },
    },
  },
  required: ['keep', 'rewrite', 'remove', 'merge'],
  additionalProperties: false,
}

export const EXTRACTION_SCHEMA = {
  type: 'object',
  properties: {
    entries: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          scope: { type: 'string', enum: ['project', 'global'] },
          category: { type: 'string', enum: [...CATEGORIES] },
          text: { type: 'string' },
        },
        required: ['scope', 'category', 'text'],
        additionalProperties: false,
      },
    },
  },
  required: ['entries'],
  additionalProperties: false,
}

const isStrings = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((v) => typeof v === 'string')

export function parseSliceChanges(value: unknown): SliceChanges | null {
  if (typeof value !== 'object' || value === null) return null
  const v = value as Record<string, unknown>
  const rewrite = v['rewrite']
  const merge = v['merge']
  if (!isStrings(v['keep']) || !isStrings(v['remove'])) return null
  if (!Array.isArray(rewrite) || !Array.isArray(merge)) return null
  if (!rewrite.every((r) => typeof r?.id === 'string' && typeof r?.text === 'string')) return null
  if (!merge.every((m) => isStrings(m?.ids) && typeof m?.text === 'string')) return null
  return {
    keep: v['keep'],
    remove: v['remove'],
    rewrite: rewrite.map((r: { id: string; text: string }) => ({ id: r.id, text: r.text })),
    merge: merge.map((m: { ids: string[]; text: string }) => ({ ids: m.ids, text: m.text })),
  }
}

/** Where a Memory Session for this Memory runs: its Workspace while it exists. */
function sessionCwd(slot: MemorySlot): string {
  return slot.scope === 'project' && existsSync(slot.workspace) ? slot.workspace : homedir()
}

function memoryName(slot: MemorySlot): string {
  return slot.scope === 'global' ? 'Global Memory' : basename(slot.workspace)
}

export function slicesOf(entries: readonly MemoryEntry[]): MemoryEntry[][] {
  const slices: MemoryEntry[][] = []
  let current: MemoryEntry[] = []
  let chars = 0
  for (const entry of entries) {
    if (current.length > 0 && chars + entry.text.length > SLICE_CHARS) {
      slices.push(current)
      current = []
      chars = 0
    }
    current.push(entry)
    chars += entry.text.length
  }
  if (current.length > 0) slices.push(current)
  return slices
}

/** Consolidates every category of one Memory; each slice stands or falls alone. */
export async function consolidate(
  options: MemoryWorkOptions,
  slot: MemorySlot,
): Promise<SliceOutcome[]> {
  const outcomes: SliceOutcome[] = []
  for (const category of CATEGORIES) {
    for (const slice of slicesOf(options.store.list(slot, category))) {
      if (slice.length < 2) continue
      const outcome = { category, entries: slice.length }
      try {
        const reply = await options.run({
          title: `Memory: consolidate ${memoryName(slot)} / ${category}`,
          cwd: sessionCwd(slot),
          modelId: options.modelId,
          prompt: options.prompt,
          input: JSON.stringify({
            memory: slot.scope === 'global' ? 'global' : slot.workspace,
            category,
            entries: slice.map(({ id, day, text }) => ({ id, day, text })),
          }),
          schema: CONSOLIDATION_SCHEMA,
        })
        const changes = parseSliceChanges(reply)
        const applied = changes
          ? options.store.applySlice(slice, changes)
          : { ok: false as const, reason: 'The reply did not match the schema.' }
        outcomes.push({ ...outcome, ok: applied.ok, reason: applied.ok ? null : applied.reason })
      } catch (error) {
        outcomes.push({
          ...outcome,
          ok: false,
          reason: error instanceof Error ? error.message : String(error),
        })
      }
    }
  }
  options.store.markConsolidated(slot)
  exportMarkdown(options.store, slot)
  return outcomes
}

export function transcriptText(turns: readonly TranscriptTurn[]): string {
  const text = turns
    .map((t) => `${t.role === 'user' ? 'User' : 'Assistant'}: ${t.text}`)
    .join('\n\n')
  return text.length > TRANSCRIPT_CHARS ? text.slice(-TRANSCRIPT_CHARS) : text
}

/** Extracts entries from a finished Session and adds the ones the store accepts. */
export async function extract(
  options: MemoryWorkOptions,
  request: ExtractionRequest,
): Promise<{ added: number; refused: number }> {
  const turns = readTranscript(request.transcriptPath)
  if (turns.length === 0) return { added: 0, refused: 0 }
  const project = projectSlot(request.cwd)
  const reply = await options.run({
    title: `Memory: extract from ${request.sessionId.slice(0, 8)}`,
    cwd: sessionCwd(project),
    modelId: options.modelId,
    prompt: options.prompt,
    input: JSON.stringify({
      workspace: request.cwd,
      existing: options.store.list(project).map(({ category, text }) => ({ category, text })),
      transcript: transcriptText(turns),
    }),
    schema: EXTRACTION_SCHEMA,
  })
  const entries =
    typeof reply === 'object' &&
    reply !== null &&
    Array.isArray((reply as { entries?: unknown }).entries)
      ? ((reply as { entries: unknown[] }).entries as Array<Record<string, unknown>>)
      : []
  let added = 0
  let refused = 0
  const touched = new Set<'project' | 'global'>()
  for (const entry of entries) {
    const scope = entry['scope'] === 'global' ? 'global' : 'project'
    if (!isCategory(entry['category']) || typeof entry['text'] !== 'string') {
      refused += 1
      continue
    }
    const result = options.store.add(
      scope === 'global' ? { scope: 'global' } : project,
      entry['category'],
      entry['text'],
    )
    if (result.ok) {
      added += 1
      touched.add(scope)
    } else refused += 1
  }
  if (touched.has('project')) exportMarkdown(options.store, project)
  if (touched.has('global')) exportMarkdown(options.store, { scope: 'global' })
  return { added, refused }
}
