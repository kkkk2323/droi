import type { EntryRecord, Storage } from '@earendil-works/pi-durable'
import type { Message } from '@earendil-works/pi-ai'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import { type ConversationRow, migrate, readMeta, writeMeta } from './schema.ts'

/**
 * Droi's conversation list, search index and view times, kept in the kernel's one SQLite file.
 *
 * **Nothing here may be the only record of something.** Pi's entries are durable when its commit
 * resolves; the index rows are written a moment later, and the process can die in between. So the index
 * carries a **watermark**: the highest entry ID it has stored, written in the same transaction as the
 * rows themselves. On every start, every entry above the watermark is read back from Pi and indexed —
 * which is the whole history on a first run, and nothing on a clean start. A crash therefore costs a
 * restart's worth of work, never a conversation that vanished from the list and from search.
 *
 * Writes are batched: on Windows a commit costs about 15.6 ms, so one per entry would make a long run
 * crawl. A flush is one transaction, at most `flushIntervalMs` after the first queued write.
 */
export type IndexerOptions = {
  /** Pause before a queued write is stored. 150 ms keeps a streaming answer to one flush per burst. */
  flushIntervalMs?: number
  now?: () => number
  /** Called after a flush that changed a conversation, so clients hear about it. */
  onChange?: (conversations: readonly number[]) => void
}

type PendingTouch = {
  at: number
  preview?: string
  title?: string
  workspace?: string
  kind?: string
  parentId?: number
  requestId?: string
  requestFingerprint?: string
  createdAt?: number
}
type PendingEntry = { id: number; conversation: number; text: string }

const TITLE_LIMIT = 120
const PREVIEW_LIMIT = 200
const SCAN = 500

export class Indexer {
  #db: BunSqliteDatabase
  #storage: Storage
  #flushIntervalMs: number
  #now: () => number
  #onChange: ((conversations: readonly number[]) => void) | undefined
  #entries: PendingEntry[] = []
  #touches = new Map<number, PendingTouch>()
  #timer: ReturnType<typeof setTimeout> | undefined
  #flush: Promise<void> | undefined
  #watermark = 0
  #closed = false

  constructor(db: BunSqliteDatabase, storage: Storage, options: IndexerOptions = {}) {
    this.#db = db
    this.#storage = storage
    this.#flushIntervalMs = options.flushIntervalMs ?? 150
    this.#now = options.now ?? Date.now
    this.#onChange = options.onChange
  }

