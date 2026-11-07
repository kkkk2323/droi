import type { CommitPublication, EntryRecord, Storage } from '@earendil-works/pi-durable'
import type { Message } from '@earendil-works/pi-ai'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import { type ConversationRow, migrate, readMeta, writeMeta } from './schema.ts'

/**
 * Droi's conversation list, search index and view times, kept in the kernel's one SQLite file.
 *
 * Writes are queued and flushed together: on Windows a commit costs about 15.6 ms, so a commit per
 * entry would make a long run crawl. A flush is one transaction, at most `flushIntervalMs` after the
 * first queued write, and always before a read that would otherwise see a stale list.
 */
export type IndexerOptions = {
  /** Pause before a queued write is stored. 150 ms keeps a streaming answer to one flush per burst. */
  flushIntervalMs?: number
  now?: () => number
}

type PendingTouch = {
  at: number
  preview?: string
  title?: string
  workspace?: string
  kind?: string
}
type PendingEntry = { id: number; conversation: number; text: string }

const TITLE_LIMIT = 120
const PREVIEW_LIMIT = 200

export class Indexer {
  #db: BunSqliteDatabase
  #storage: Storage
  #flushIntervalMs: number
  #now: () => number
  #entries: PendingEntry[] = []
  #touches = new Map<number, PendingTouch>()
  #timer: ReturnType<typeof setTimeout> | undefined
  #flush: Promise<void> | undefined
  #closed = false

  constructor(db: BunSqliteDatabase, storage: Storage, options: IndexerOptions = {}) {
    this.#db = db
    this.#storage = storage
    this.#flushIntervalMs = options.flushIntervalMs ?? 150
    this.#now = options.now ?? Date.now
  }

