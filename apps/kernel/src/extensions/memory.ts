import { readFileSync } from 'node:fs'
import { defineExtension, defineTool, section } from '@earendil-works/pi-durable'
import type { Extension, ToolRegistration } from '@earendil-works/pi-durable'
import { arr, int, obj, oneOf, opt, str } from '@earendil-works/pi-schema'
import type { HistoryHit, Indexer } from '../indexer/index.ts'
import { searchWords } from '../indexer/index.ts'
import {
  SESSION_SUMMARIES_DIR,
  list,
  memoryContext,
  numbered,
  resolveFile,
  search,
  writeNote,
} from '../memory/folder.ts'

export type MemoryOptions = {
  /**
   * The Memory folder, or a function that answers it as it is now: undefined once Memory was turned off while the
   * Kernel runs (`harness.configure`), until the conversation's next turn leaves the extension out.
   */
  dir: string | (() => string | undefined)
  /** A Session Summary was read: the Host counts it, so the summary does not age out. */
  onRead: (path: string, at: Date) => void
  /** The index `history_search` reads; undefined until the Kernel has opened it. */
  history?: () => Indexer | undefined
  now?: () => Date
}

/** The section's key; its text is the conversation's snapshot of Memory. */
export const MEMORY_SECTION = 'memory'

type Output = { output: { type: 'text'; text: string }[]; isError?: boolean }
const answer = (text: string, isError = false): Output => ({
  output: [{ type: 'text', text }],
  ...(isError ? { isError: true } : {}),
})

/** A number argument as the Go Memory Server read one: floored and kept within 1 and `max`. */
const bounded = (value: number | undefined, fallback: number, max: number): number =>
  value === undefined ? fallback : Math.min(Math.max(1, Math.floor(value)), max)

/**
 * Memory in the Kernel (ADR 0042): four tools that read the Memory folder and write only a Memory Note, and the
 * Memory Summary in each conversation's prompt.
 *
 * The section is a snapshot: a conversation keeps the text it was first shown, so a note or a new Memory Summary
 * written meanwhile does not change its prompt and the provider's prompt cache holds. After a compaction the
 * conversation is shown Memory as it is then.
 */
export function memoryExtension(options: MemoryOptions): Extension {
  const now = options.now ?? (() => new Date())
  const folder = (): string | undefined =>
    typeof options.dir === 'function' ? options.dir() : options.dir
  const off = () => answer('Memory is turned off.', true)
  const memorySearch = defineTool({
    name: 'memory_search',
    description:
      'Search Droi Memory (the Memory Summary, the Session Summaries of earlier Sessions and the Memory Notes) for words, when the Memory Summary does not point you to what you need. Answers matching lines as file:line.',
    parameters: obj({
      query: str({
        description:
          'One to three specific words; lines holding all of them come first. Quote a phrase to keep it whole.',
      }),
      limit: opt(int({ minimum: 1, maximum: 100, description: 'Default 20.' })),
    }),
    replay: 'safe',
    callers: ['model'],
    async execute(args) {
      const dir = folder()
      if (dir === undefined) return off()
      if (args.query.trim() === '') return answer('memory_search needs a query.', true)
      return answer(search(dir, args.query, bounded(args.limit, 20, 100)))
    },
  })
  const memoryRead = defineTool({
    name: 'memory_read',
    description:
      'Read one Memory file: a Session Summary the Memory Summary points to (session_summaries/...), a Memory Note (notes/...) or memory_summary.md. Lines are numbered.',
    parameters: obj({
      path: str({
        description:
          "The file's path in Memory, such as session_summaries/2026-10-27T09-21-03-1a2b3c4d-fix-login.md.",
      }),
      offset: opt(int({ minimum: 1, description: 'First line to read; default 1.' })),
      limit: opt(int({ minimum: 1, maximum: 2000, description: 'Lines to read; default 2000.' })),
    }),
    replay: 'safe',
    callers: ['model'],
    async execute(args) {
      const dir = folder()
      if (dir === undefined) return off()
      const found = resolveFile(dir, args.path)
      if ('error' in found) return answer(found.error, true)
      const text = readFileSync(found.file, 'utf8')
      if (found.path.startsWith(`${SESSION_SUMMARIES_DIR}/`)) options.onRead(found.path, now())
      return answer(
        numbered(
          found.path,
          text,
          bounded(args.offset, 1, 2 ** 31 - 1),
          bounded(args.limit, 2_000, 2_000),
        ),
      )
    },
  })
  const memoryList = defineTool({
    name: 'memory_list',
    description:
      "List Memory's files: the Session Summaries newest first, with their day, Workspace and title, and the Memory Notes.",
    parameters: obj({
      workspace: opt(
        str({ description: 'Only Session Summaries whose Workspace path contains this.' }),
      ),
      limit: opt(int({ minimum: 1, maximum: 300, description: 'Default 50.' })),
    }),
    replay: 'safe',
    callers: ['model'],
    async execute(args) {
      const dir = folder()
      if (dir === undefined) return off()
      return answer(list(dir, args.workspace ?? '', bounded(args.limit, 50, 300)))
    },
  })
  const memoryNote = defineTool({
    name: 'memory_note',
    description:
      'Only when the user explicitly asks you to remember, forget or correct something for later Sessions: save what they asked as a Memory Note. Droi folds it into the Memory Summary, where it wins over older memory. Do not call it for things you learned on your own; Droi summarizes finished Sessions by itself. Never include secrets.',
    parameters: obj({
      text: str({
        description:
          'What the user asked, in their language, standing on its own: "The user asked to remember: ...", "The user asked to forget: ..." or "The user corrected: ...".',
      }),
    }),
    // A note written twice is two notes.
    replay: 'unsafe',
    callers: ['model'],
    async execute(args, api) {
      const dir = folder()
      if (dir === undefined) return off()
      const saved = writeNote(dir, args.text, String(api.conversationId), now())
      if ('error' in saved) return answer(saved.error, true)
      return answer(
        `Saved ${saved.path}. Droi folds it into the Memory Summary within a few minutes, and every new Session sees it until then.`,
      )
    },
  })
  const historySearch = defineTool({
    name: 'history_search',
    description:
      "Search the words of earlier conversations on this computer, as they were said. Answer from the Memory Summary and the Session Summaries first (memory_search, memory_read); use this only when they do not settle the question, since it costs more. Answers the newest messages that hold every word, at most 20, each with its conversation, kind, time and an excerpt with the words marked. Automation and Workstream runs and subagents' conversations are left out unless include names them, and the answer says how many were.",
    parameters: obj({
      query: str({
        description:
          'Specific words, such as an error, a file name or a decision; all must appear. Two Chinese characters are a word.',
      }),
      include: opt(
        arr(oneOf(['automations', 'workstreams', 'subagents']), {
          description: 'Also search these conversations.',
        }),
      ),
      limit: opt(int({ minimum: 1, maximum: 20, description: 'Default 20.' })),
    }),
    replay: 'safe',
    callers: ['model'],
    async execute(args, api) {
      const index = options.history?.()
      if (index === undefined) return answer('Earlier conversations cannot be searched yet.', true)
      if (args.query.trim() === '') return answer('history_search needs a query.', true)
      const { hits, leftOut } = await index.history({
        text: args.query,
        limit: bounded(args.limit, 20, 20),
        exclude: api.conversationId as unknown as number,
        ...(args.include === undefined ? {} : { include: args.include }),
      })
      return answer(historyText(args.query, hits, leftOut))
    },
  })
  return defineExtension({
    name: 'memory',
    tools: [memorySearch, memoryRead, memoryList, memoryNote, historySearch] as ToolRegistration[],
    sections: [
      section(
        MEMORY_SECTION,
        ({ shown }) => {
          if (shown[MEMORY_SECTION] !== undefined) return shown[MEMORY_SECTION]
          const dir = folder()
          return dir === undefined ? '' : memoryContext(dir)
        },
        { tag: false },
      ),
    ],
  })
}

