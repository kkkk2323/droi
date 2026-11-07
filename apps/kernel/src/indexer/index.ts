import type { SqliteExecutor } from '@earendil-works/chord/sqlite'
import type { EntryRecord, Storage } from '@earendil-works/pi-durable'
import type { Message } from '@earendil-works/pi-ai'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import { type ConversationMeta, type ConversationRow, migrate } from './schema.ts'

/**
 * Droi's conversation list, search index and view times, kept in the kernel's one SQLite file.
 *
 * **Nothing here may be the only record of something.** Two mechanisms keep that true:
 *
 * - **A per-conversation watermark**: the highest entry ID whose text is indexed, written in the same
 *   transaction as the rows it covers. A conversation's watermark can only advance over entries this
 *   index has already stored, so an interrupted catch-up re-reads exactly what it missed — never a
 *   neighbour's older entries. (One global number cannot say that: advancing it past conversation A
 *   while B is still unread skips B's smaller IDs for good.)
 * - **The conversation's own transcript**: `droi.meta`, appended in the commit that creates the
 *   conversation, carries its requestId, fingerprint, kind and workspace. The list row is the fast path;
 *   when it is missing, the row is rebuilt from that entry.
 *
 * Writes are batched — on Windows a commit costs about 15.6 ms, so one per entry would make a long run
 * crawl — but reading a conversation's activity never invents a time: it moves to the newest entry's own
 * timestamp, so a restart that finds nothing new changes nothing.
 */
export type IndexerOptions = {
  /** Pause before a queued write is stored. 150 ms keeps a streaming answer to one flush per burst. */
  flushIntervalMs?: number
  now?: () => number
  /** Called after a flush that changed a conversation, so clients hear about it. */
  onChange?: (conversations: readonly number[]) => void
  /** Called when a flush fails. A storage error here is worth a warning, not a crash. */
  onError?: (error: unknown) => void
}

/** A conversation's row as a flush will write it. */
type PendingRow = {
  id: number
  /** Present for a row that does not exist yet. */
  insert?: {
    createdAt: number
    kind?: string
    workspace?: string
    requestId?: string
    requestFingerprint?: string
    parentId?: number
  }
  /** The newest entry's own timestamp: the only thing that may move `last_activity_at`. */
  activity?: number
  title?: string
  preview?: string
  /** Set when a real event happened now (a create, a submit), not by recovery. */
  eventAt?: number
}

type PendingEntry = { id: number; conversation: number; text: string }

const TITLE_LIMIT = 120
const PREVIEW_LIMIT = 200
const SCAN = 500

/**
 * How far back a search term shorter than the index's three characters reaches.
 *
 * The trigram tokenizer answers a pattern of three or more characters from the index. A shorter one —
 * two Chinese characters, the common case for 内存 or 配置 — cannot be answered that way; SQLite falls
 * back to reading the table, which is correct and, measured on 20,000 entries of realistic length, about
 * 5 ms. The bound keeps that from growing with a user's whole history: a short term searches the most
 * recent 50,000 entries. Three characters or more is unbounded and exact.
 */
export const SHORT_TERM_WINDOW = 50_000

export class Indexer {
  #db: BunSqliteDatabase
  #storage: Storage
  #flushIntervalMs: number
  #now: () => number
  #onChange: ((conversations: readonly number[]) => void) | undefined
  #onError: ((error: unknown) => void) | undefined
  #entries: PendingEntry[] = []
  #rows = new Map<number, PendingRow>()
  #timer: ReturnType<typeof setTimeout> | undefined
  #flush: Promise<void> | undefined
  #watermarks = new Map<number, number>()
  #closed = false

  constructor(db: BunSqliteDatabase, storage: Storage, options: IndexerOptions = {}) {
    this.#db = db
    this.#storage = storage
    this.#flushIntervalMs = options.flushIntervalMs ?? 150
    this.#now = options.now ?? Date.now
    this.#onChange = options.onChange
    this.#onError = options.onError
  }