  async open(): Promise<void> {
    await migrate(this.#db)
    this.#watermark = Number((await readMeta(this.#db, 'index.entryWatermark')) ?? 0)
  }

  /** The highest entry ID whose text is in the index. */
  get watermark(): number {
    return this.#watermark
  }

  /**
   * Index every entry above the watermark, from Pi's own tables. This is the first-run backfill and the
   * crash repair in one: a restart after a kill re-reads exactly what the last flush did not store.
   */
  async catchUp(): Promise<number> {
    let cursor: never | undefined
    let indexed = 0
    const from = this.#watermark
    do {
      const page = await this.#storage.scanConversations(
        { order: 'ascending' },
        SCAN,
        cursor,
        context,
      )
      for (const record of page.items) {
        const id = record.id as unknown as number
        const rows: PendingEntry[] = []
        let title: string | undefined
        let preview: string | undefined
        let newest = 0
        let entryCursor: never | undefined
        do {
          const entries = await this.#storage.scanEntries(
            {
              conversationId: record.id,
              order: 'ascending',
              ...(from > 0 ? { minEntryId: (from + 1) as never } : {}),
            },
            SCAN,
            entryCursor,
            context,
          )
          for (const entry of entries.items) {
            newest = Math.max(newest, entry.id as unknown as number)
            const text = entryText(entry)
            if (text === '') continue
            rows.push({ id: entry.id as unknown as number, conversation: id, text })
            // The name comes from the first user message of the whole conversation, so a repaired run
            // that only sees later entries does not rename anything.
            if (entry.kind === 'pi.user') title ??= firstLine(text, TITLE_LIMIT)
            preview = firstLine(text, PREVIEW_LIMIT)
          }
          entryCursor = entries.next as never
        } while (entryCursor !== undefined)

        if (rows.length === 0 && from > 0 && newest === 0) {
          // Nothing new here: leave the row and its title alone.
          await this.touch(id, this.#now(), {})
          continue
        }
        this.#entries.push(...rows)
        // A conversation that already has a row keeps its own title: a repair must not overwrite a
        // user's rename with the first message again.
        const existing = await this.#db.get<{ title: string | null }>(
          'SELECT title FROM droi_conversations WHERE id = ?',
          id,
        )
        await this.touch(id, this.#now(), {
          ...(existing?.title == null && title !== undefined ? { title } : {}),
          ...(preview === undefined ? {} : { preview }),
        })
        indexed += rows.length
      }
      cursor = page.next as never
    } while (cursor !== undefined)
    await this.flush()
    return indexed
  }

  /** A conversation exists and had activity at `at`. Queues one flush. */
  touch(
    id: number,
    at: number,
    extra: {
      workspace?: string
      kind?: string
      title?: string
      preview?: string
      parentId?: number
      requestId?: string
      requestFingerprint?: string
      createdAt?: number
    },
  ): Promise<void> {
    if (this.#closed) return Promise.resolve()
    const pending = this.#touches.get(id) ?? { at }
    pending.at = Math.max(pending.at, at)
    if (extra.title !== undefined) pending.title = extra.title
    if (extra.preview !== undefined) pending.preview = extra.preview
    if (extra.workspace !== undefined) pending.workspace = extra.workspace
    if (extra.kind !== undefined) pending.kind = extra.kind
    if (extra.parentId !== undefined) pending.parentId = extra.parentId
    if (extra.requestId !== undefined) pending.requestId = extra.requestId
    if (extra.requestFingerprint !== undefined)
      pending.requestFingerprint = extra.requestFingerprint
    if (extra.createdAt !== undefined) pending.createdAt = extra.createdAt
    this.#touches.set(id, pending)
    return this.#queue()
  }

  /** Remember an entry: its text for search, the conversation's name, and its last words as a preview. */
  entry(entry: EntryRecord): void {
    if (this.#closed) return
    const text = entryText(entry)
    if (text !== '') {
      this.#entries.push({
        id: entry.id as unknown as number,
        conversation: entry.conversationId as unknown as number,
        text,
      })
    }
    const id = entry.conversationId as unknown as number
    const pending = this.#touches.get(id) ?? { at: this.#now() }
    if (entry.kind === 'pi.user' && pending.title === undefined && text !== '') {
      pending.title = firstLine(text, TITLE_LIMIT)
    }
    const preview = previewText(entry)
    if (preview !== undefined) pending.preview = preview
    this.#touches.set(id, pending)
    void this.#queue()
  }

  /**
   * Follow stored commits: every entry that reached the database is queued for the index in the same
   * order Pi stored it, and the watermark moves with the flush.
   */
  observeCommits() {
    return {
      advance: (publication: { changes: readonly { type: string; value?: unknown }[] }) => {
        for (const change of publication.changes) {
          if (change.type !== 'entry') continue
          this.entry(change.value as EntryRecord)
        }
      },
      end: () => {
        void this.flush()
      },
    }
  }

  #queue(): Promise<void> {
    if (this.#flushIntervalMs <= 0) return this.flush()
    if (this.#timer !== undefined) return Promise.resolve()
    this.#timer = setTimeout(() => {
      this.#timer = undefined
      void this.flush()
    }, this.#flushIntervalMs)
    this.#timer.unref?.()
    return Promise.resolve()
  }

  /** Store everything queued, in one transaction, with the watermark it covers. */
  async flush(): Promise<void> {
    if (this.#flush !== undefined) {
      await this.#flush
      if (this.#entries.length === 0 && this.#touches.size === 0) return
    }
    if (this.#timer !== undefined) {
      clearTimeout(this.#timer)
      this.#timer = undefined
    }
    const entries = this.#entries
    const touches = [...this.#touches.entries()]
    if (entries.length === 0 && touches.length === 0) return
    this.#entries = []
    this.#touches.clear()
    const highest = entries.reduce((max, entry) => Math.max(max, entry.id), this.#watermark)
    const changed = touches.map(([id]) => id)
    const run = this.#db.transaction(async (tx) => {
      for (const [id, pending] of touches) {
        await tx.run(
          `INSERT INTO droi_conversations
             (id, created_at, last_activity_at, workspace, kind, title, preview, parent_id, request_id, request_fingerprint)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT (id) DO UPDATE SET
             last_activity_at = MAX(droi_conversations.last_activity_at, excluded.last_activity_at),
             workspace = COALESCE(excluded.workspace, droi_conversations.workspace),
             kind = COALESCE(excluded.kind, droi_conversations.kind),
             title = COALESCE(droi_conversations.title, excluded.title),
             preview = COALESCE(excluded.preview, droi_conversations.preview),
             parent_id = COALESCE(droi_conversations.parent_id, excluded.parent_id),
             request_id = COALESCE(droi_conversations.request_id, excluded.request_id),
             request_fingerprint = COALESCE(droi_conversations.request_fingerprint, excluded.request_fingerprint)`,
          id,
          pending.createdAt ?? pending.at,
          pending.at,
          pending.workspace ?? null,
          pending.kind ?? null,
          pending.title ?? null,
          pending.preview ?? null,
          pending.parentId ?? null,
          pending.requestId ?? null,
          pending.requestFingerprint ?? null,
        )
      }
      for (const entry of entries) {
        // FTS5 has no UPSERT; REPLACE is its own conflict handling, and an entry ID is never reused.
        await tx.run(
          'INSERT OR REPLACE INTO droi_entries_fts (rowid, text, conversation) VALUES (?, ?, ?)',
          entry.id,
          entry.text,
          entry.conversation,
        )
      }
      if (highest > this.#watermark) {
        await writeMeta(tx, 'index.entryWatermark', String(highest))
      }
    })
    this.#flush = run.then(
      () => undefined,
      () => undefined,
    )
    try {
      await run
      this.#watermark = Math.max(this.#watermark, highest)
      if (changed.length > 0) this.#onChange?.(changed)
    } finally {
      this.#flush = undefined
    }
  }

  async setTitle(id: number, title: string): Promise<void> {
    await this.#db.run('UPDATE droi_conversations SET title = ? WHERE id = ?', title, id)
  }

  /** Archive, rename or re-kind one conversation. Absent fields stay as they are; `null` clears. */
  async update(
    id: number,
    change: { title?: string | null; archived?: boolean; kind?: string | null },
  ): Promise<void> {
    await this.flush()
    // A title the user chose must survive the next repair, so an empty title stands for "chosen".
    if (change.title !== undefined) {
      await this.#db.run(
        'UPDATE droi_conversations SET title = ? WHERE id = ?',
        change.title ?? '',
        id,
      )
    }
    if (change.kind !== undefined) {
      await this.#db.run('UPDATE droi_conversations SET kind = ? WHERE id = ?', change.kind, id)
    }
    if (change.archived !== undefined) {
      await this.#db.run(
        'UPDATE droi_conversations SET archived_at = ? WHERE id = ?',
        change.archived ? this.#now() : null,
        id,
      )
    }
  }

  async markViewed(id: number, at = this.#now()): Promise<void> {
    await this.#db.run('UPDATE droi_conversations SET viewed_at = ? WHERE id = ?', at, id)
  }

  async conversation(id: number): Promise<ConversationRow | undefined> {
    await this.flush()
    return this.#db.get<ConversationRow>('SELECT * FROM droi_conversations WHERE id = ?', id)
  }

  async byRequestId(requestId: string): Promise<ConversationRow | undefined> {
    await this.flush()
    return this.#db.get<ConversationRow>(
      'SELECT * FROM droi_conversations WHERE request_id = ?',
      requestId,
    )
  }

  /** The `requestId` → fingerprint memo for submissions of one conversation. */
  async requestFingerprint(conversationId: number, requestId: string): Promise<string | undefined> {
    await this.flush()
    const row = await this.#db.get<{ fingerprint: string; submission_id: number | null }>(
      'SELECT fingerprint, submission_id FROM droi_requests WHERE conversation_id = ? AND request_id = ?',
      conversationId,
      requestId,
    )
    return row?.fingerprint
  }

  async submissionForRequest(
    conversationId: number,
    requestId: string,
  ): Promise<number | undefined> {
    await this.flush()
    const row = await this.#db.get<{ submission_id: number | null }>(
      'SELECT submission_id FROM droi_requests WHERE conversation_id = ? AND request_id = ?',
      conversationId,
      requestId,
    )
    return row?.submission_id ?? undefined
  }

  async rememberRequest(
    conversationId: number,
    requestId: string,
    fingerprint: string,
    submissionId: number,
  ): Promise<void> {
    await this.#db.run(
      `INSERT INTO droi_requests (conversation_id, request_id, fingerprint, submission_id)
       VALUES (?, ?, ?, ?)
       ON CONFLICT (conversation_id, request_id) DO UPDATE SET
         fingerprint = excluded.fingerprint,
         submission_id = COALESCE(droi_requests.submission_id, excluded.submission_id)`,
      conversationId,
      requestId,
      fingerprint,
      submissionId,
    )
  }

  /**
   * The list, newest activity first. `text` searches the entry index and keeps the conversations whose
   * text matches; the title is searched too, since a renamed conversation may hold nothing else.
   */
  async list(query: {
    kind?: string
    archived?: boolean
    workspace?: string
    text?: string
    /** The last row of the previous page: `{before: id}`. */
    cursor?: { before?: number }
    limit?: number
  }): Promise<{ items: ConversationRow[]; next?: { before: number } }> {
    await this.flush()
    const limit = Math.min(Math.max(query.limit ?? 50, 1), 200)
    const where: string[] = []
    const params: (string | number | null)[] = []
    // The archive is a place you ask for: without `archived`, the list is the live conversations.
    if (query.archived === true) where.push('archived_at IS NOT NULL')
    else where.push('archived_at IS NULL')
    if (query.kind !== undefined) {
      where.push('kind = ?')
      params.push(query.kind)
    }
    if (query.workspace !== undefined) {
      where.push('workspace = ?')
      params.push(query.workspace)
    }
    if (query.text !== undefined && query.text.trim() !== '') {
      const words = searchWords(query.text)
      if (words.length === 0) {
        // Nothing to look for, so nothing matches: a list of every conversation would be a wrong answer.
        where.push('0')
      } else {
        // Plain `text LIKE ?` is what the trigram index can answer; `(text || '') LIKE ?` would force a
        // scan of the whole index, which is what the query plan in `search.test.ts` pins.
        const clauses = words.map(() => "text LIKE ? ESCAPE '\\'")
        where.push(
          `(id IN (SELECT conversation FROM droi_entries_fts WHERE ${clauses.join(' AND ')})
            OR (title || '') LIKE ? ESCAPE '\\')`,
        )
        for (const word of words) params.push(`%${escapeLike(word)}%`)
        params.push(`%${escapeLike(query.text.trim())}%`)
      }
    }
    const before = query.cursor?.before
    if (before !== undefined) {
      // Newest activity first, and the ID breaks ties so a page boundary is never ambiguous. The row the
      // cursor names carries the activity the next page must stay below.
      const anchor = await this.#db.get<{ last_activity_at: number }>(
        'SELECT last_activity_at FROM droi_conversations WHERE id = ?',
        before,
      )
      if (anchor === undefined) {
        where.push('id < ?')
        params.push(before)
      } else {
        where.push('(last_activity_at < ? OR (last_activity_at = ? AND id < ?))')
        params.push(anchor.last_activity_at, anchor.last_activity_at, before)
      }
    }
    const sql = `SELECT * FROM droi_conversations
      ${where.length > 0 ? `WHERE ${where.join(' AND ')}` : ''}
      ORDER BY last_activity_at DESC, id DESC LIMIT ?`
    const rows = await this.#db.all<ConversationRow>(sql, ...params, limit + 1)
    const items = rows.slice(0, limit)
    const last = items.at(-1)
    return last !== undefined && rows.length > limit
      ? { items, next: { before: last.id } }
      : { items }
  }

  async close(): Promise<void> {
    await this.flush()
    this.#closed = true
    if (this.#timer !== undefined) clearTimeout(this.#timer)
  }
}

