import type { SqliteExecutor } from '@earendil-works/chord/sqlite'

/**
 * Droi's own tables, in the same SQLite file as Pi's (`pi_` prefix), so a conversation's metadata
 * and the entries that move it commit against the same file and one backup covers both.
 *
 * `droi_entries_fts` is FTS5 with the **trigram** tokenizer, its rowid the Pi entry ID.
 *
 * The tokenizer choice is about Chinese. `unicode61` splits on spaces and punctuation, so a run of
 * Chinese characters is one token: searching 内存 in 这是内存里的摘要 would find nothing, because the
 * whole clause is a single token. Trigram indexes every three-character sequence instead, so a
 * substring search is a `LIKE '%…%'` the index can answer — for Chinese, Japanese and Korean as well
 * as for English, where it also matches inside a word (parseConfig for "seConfig").
 *
 * The cost: a pattern shorter than three characters cannot use the index and scans instead. That is
 * still correct (SQLite falls back), and a two-character Chinese word is common enough that returning
 * nothing would be worse than the scan.
 *
 * A row exists only for an entry that has text, so the table is a search index and not a copy of the
 * transcript.
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
    parent_id INTEGER,
    preview TEXT
  )`,
  `CREATE INDEX IF NOT EXISTS droi_index_last_activity
    ON droi_conversations (last_activity_at DESC)`,
  `CREATE INDEX IF NOT EXISTS droi_index_kind ON droi_conversations (kind, last_activity_at DESC)`,
  `CREATE INDEX IF NOT EXISTS droi_index_workspace
    ON droi_conversations (workspace, last_activity_at DESC)`,
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
  const recorded = await readMeta(db, 'index.tokenizer')
  if (recorded === TOKENIZER) return
  if (recorded !== undefined) {
    // An index from an older build cannot be searched the way this one builds queries: drop it and
    // let the backfill refill it, rather than keep a table whose results would be wrong.
    await db.exec('DROP TABLE IF EXISTS droi_entries_fts')
    await db.exec(
      `CREATE VIRTUAL TABLE droi_entries_fts USING fts5(
        text,
        conversation UNINDEXED,
        tokenize = '${TOKENIZER}'
      )`,
    )
    await db.run("DELETE FROM droi_meta WHERE key = 'index.backfilled'")
  }
  await writeMeta(db, 'index.tokenizer', TOKENIZER)
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
  parent_id: number | null
  preview: string | null
}