  async open(): Promise<void> {
    await migrate(this.#db)
    const rows = await this.#db.all<{ conversation_id: number; entry_id: number }>(
      'SELECT conversation_id, entry_id FROM droi_index_watermarks',
    )
    this.#watermarks = new Map(rows.map((row) => [row.conversation_id, row.entry_id]))
  }

  /** The highest entry ID whose text is indexed for one conversation. */
  watermark(conversationId: number): number {
    return this.#watermarks.get(conversationId) ?? 0
  }

  /** How many conversations have a watermark, for a test. */
  get watermarkedConversations(): number {
    return this.#watermarks.size
  }

  /**
   * Bring the index up to date with what Pi stored: every entry above each conversation's own watermark,
   * and a row for any conversation that has none.
   *
   * This is the first-run build and the crash repair in one. It reads a conversation at a time and
   * flushes when that conversation is done, so an interruption leaves every finished conversation
   * watermarked and the rest untouched — the next start re-reads only those.
   */
  async catchUp(): Promise<number> {
    let cursor: never | undefined
    let indexed = 0
    do {
      const page = await this.#storage.scanConversations(
        { order: 'ascending' },
        SCAN,
        cursor,
        context,
      )
      for (const record of page.items) {
        const id = record.id as unknown as number
        indexed += await this.#catchUpConversation(
          id,
          record.parent?.conversationId as unknown as number,
        )
      }
      cursor = page.next as never
    } while (cursor !== undefined)
    await this.flush()
    return indexed
  }

  async #catchUpConversation(id: number, parentId: number | undefined): Promise<number> {
    const row = await this.#db.get<ConversationRow>(
      'SELECT * FROM droi_conversations WHERE id = ?',
      id,
    )
    const from = this.watermark(id)
    let cursor: never | undefined
    let count = 0
    let newest: number | undefined
    let title: string | undefined
    let preview: string | undefined
    let meta: ConversationMeta | undefined
    let firstEntryAt: number | undefined
    const requests: { requestId: string; fingerprint: string; submissionId?: number }[] = []

    do {
      const entries = await this.#storage.scanEntries(
        {
          conversationId: id as never,
          order: 'ascending',
          ...(from > 0 ? { minEntryId: (from + 1) as never } : {}),
        },
        SCAN,
        cursor,
        context,
      )
      for (const entry of entries.items) {
        const entryId = entry.id as unknown as number
        if (entryId <= from) continue
        const at = entryTimestamp(entry)
        firstEntryAt ??= at
        newest = Math.max(newest ?? 0, at)
        const text = entryText(entry)
        if (entry.kind === 'droi.meta') meta ??= entry.data as ConversationMeta
        if (entry.kind === 'droi.request') {
          const request = entry.data as {
            requestId?: unknown
            fingerprint?: unknown
            submissionId?: unknown
          }
          if (typeof request.requestId === 'string' && typeof request.fingerprint === 'string') {
            requests.push({
              requestId: request.requestId,
              fingerprint: request.fingerprint,
              ...(typeof request.submissionId === 'number'
                ? { submissionId: request.submissionId }
                : {}),
            })
          }
        }
        if (text !== '') {
          this.#entries.push({ id: entryId, conversation: id, text })
          count++
          // The name comes from the first user message, so a repair that sees only later entries keeps
          // the one the conversation already had.
          if (title === undefined && entry.kind === 'pi.user') title = firstLine(text, TITLE_LIMIT)
          preview = firstLine(text, PREVIEW_LIMIT)
        }
      }
      cursor = entries.next as never
    } while (cursor !== undefined)

    // The request memos are rebuilt too, so a retried requestId still conflicts after a rebuilt index.
    for (const request of requests) {
      await this.#db.run(
        `INSERT INTO droi_requests (conversation_id, request_id, fingerprint, submission_id)
         VALUES (?, ?, ?, ?)
         ON CONFLICT (conversation_id, request_id) DO UPDATE SET
           fingerprint = excluded.fingerprint,
           submission_id = COALESCE(droi_requests.submission_id, excluded.submission_id)`,
        id,
        request.requestId,
        request.fingerprint,
        request.submissionId ?? null,
      )
    }

    // A conversation with no row at all — an index rebuilt from scratch, or a database whose row was
    // lost — is rebuilt from its own `droi.meta` entry and its oldest entry's time.
    if (row === undefined) {
      const createdAt = meta?.createdAt ?? firstEntryAt ?? this.#now()
      this.#row(id, {
        insert: {
          createdAt,
          ...(meta?.kind === undefined ? {} : { kind: meta.kind }),
          ...(meta?.workspace === undefined ? {} : { workspace: meta.workspace }),
          ...(meta?.requestId === undefined ? {} : { requestId: meta.requestId }),
          ...(meta?.requestFingerprint === undefined
            ? {}
            : { requestFingerprint: meta.requestFingerprint }),
          ...(meta?.parentId !== undefined ? { parentId: meta.parentId } : {}),
          ...(parentId === undefined ? {} : { parentId }),
        },
        // A conversation that existed before this index did has no later event to point at, so its own
        // newest entry is the truth. Never `now`: a restart must not look like activity (F2).
        activity: newest ?? createdAt,
        ...(title === undefined ? {} : { title }),
        ...(preview === undefined ? {} : { preview }),
      })
      return count
    }
    if (count === 0 && newest === undefined) {
      // Nothing new: change nothing. Not the activity time, not the title (F2).
      return 0
    }
    const pending = this.#row(id, {})
    if (newest !== undefined) pending.activity = Math.max(pending.activity ?? 0, newest)
    if (row.title === null && title !== undefined) pending.title = title
    if (preview !== undefined) pending.preview = preview
    // A row written before the crash may lack what the meta entry records; fill it rather than trust a row.
    if (meta !== undefined) {
      pending.insert ??= { createdAt: meta.createdAt }
      if (row.kind === null && meta.kind !== undefined) pending.insert.kind = meta.kind
      if (row.workspace === null && meta.workspace !== undefined)
        pending.insert.workspace = meta.workspace
      if (row.request_id === null && meta.requestId !== undefined) {
        pending.insert.requestId = meta.requestId
        if (meta.requestFingerprint !== undefined) {
          pending.insert.requestFingerprint = meta.requestFingerprint
        }
      }
      if (row.parent_id === null && meta.parentId !== undefined)
        pending.insert.parentId = meta.parentId
    }
    return count
  }

  /**
   * A conversation exists and something happened to it now: a create, or a submission. Unlike recovery,
   * this is a real event, so it may move `last_activity_at`.
   */
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
    const pending = this.#row(id, {
      ...(extra.createdAt === undefined ? {} : { insert: { createdAt: extra.createdAt } }),
    })
    pending.eventAt = Math.max(pending.eventAt ?? 0, at)
    if (extra.title !== undefined) pending.title = extra.title
    if (extra.preview !== undefined) pending.preview = extra.preview
    if (extra.workspace !== undefined) {
      pending.insert ??= { createdAt: extra.createdAt ?? at }
      pending.insert.workspace = extra.workspace
    }
    if (extra.kind !== undefined) {
      pending.insert ??= { createdAt: extra.createdAt ?? at }
      pending.insert.kind = extra.kind
    }
    if (extra.parentId !== undefined) {
      pending.insert ??= { createdAt: extra.createdAt ?? at }
      pending.insert.parentId = extra.parentId
    }
    if (extra.requestId !== undefined) {
      pending.insert ??= { createdAt: extra.createdAt ?? at }
      pending.insert.requestId = extra.requestId
      if (extra.requestFingerprint !== undefined) {
        pending.insert.requestFingerprint = extra.requestFingerprint
      }
    }
    return this.#queue()
  }

  /** Remember an entry: its text for search, the conversation's name, and its last words as a preview. */
  entry(entry: EntryRecord): void {
    if (this.#closed) return
    const id = entry.conversationId as unknown as number
    const entryId = entry.id as unknown as number
    if (entryId <= this.watermark(id)) return
    const text = entryText(entry)
    if (text !== '') {
      this.#entries.push({ id: entryId, conversation: id, text })
    }
    const pending = this.#row(id, {})
    if (entry.kind === 'pi.user' && pending.title === undefined && text !== '') {
      pending.title = firstLine(text, TITLE_LIMIT)
    }
    const preview = previewText(entry)
    if (preview !== undefined) pending.preview = preview
    this.#queue()
  }

  /**
   * Follow stored commits: every entry that reached the database is queued for the index in the order Pi
   * stored it, and each conversation's watermark moves with the flush that stores it.
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
        this.#queue()
      },
    }
  }

  /** Queue a flush. A queue that cannot be stored is reported: dropping it silently loses indexing. */
  #queue(): Promise<void> {
    if (this.#flushIntervalMs <= 0) return this.flush()
    if (this.#timer !== undefined) return Promise.resolve()
    this.#timer = setTimeout(() => {
      this.#timer = undefined
      this.flush().catch((error: unknown) => this.#onError?.(error))
    }, this.#flushIntervalMs)
    this.#timer.unref?.()
    return Promise.resolve()
  }

  /** Store everything queued, in one transaction, with the watermarks it covers. */
  async flush(): Promise<void> {
    if (this.#flush !== undefined) {
      await this.#flush
      if (this.#entries.length === 0 && this.#rows.size === 0) return
    }
    if (this.#timer !== undefined) {
      clearTimeout(this.#timer)
      this.#timer = undefined
    }
    const entries = this.#entries
    const rows = [...this.#rows.values()]
    if (entries.length === 0 && rows.length === 0) return
    this.#entries = []
    this.#rows.clear()

    // The watermark a flush may write is the highest entry it stored **per conversation**, and only over
    // entries contiguous with what that conversation already had.
    const next = new Map<number, number>()
    for (const entry of entries) {
      next.set(entry.conversation, Math.max(next.get(entry.conversation) ?? 0, entry.id))
    }
    for (const [conversationId, entryId] of next) {
      next.set(conversationId, Math.max(this.watermark(conversationId), entryId))
    }
    const changed = rows.map((row) => row.id)
    const run = this.#db.transaction(async (tx) => {
      for (const row of rows) await writeRow(tx, row)
      for (const entry of entries) {
        // FTS5 has no UPSERT; REPLACE is its own conflict handling, and an entry ID is never reused.
        await tx.run(
          'INSERT OR REPLACE INTO droi_entries_fts (rowid, text, conversation) VALUES (?, ?, ?)',
          entry.id,
          entry.text,
          entry.conversation,
        )
      }
      for (const [conversationId, entryId] of next) {
        await tx.run(
          `INSERT INTO droi_index_watermarks (conversation_id, entry_id) VALUES (?, ?)
           ON CONFLICT (conversation_id) DO UPDATE SET entry_id = MAX(entry_id, excluded.entry_id)`,
          conversationId,
          entryId,
        )
      }
    })
    this.#flush = run.then(
      () => undefined,
      () => undefined,
    )
    try {
      await run
      for (const [conversationId, entryId] of next) {
        this.#watermarks.set(conversationId, entryId)
      }
      if (changed.length > 0) this.#onChange?.(changed)
    } finally {
      this.#flush = undefined
    }
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
  async request(
    conversationId: number,
    requestId: string,
  ): Promise<{ fingerprint: string; submissionId?: number } | undefined> {
    await this.flush()
    const row = await this.#db.get<{ fingerprint: string; submission_id: number | null }>(
      'SELECT fingerprint, submission_id FROM droi_requests WHERE conversation_id = ? AND request_id = ?',
      conversationId,
      requestId,
    )
    if (row === undefined) return undefined
    return {
      fingerprint: row.fingerprint,
      ...(row.submission_id === null ? {} : { submissionId: row.submission_id }),
    }
  }

  /**
   * Cache a submission's fingerprint. The transcript's `droi.request` entry is the record, written in the
   * commit that admits the submission; this row only saves a scan, and losing it costs nothing.
   */
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
        const { clauses, values } = likeClauses(words)
        where.push(
          `(id IN (SELECT conversation FROM droi_entries_fts WHERE ${clauses.join(' AND ')})
            OR (title || '') LIKE ? ESCAPE '\\')`,
        )
        params.push(...values)
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

  #row(id: number, seed: Partial<PendingRow>): PendingRow {
    const existing = this.#rows.get(id)
    if (existing !== undefined) {
      if (seed.insert !== undefined) existing.insert = { ...existing.insert, ...seed.insert }
      return existing
    }
    const row: PendingRow = { id, ...seed }
    this.#rows.set(id, row)
    return row
  }
}