const context = { signal: new AbortController().signal } as never

/** Text an entry contributes to search: its messages, and the note a `droi.` entry carries. */
export function entryText(entry: EntryRecord): string {
  return messageTextOf(entry.model) + '\n' + dataText(entry.data)
}

function dataText(data: unknown): string {
  if (data === undefined || data === null) return ''
  if (typeof data === 'string') return data
  const record = data as Record<string, unknown>
  const parts: string[] = []
  for (const key of ['text', 'message', 'title', 'question', 'command']) {
    const value = record[key]
    if (typeof value === 'string') parts.push(value)
  }
  return parts.join('\n')
}

function messageTextOf(model: readonly Message[] | undefined): string {
  if (model === undefined) return ''
  const parts: string[] = []
  for (const message of model) parts.push(messageText(message))
  return parts.join('\n')
}

function messageText(message: Message): string {
  const content = (message as { content?: unknown }).content
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  const parts: string[] = []
  for (const item of content) {
    if (typeof item === 'object' && item !== null && (item as { type?: string }).type === 'text') {
      parts.push(String((item as { text?: unknown }).text ?? ''))
    }
  }
  return parts.join('\n')
}

/** The last text an entry wrote, for the list's second line. */
function previewText(entry: EntryRecord): string | undefined {
  if (entry.kind !== 'pi.assistant' && entry.kind !== 'pi.user') return undefined
  const text = entryText(entry).trim()
  return text === '' ? undefined : firstLine(text, PAGE_PREVIEW)
}

