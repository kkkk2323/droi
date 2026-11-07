import type { SqliteExecutor } from '@earendil-works/chord/sqlite'

/**
 * Droi's own tables, in the same SQLite file as Pi's (`pi_` prefix), so a conversation's metadata and
 * the entries that move it commit against the same file and one backup covers both.
 *
 * `droi_entries_fts` is FTS5 with the **trigram** tokenizer, its rowid the Pi entry ID.
 *
 * The tokenizer choice is about Chinese. `unicode61` splits on spaces and punctuation, so a run of Chinese
 * characters is one token: searching 内存 in 这是内存里的摘要 would find nothing, because the whole clause is
 * a single token. Trigram indexes every three-character sequence instead, so `text LIKE '%…%'` is answered
 * by the index — for Chinese, Japanese and Korean as well as for English, where it also matches inside a
 * word (parseConfig for "seConfig"). A pattern shorter than three characters cannot use the index and
 * scans the vtable; that is still correct, and the scan is bounded below.
 *
 * `droi_index_watermarks` remembers, **per conversation**, the highest entry ID whose text is indexed. Per
 * conversation, not one number: a single number would have to advance past every conversation at once, and
 * a flush in the middle of a catch-up would then skip the conversations not yet read.
 */
const statements = [
  `CREATE TABLE IF NOT EXISTS droi_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
  )`,
  `CREATE TABLE IF NOT EXISTS droi_conversations (
    id INTEGER PRIMARY KEY,
    title TEXT,
    kind TEXT,
    workspace TEXT,
    archived_at INTEGER,
    created_at INTEGER NOT NULL,
    last_activity_at INTEGER NOT NULL,
    viewed_at INTEGER,
    request_id TEXT UNIQUE,
    request_fingerprint TEXT,
    parent_id INTEGER,
    preview TEXT
  )`,
  `CREATE INDEX IF NOT EXISTS droi_index_last_activity
    ON droi_conversations (last_activity_at DESC, id DESC)`,
  `CREATE INDEX IF NOT EXISTS droi_index_kind
    ON droi_conversations (kind, last_activity_at DESC, id DESC)`,
  `CREATE INDEX IF NOT EXISTS droi_index_workspace
    ON droi_conversations (workspace, last_activity_at DESC, id DESC)`,
  `CREATE TABLE IF NOT EXISTS droi_requests (
    conversation_id INTEGER NOT NULL,
    request_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    submission_id INTEGER,
    PRIMARY KEY (conversation_id, request_id)
  )`,
  `CREATE TABLE IF NOT EXISTS droi_index_watermarks (
    conversation_id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL
  )`,
  `CREATE VIRTUAL TABLE IF NOT EXISTS droi_entries_fts USING fts5(
    text,
    conversation UNINDEXED,
    tokenize = 'trigram'
  )`,
]

/** The tokenizer the index was built with; a database built with another one is rebuilt. */
const TOKENIZER = 'trigram'

/** Create whatever the database lacks. Idempotent: it runs on every start. */
export async function migrate(db: SqliteExecutor): Promise<void> {
  for (const sql of statements) await db.exec(sql)
  // A database from an earlier M1 build lacks the fingerprint column.
  const columns = await db.all<{ name: string }>('PRAGMA table_info(droi_conversations)')
  if (!columns.some((column) => column.name === 'request_fingerprint')) {
    await db.exec('ALTER TABLE droi_conversations ADD COLUMN request_fingerprint TEXT')
  }
  const recorded = await readMeta(db, 'index.tokenizer')
  if (recorded !== TOKENIZER) {
    if (recorded !== undefined) {
      // An index from an older build cannot be searched the way this one builds queries: drop it and
      // re-read everything, rather than keep a table whose results would be wrong.
      await db.exec('DROP TABLE IF EXISTS droi_entries_fts')
      await db.exec(
        `CREATE VIRTUAL TABLE droi_entries_fts USING fts5(
          text,
          conversation UNINDEXED,
          tokenize = '${TOKENIZER}'
        )`,
      )
      await db.exec('DELETE FROM droi_index_watermarks')
    }
    await writeMeta(db, 'index.tokenizer', TOKENIZER)
  }
  // The global watermark of the first M1 build is not a per-conversation one; forget it, so every
  // conversation is re-read once. Writing the same rows twice changes nothing.
  const legacy = await readMeta(db, 'index.entryWatermark')
  if (legacy !== undefined) {
    await db.exec('DELETE FROM droi_index_watermarks')
    await db.run("DELETE FROM droi_meta WHERE key = 'index.entryWatermark'")
  }
}

export async function readMeta(db: SqliteExecutor, key: string): Promise<string | undefined> {
  const row = await db.get<{ value: string }>('SELECT value FROM droi_meta WHERE key = ?', key)
  return row?.value
}

export async function writeMeta(db: SqliteExecutor, key: string, value: string): Promise<void> {
  await db.run(
    `INSERT INTO droi_meta (key, value) VALUES (?, ?)
     ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
    key,
    value,
  )
}

/** A conversation as the list shows it, before the kernel turns it into the wire shape. */
export type ConversationRow = {
  id: number
  title: string | null
  kind: string | null
  workspace: string | null
  archived_at: number | null
  created_at: number
  last_activity_at: number
  viewed_at: number | null
  request_id: string | null
  request_fingerprint: string | null
  parent_id: number | null
  preview: string | null
}

/**
 * Why a conversation exists, as the kernel records it in the conversation's own transcript.
 *
 * It is a `droi.meta` entry, appended in the commit that creates the conversation, so the answer survives
 * anything that happens to `droi_conversations`: a crash before the index flush, a rebuilt index, a
 * requestId retried after the process died. The list row is the fast path; this is the record.
 */
export type ConversationMeta = {
  requestId?: string
  requestFingerprint?: string
  kind?: string
  workspace?: string
  parentId?: number
  createdAt: number
}