const context = { signal: new AbortController().signal } as never

/**
 * Write one conversation's row. An `insert` creates it (or updates the fields that only exist at
 * creation); an `activity` moves `last_activity_at` forward and never back; a `title` fills an empty one
 * and never overwrites a name a person chose.
 */
async function writeRow(exec: SqliteExecutor, row: PendingRow): Promise<void> {
  if (row.insert !== undefined) {
    const at = row.eventAt ?? row.activity ?? row.insert.createdAt
    await exec.run(
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
      row.id,
      row.insert.createdAt,
      at,
      row.insert.workspace ?? null,
      row.insert.kind ?? null,
      row.title ?? null,
      row.preview ?? null,
      row.insert.parentId ?? null,
      row.insert.requestId ?? null,
      row.insert.requestFingerprint ?? null,
    )
    return
  }
  // An existing row: only its activity may move, and only forward.
  if (row.eventAt !== undefined || row.activity !== undefined) {
    await exec.run(
      `UPDATE droi_conversations SET last_activity_at = MAX(last_activity_at, ?),
         preview = COALESCE(?, preview)
       WHERE id = ?`,
      Math.max(row.eventAt ?? 0, row.activity ?? 0),
      row.preview ?? null,
      row.id,
    )
  } else if (row.preview !== undefined) {
    await exec.run('UPDATE droi_conversations SET preview = ? WHERE id = ?', row.preview, row.id)
  }
  if (row.title !== undefined) {
    await exec.run(
      'UPDATE droi_conversations SET title = ? WHERE id = ? AND (title IS NULL OR title = ?)',
      row.title,
      row.id,
      '',
    )
  }
}