const PAGE_PREVIEW = PREVIEW_LIMIT

function firstLine(text: string, limit: number): string {
  const line = text.trim().split('\n')[0] ?? ''
  return line.length > limit ? `${line.slice(0, limit - 1)}…` : line
}

/**
 * A search text as words to look for, each a substring match joined by AND, so "parse conf" finds
 * "parseConfig" and a later word narrows an earlier one.
 *
 * Whitespace and punctuation split Latin text; a run of Chinese, Japanese or Korean is kept whole,
 * because those scripts do not put spaces between words and splitting a run into single characters
 * would match far too much.
 */
export function searchWords(text: string): string[] {
  return text
    .trim()
    .split(/\s+/u)
    .map((word) => word.replace(/^[\p{P}]+|[\p{P}]+$/gu, ''))
    .filter((word) => word !== '')
    .slice(0, 8)
}

/** A word as a LIKE pattern can take it: the wildcards it may contain are its own characters. */
export function escapeLike(word: string): string {
  return word.replace(/[\\%_]/g, (character) => `\\${character}`)
}

/**
 * How far back a search term shorter than the index's three characters reaches.
 *
 * The trigram tokenizer answers a pattern of three or more characters from the index. A shorter one —
 * two Chinese characters, the common case for 内存 or 配置 — cannot be answered that way; SQLite falls
 * back to reading the table, which is correct and, measured on 20,000 entries of realistic length, about
 * 5 ms. The bound keeps that from growing with a user's whole history: a short term searches the most
 * recent 50,000 entries and says so in the protocol README. Three characters or more is unbounded and
 * exact.
 */
export const SHORT_TERM_WINDOW = 50_000

/** The SQL for a search: the words as LIKE patterns, and whether each needs the bounded scan. */
export function likeClauses(words: readonly string[]): {
  clauses: string[]
  values: (string | number)[]
} {
  const clauses: string[] = []
  const values: (string | number)[] = []
  for (const word of words) {
    if ([...word].length >= 3) {
      // Plain `text LIKE ?` is the form the trigram index answers with its `L0` optimization.
      clauses.push("text LIKE ? ESCAPE '\\'")
      values.push(`%${escapeLike(word)}%`)
    } else {
      clauses.push(
        "text LIKE ? ESCAPE '\\' AND rowid > (SELECT MAX(rowid) - ? FROM droi_entries_fts)",
      )
      values.push(`%${escapeLike(word)}%`, SHORT_TERM_WINDOW)
    }
  }
  return { clauses, values }
}
