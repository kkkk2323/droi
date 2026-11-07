import type { SqliteExecutor } from '@earendil-works/chord/sqlite'

/** The shape of the index's own tables. A different value clears the caches and rebuilds them. */
export const INDEX_VERSION = '2'

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
 * word (parseConfig for "seConfig").
 *
 * `droi_index_watermarks` remembers, **per conversation**, the highest entry ID whose text is indexed. Per
 * conversation, not one number: a single number would have to advance past every conversation at once, and
 * a flush in the middle of a catch-up would then skip the conversations not yet read.
 *
 * The caches here — the rows, the search index, the watermarks and `droi_requests` — are all rebuildable
 * from the transcripts (`droi.meta`, `droi.request`). That is what makes `INDEX_VERSION` a safe reset.
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
  // Which conversation one import key already made, so a retried create converges instead of importing twice.
  //
  // The transcript's `droi.import` memo is the record for an append (it lives in the conversation it wrote);
  // a create has no conversation to search yet, so this table is what a retry reads. It survives an
  // `INDEX_VERSION` reset: an identity is not a cache, and forgetting it would import a second conversation.
  `CREATE TABLE IF NOT EXISTS droi_import_keys (
    key TEXT PRIMARY KEY,
    conversation_id INTEGER NOT NULL,
    fingerprint TEXT NOT NULL,
    entry_ids TEXT NOT NULL
  )`,
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

/** The tokenizer the index was built with; a database built another way is rebuilt. */
const TOKENIZER = 'trigram'

/**
 * Create whatever the database lacks, and reset the caches when this build's index differs from the one
 * that wrote them.
 *
 * The reset keeps what a person chose — a title, an archive flag, the time they last looked — and drops
 * what can be recomputed: the search rows, the watermarks, the `droi_requests` cache and the identity
 * columns, so a `requestId` fingerprint is recomputed in this build's format rather than compared against
 * an older one. M1's databases before this build are not a compatibility promise: the kernel has not
 * shipped, and the alternative is carrying a migration for a shape nobody has.
 */
export async function migrate(db: SqliteExecutor): Promise<void> {
  for (const sql of statements) await db.exec(sql)
  // A table this build no longer has. An earlier M1 recorded where a submission's turn ended; the contract
  // has no such field, and the rows are neither read nor rebuildable, so the table goes rather than linger.
  await db.exec('DROP TABLE IF EXISTS droi_submission_ends')
  const columns = await db.all<{ name: string }>('PRAGMA table_info(droi_conversations)')
  if (!columns.some((column) => column.name === 'request_fingerprint')) {
    await db.exec('ALTER TABLE droi_conversations ADD COLUMN request_fingerprint TEXT')
  }
  const version = await readMeta(db, 'index.version')
  const tokenizer = await readMeta(db, 'index.tokenizer')
  if (version === INDEX_VERSION && tokenizer === TOKENIZER) return

  // One connection and one transaction: either the whole reset lands or none of it does.
  const reset = async (tx: SqliteExecutor): Promise<void> => {
    await tx.exec('DELETE FROM droi_entries_fts')
    await tx.exec('DELETE FROM droi_index_watermarks')
    await tx.exec('DELETE FROM droi_requests')
    // An import key is rebuilt from the `droi.import` entries the catch-up then re-reads, so this cache goes
    // with the rest: nothing is lost that the transcript does not hold.
    await tx.exec('DELETE FROM droi_import_keys')
    await tx.exec('UPDATE droi_conversations SET request_id = NULL, request_fingerprint = NULL')
    await writeMeta(tx, 'index.version', INDEX_VERSION)
    await writeMeta(tx, 'index.tokenizer', TOKENIZER)
    // The M1 build before this one kept a single global watermark; it says nothing about the per-
    // conversation ones, so forget it.
    await tx.run("DELETE FROM droi_meta WHERE key = 'index.entryWatermark'")
    await tx.run("DELETE FROM droi_meta WHERE key = 'index.backfilled'")
  }
  // `migrate` is called with the pooled facade, which serialises the transaction; a bare executor has no
  // transactions, so the reset degrades to statements in order.
  const transactional = db as SqliteExecutor & {
    transaction?: (fn: (tx: SqliteExecutor) => Promise<void>) => Promise<void>
  }
  if (transactional.transaction === undefined) await reset(db)
  else await transactional.transaction(reset)
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
 * It is a `droi.meta` entry, appended in the commit that creates the conversation (or forks it), so the
 * answer survives anything that happens to `droi_conversations`: a crash before the index flush, a rebuilt
 * index, a requestId retried after the process died. The list row is the fast path; this is the record.
 *
 * A fork writes its own, with `parentId` and no `requestId`: its identity is its own, never its parent's.
 */
export type ConversationMeta = {
  requestId?: string
  requestFingerprint?: string
  kind?: string
  workspace?: string
  parentId?: number
  createdAt: number
}