  async open(): Promise<void> {
    await migrate(this.#db)
  }

  /**
   * Fill the list and the search index from what Pi already stored. Runs once per database (a flag in
   * `droi_meta`), so a restart with a big history does not re-read every entry.
   */
  async backfill(): Promise<void> {
    if ((await readMeta(this.#db, 'index.backfilled')) === '1') return
    let cursor: never | undefined
    do {
      const page = await this.#storage.scanConversations(
        { order: 'ascending' },
        100,
        cursor,
        context,
      )
      for (const record of page.items) {
        let title: string | undefined
        let preview: string | undefined
        let entryCursor: never | undefined
        const rows: PendingEntry[] = []
        do {
          const entries = await this.#storage.scanEntries(
            { conversationId: record.id, order: 'ascending' },
            500,
            entryCursor,
            context,
          )
          for (const entry of entries.items) {
            const text = entryText(entry)
            if (text === '') continue
            rows.push({ id: entry.id, conversation: record.id, text })
            if (title === undefined && entry.kind === 'pi.user')
              title = firstLine(text, TITLE_LIMIT)
            preview = firstLine(text, PREVIEW_LIMIT)
          }
          entryCursor = entries.next as never
        } while (entryCursor !== undefined)
        this.#entries.push(...rows)
        await this.touch(record.id, this.#now(), {
          ...(title === undefined ? {} : { title }),
          ...(preview === undefined ? {} : { preview }),
        })
        await this.flush()
      }
      cursor = page.next as never
    } while (cursor !== undefined)
    await this.flush()
    await writeMeta(this.#db, 'index.backfilled', '1')
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
    },
  ): Promise<void> {
    if (this.#closed) return Promise.resolve()
    if (extra.requestId !== undefined) {
      return this.#db.run(
        `INSERT INTO droi_conversations (id, created_at, last_activity_at, request_id, workspace, kind, parent_id)
         VALUES (?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT (id) DO UPDATE SET request_id = excluded.request_id`,
        id,
        at,
        at,
        extra.requestId,
        extra.workspace ?? null,
        extra.kind ?? null,
        extra.parentId ?? null,
      )
    }
    const pending = this.#touches.get(id) ?? { at }
    pending.at = Math.max(pending.at, at)
    if (extra.title !== undefined) pending.title = extra.title
    if (extra.preview !== undefined) pending.preview = extra.preview
    if (extra.workspace !== undefined) pending.workspace = extra.workspace
    if (extra.kind !== undefined) pending.kind = extra.kind
    this.#touches.set(id, pending)
    return this.#queue()
  }

  /**
   * Remember an entry: its text for search, the conversation's name from its first user message, and its
   * last words as the list's preview. Everything queued for the same conversation travels together, so the
   * row's first insert already carries the name.
   */
  entry(entry: EntryRecord): void {
    if (this.#closed) return
    const text = entryText(entry)
    if (text !== '') this.#entries.push({ id: entry.id, conversation: entry.conversationId, text })
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
   * Follow stored commits: every entry that reached the database is indexed, so the list and search
   * stay correct after a restart without a re-scan.
   */
  observeCommits() {
    return {
      advance: (publication: CommitPublication) => {
        for (const change of publication.changes) {
          if (change.type !== 'entry') continue
          this.entry(change.value)
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

  /** Store everything queued, in one transaction. A concurrent call joins the running flush. */
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
    const run = this.#db.transaction(async (tx) => {
      for (const [id, pending] of touches) {
        await tx.run(
          `INSERT INTO droi_conversations (id, created_at, last_activity_at, workspace, kind, title, preview)
           VALUES (?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT (id) DO UPDATE SET
             last_activity_at = MAX(droi_conversations.last_activity_at, excluded.last_activity_at),
             workspace = COALESCE(excluded.workspace, droi_conversations.workspace),
             kind = COALESCE(excluded.kind, droi_conversations.kind),
             title = COALESCE(droi_conversations.title, excluded.title),
             preview = COALESCE(excluded.preview, droi_conversations.preview)`,
          id,
          pending.at,
          pending.at,
          pending.workspace ?? null,
          pending.kind ?? null,
          pending.title ?? null,
          pending.preview ?? null,
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
    })
    this.#flush = run.then(
      () => undefined,
      () => undefined,
    )
    try {
      await run
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
    if (change.title !== undefined) {
      await this.#db.run('UPDATE droi_conversations SET title = ? WHERE id = ?', change.title, id)
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
    const row = await this.#db.get<ConversationRow>(
      'SELECT * FROM droi_conversations WHERE id = ?',
      id,
    )
    return row
  }

  async byRequestId(requestId: string): Promise<ConversationRow | undefined> {
    await this.flush()
    return this.#db.get<ConversationRow>(
      'SELECT * FROM droi_conversations WHERE request_id = ?',
      requestId,
    )
  }

  /**
   * The list, newest activity first. `query` searches the entry index and keeps the conversations
   * whose text matches, newest first.
   */
  async list(query: {
    kind?: string
    archived?: boolean
    workspace?: string
    text?: string
    cursor?: number
    limit?: number
  }): Promise<{ items: ConversationRow[]; next?: number }> {
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
      // Substring search through the trigram index: a word of three or more characters is answered by
      // the index, a shorter one scans, and the title is searched beside the transcript.
      const words = searchWords(query.text)
      if (words.length === 0) {
        // Nothing to look for, so nothing matches: a list of every conversation would be a wrong answer.
        where.push('0')
      } else {
        const clauses = words.map(() => "(text || '') LIKE ? ESCAPE '\\'")
        where.push(
          `(id IN (SELECT conversation FROM droi_entries_fts WHERE ${clauses.join(' AND ')})
            OR (title || '') LIKE ? ESCAPE '\\')`,
        )
        for (const word of words) params.push(`%${escapeLike(word)}%`)
        params.push(`%${escapeLike(query.text.trim())}%`)
      }
    }
    if (query.cursor !== undefined) {
      where.push('id < ?')
      params.push(query.cursor)
    }
    const sql = `SELECT * FROM droi_conversations
      ${where.length > 0 ? `WHERE ${where.join(' AND ')}` : ''}
      ORDER BY id DESC LIMIT ?`
    const rows = await this.#db.all<ConversationRow>(sql, ...params, limit + 1)
    const items = rows.slice(0, limit)
    const last = items.at(-1)
    return last !== undefined && rows.length > limit ? { items, next: last.id } : { items }
  }

  /** Which conversations have a row at all: what a scan of Pi's tables has to catch up with. */
  async missing(ids: readonly number[]): Promise<number[]> {
    if (ids.length === 0) return []
    const rows = await this.#db.all<{ id: number }>(
      `SELECT id FROM droi_conversations WHERE id IN (${ids.map(() => '?').join(',')})`,
      ...ids,
    )
    const have = new Set(rows.map((row) => row.id))
    return ids.filter((id) => !have.has(id))
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
  const parts: string[] = []
  for (const message of entry.model ?? []) parts.push(messageText(message))
  if (entry.data !== undefined && entry.data !== null) {
    if (typeof entry.data === 'string') parts.push(entry.data)
    else {
      const data = entry.data as Record<string, unknown>
      for (const key of ['text', 'message', 'title', 'question', 'command']) {
        const value = data[key]
        if (typeof value === 'string') parts.push(value)
      }
    }
  }
  return parts
    .filter((part) => part !== '')
    .join('\n')
    .trim()
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

/** The last text the assistant wrote, for the list's second line. */
function previewText(entry: EntryRecord): string | undefined {
  if (entry.kind !== 'pi.assistant' && entry.kind !== 'pi.user') return undefined
  const text = entryText(entry)
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
 * because those writing systems do not put spaces between words and splitting one run into single
 * characters would match far too much.
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