/** Text an entry contributes to search: its messages, and the note a `droi.` entry carries. */
export function entryText(entry: EntryRecord): string {
  return messageTextOf(entry.model) + '\n' + dataText(entry.data)
}

/** When an entry was written, from its own message: the only time an entry can honestly report. */
function entryTimestamp(entry: EntryRecord): number {
  let newest = 0
  for (const message of entry.model ?? []) {
    const timestamp = (message as { timestamp?: unknown }).timestamp
    if (typeof timestamp === 'number') newest = Math.max(newest, timestamp)
  }
  return newest
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
  return text === '' ? undefined : firstLine(text, PREVIEW_LIMIT)
}

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
 * The SQL for a search: the words as LIKE patterns, and whether each needs the bounded scan.
 *
 * A word of three or more characters is answered by the trigram index (`text LIKE ?`, which the index
 * optimizes as `L0`). A shorter one cannot be, so its clause carries its own bound — the newest
 * `SHORT_TERM_WINDOW` rows — instead of reading the whole table.
 */
export function likeClauses(words: readonly string[]): {
  clauses: string[]
  values: (string | number)[]
} {
  const clauses: string[] = []
  const values: (string | number)[] = []
  for (const word of words) {
    if ([...word].length >= 3) {
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

export type { ConversationMeta }