/** How much of an entry an answer shows: about 200 characters around the first word it holds. */
const BEFORE = 60
const AFTER = 140

/**
 * The answer of `history_search`: the hits grouped by conversation, in the order the search gave them, and how many
 * conversations matched but were left out.
 */
export function historyText(query: string, hits: readonly HistoryHit[], leftOut = 0): string {
  const more =
    leftOut === 0
      ? ''
      : `\n${leftOut} more ${leftOut === 1 ? 'conversation matches' : 'conversations match'} among Automation runs, Workstream runs and subagents; include names them.\n`
  if (hits.length === 0)
    return `Nothing in earlier conversations matches ${JSON.stringify(query)}.${more === '' ? '' : `\n${more}`}`
  const words = searchWords(query)
  const groups = new Map<number, string[]>()
  const heads = new Map<number, string>()
  for (const hit of hits) {
    const c = hit.conversation
    if (!heads.has(c.id)) {
      heads.set(
        c.id,
        `Conversation ${c.id} · ${day(c.last_activity_at)} · ${c.title ?? '(untitled)'}`,
      )
      groups.set(c.id, [])
    }
    const about = [
      `entry ${hit.entryId}`,
      ...(hit.kind === undefined ? [] : [hit.kind.replace(/^pi\./, '')]),
      ...(hit.at === undefined ? [] : [minute(hit.at)]),
    ].join(' · ')
    groups.get(c.id)!.push(`- ${about}: ${excerpt(hit.text, words)}`)
  }
  let out = `${hits.length} messages in earlier conversations hold every word of ${JSON.stringify(query)}, newest first:\n`
  for (const [id, lines] of groups) out += `\n${heads.get(id)}\n${lines.join('\n')}\n`
  return out + more
}

function day(at: number): string {
  return new Date(at).toISOString().slice(0, 10)
}

function minute(at: number): string {
  return `${new Date(at).toISOString().slice(0, 16).replace('T', ' ')} UTC`
}

/**
 * The text around the first of `words` it holds, on one line, with an ellipsis where it was cut and every word
 * marked as **word**.
 */
function excerpt(text: string, words: readonly string[]): string {
  const lower = text.toLowerCase()
  const found = words.map((word) => lower.indexOf(word.toLowerCase())).filter((index) => index >= 0)
  const at = found.length === 0 ? 0 : Math.min(...found)
  const chars = Array.from(text)
  // The index is in UTF-16 units; the cut is in characters, so a surrogate pair is never split.
  const start = Math.max(0, Array.from(text.slice(0, at)).length - BEFORE)
  const end = Math.min(chars.length, start + BEFORE + AFTER)
  const middle = chars
    .slice(start, end)
    .join('')
    .split(/\s+/)
    .filter((part) => part !== '')
    .join(' ')
  // Longest first, so a word inside another is marked once, as part of the longer one.
  const pattern = [...words]
    .sort((a, b) => b.length - a.length)
    .map((word) => word.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    .join('|')
  const marked = pattern === '' ? middle : middle.replace(new RegExp(pattern, 'giu'), '**$&**')
  return `${start > 0 ? '…' : ''}${marked}${end < chars.length ? '…' : ''}`
}
