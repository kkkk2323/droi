// Memory's source of truth: one SQLite database under the Desktop Shell's user
// data (ADR 0010). Every Session runs its own Memory Server process and the
// hooks open it too, so it runs in WAL mode with a busy timeout and every
// write is one short transaction.
import { DatabaseSync } from 'node:sqlite'
import { createHash, randomBytes } from 'node:crypto'
import { mkdirSync, realpathSync } from 'node:fs'
import { join } from 'node:path'
import { findSecret } from './secrets'

export const CATEGORIES = [
  'failure',
  'correction',
  'insight',
  'preference',
  'convention',
  'tool-quirk',
] as const
export type Category = (typeof CATEGORIES)[number]

export type Scope = 'project' | 'global'

export interface MemoryEntry {
  /** 8 lowercase hex characters. */
  id: string
  scope: Scope
  /** The Workspace path for Project Memory; null for Global Memory. */
  workspace: string | null
  category: Category
  /** ISO day the entry was recorded or last rewritten, YYYY-MM-DD. */
  day: string
  text: string
}

/** In characters of entry text. */
export const LIMITS: Record<Scope, { soft: number; hard: number }> = {
  project: { soft: 200_000, hard: 300_000 },
  global: { soft: 40_000, hard: 60_000 },
}

export const DATABASE_FILE = 'memory.sqlite'

/** One Project Memory or the Global Memory. */
export type MemorySlot = { scope: 'project'; workspace: string } | { scope: 'global' }

export interface SlotSummary {
  scope: Scope
  workspace: string | null
  entries: number
  chars: number
  /** ISO timestamp of the last consolidation; null before the first. */
  lastConsolidated: string | null
  overSoftLimit: boolean
}

export type WriteResult =
  | { ok: true; entry: MemoryEntry; overSoftLimit: boolean }
  | { ok: false; reason: string }

export interface SearchOptions {
  query: string
  slot: MemorySlot
  category?: Category
  limit?: number
}

/** A consolidation's answer for one slice: what becomes of each entry it was sent. */
export interface SliceChanges {
  keep: string[]
  rewrite: Array<{ id: string; text: string }>
  remove: string[]
  /** Several entries become one new entry with this text. */
  merge: Array<{ ids: string[]; text: string }>
}

export interface MemoryStore {
  readonly dir: string
  add(slot: MemorySlot, category: Category, text: string): WriteResult
  replace(id: string, text: string): WriteResult
  remove(id: string): boolean
  get(id: string): MemoryEntry | null
  list(slot: MemorySlot, category?: Category): MemoryEntry[]
  search(options: SearchOptions): MemoryEntry[]
  /** Correction entries of the slot, newest first, within both caps. */
  corrections(slot: MemorySlot, caps: { entries: number; chars: number }): MemoryEntry[]
  size(slot: MemorySlot): number
  /** Every Project Memory with entries, then the Global Memory. */
  summaries(): SlotSummary[]
  /**
   * Applies a consolidation to exactly the entries that were sent. Anything
   * naming an id outside `sent`, or leaving one of them unaccounted for,
   * changes nothing.
   */
  applySlice(
    sent: readonly MemoryEntry[],
    changes: SliceChanges,
  ): { ok: true } | { ok: false; reason: string }
  markConsolidated(slot: MemorySlot): void
  /** Notes that a Session wrote to Memory, so the fallback extraction leaves it alone. */
  recordWrite(sessionId: string): void
  hasWrite(sessionId: string): boolean
  close(): void
}

const today = () => new Date().toISOString().slice(0, 10)

/**
 * A Workspace's Project Memory. The Daemon may hand over the path a Session was
 * opened with while a process's cwd is resolved (/tmp against /private/tmp),
 * so both are keyed by the real path.
 */
export function projectSlot(workspace: string): { scope: 'project'; workspace: string } {
  let path = workspace
  try {
    path = realpathSync(workspace)
  } catch {
    // A Workspace that is gone keeps the path it was recorded under.
  }
  return { scope: 'project', workspace: path }
}

export function workspaceKey(path: string): string {
  return createHash('sha256').update(path).digest('hex').slice(0, 16)
}

const GLOBAL_KEY = 'global'

function slotKey(slot: MemorySlot): string {
  return slot.scope === 'global' ? GLOBAL_KEY : workspaceKey(slot.workspace)
}

export function isCategory(value: unknown): value is Category {
  return typeof value === 'string' && (CATEGORIES as readonly string[]).includes(value)
}

interface Row {
  id: string
  slot: string
  category: string
  day: string
  text: string
  workspace: string | null
}

export function openMemoryStore(dir: string): MemoryStore {
  mkdirSync(dir, { recursive: true })
  const db = new DatabaseSync(join(dir, DATABASE_FILE))
  db.exec(`
    PRAGMA busy_timeout = 5000;
    PRAGMA journal_mode = WAL;
    PRAGMA synchronous = NORMAL;
  `)
  db.exec(`
    CREATE TABLE IF NOT EXISTS slots (
      key TEXT PRIMARY KEY,
      workspace TEXT,
      last_consolidated TEXT
    );
    CREATE TABLE IF NOT EXISTS entries (
      rowid INTEGER PRIMARY KEY,
      id TEXT NOT NULL UNIQUE,
      slot TEXT NOT NULL REFERENCES slots(key),
      category TEXT NOT NULL,
      day TEXT NOT NULL,
      text TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS entries_slot ON entries(slot, category);
    CREATE TABLE IF NOT EXISTS session_writes (
      session_id TEXT PRIMARY KEY,
      at TEXT NOT NULL
    );
    CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts
      USING fts5(text, content='entries', content_rowid='rowid', tokenize='trigram');
    CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
      INSERT INTO entries_fts(rowid, text) VALUES (new.rowid, new.text);
    END;
    CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
      INSERT INTO entries_fts(entries_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
    END;
    CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE OF text ON entries BEGIN
      INSERT INTO entries_fts(entries_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
      INSERT INTO entries_fts(rowid, text) VALUES (new.rowid, new.text);
    END;
  `)

  const SELECT = `SELECT e.id, e.slot, e.category, e.day, e.text, s.workspace
    FROM entries e JOIN slots s ON s.key = e.slot`

  const toEntry = (row: Row): MemoryEntry => ({
    id: row.id,
    scope: row.slot === GLOBAL_KEY ? 'global' : 'project',
    workspace: row.slot === GLOBAL_KEY ? null : row.workspace,
    category: row.category as Category,
    day: row.day,
    text: row.text,
  })

  const rows = (sql: string, ...params: Array<string | number>) =>
    (db.prepare(sql).all(...params) as unknown as Row[]).map(toEntry)

  const transaction = <T>(run: () => T): T => {
    db.exec('BEGIN IMMEDIATE')
    try {
      const result = run()
      db.exec('COMMIT')
      return result
    } catch (error) {
      db.exec('ROLLBACK')
      throw error
    }
  }

  const ensureSlot = (slot: MemorySlot) => {
    db.prepare('INSERT OR IGNORE INTO slots (key, workspace) VALUES (?, ?)').run(
      slotKey(slot),
      slot.scope === 'global' ? null : slot.workspace,
    )
  }

  const sizeOf = (key: string): number =>
    Number(
      (
        db
          .prepare('SELECT COALESCE(SUM(LENGTH(text)), 0) AS n FROM entries WHERE slot = ?')
          .get(key) as {
          n: number
        }
      ).n,
    )

  const newId = (): string => {
    const exists = db.prepare('SELECT 1 FROM entries WHERE id = ?')
    for (;;) {
      const id = randomBytes(4).toString('hex')
      if (!exists.get(id)) return id
    }
  }

  const get = (id: string): MemoryEntry | null => rows(`${SELECT} WHERE e.id = ?`, id)[0] ?? null

  const slotOf = (entry: MemoryEntry): MemorySlot =>
    entry.scope === 'global'
      ? { scope: 'global' }
      : { scope: 'project', workspace: entry.workspace ?? '' }

  const checkText = (text: string): string | null => {
    if (!text.trim()) return 'The text is empty.'
    return findSecret(text)
  }

  const insert = (key: string, category: Category, text: string): string => {
    const id = newId()
    db.prepare('INSERT INTO entries (id, slot, category, day, text) VALUES (?, ?, ?, ?, ?)').run(
      id,
      key,
      category,
      today(),
      text,
    )
    return id
  }

  return {
    dir,
    add(slot, category, rawText) {
      const text = rawText.trim()
      const refused = checkText(text)
      if (refused) return { ok: false, reason: refused }
      const limits = LIMITS[slot.scope]
      return transaction((): WriteResult => {
        ensureSlot(slot)
        const key = slotKey(slot)
        const size = sizeOf(key)
        if (size + text.length > limits.hard) {
          const where = slot.scope === 'global' ? 'Global Memory' : 'This Workspace’s Memory'
          return {
            ok: false,
            reason: `${where} is full (${size} of ${limits.hard} characters). Nothing was saved. Ask the user to consolidate it in Droi under Settings → Memory, or replace or remove entries that are no longer true.`,
          }
        }
        const id = insert(key, category, text)
        return {
          ok: true,
          entry: get(id)!,
          overSoftLimit: size + text.length > limits.soft,
        }
      })
    },
    replace(id, rawText) {
      const text = rawText.trim()
      const refused = checkText(text)
      if (refused) return { ok: false, reason: refused }
      return transaction((): WriteResult => {
        const found = get(id)
        if (!found) return { ok: false, reason: `No Memory entry has the id ${id}.` }
        db.prepare('UPDATE entries SET text = ?, day = ? WHERE id = ?').run(text, today(), id)
        const slot = slotOf(found)
        return {
          ok: true,
          entry: get(id)!,
          overSoftLimit: sizeOf(slotKey(slot)) > LIMITS[slot.scope].soft,
        }
      })
    },
    remove(id) {
      return Number(db.prepare('DELETE FROM entries WHERE id = ?').run(id).changes) > 0
    },
    get,
    list(slot, category) {
      const key = slotKey(slot)
      return category
        ? rows(`${SELECT} WHERE e.slot = ? AND e.category = ? ORDER BY e.rowid`, key, category)
        : rows(`${SELECT} WHERE e.slot = ? ORDER BY e.category, e.rowid`, key)
    },
    search({ query, slot, category, limit = 10 }) {
      const key = slotKey(slot)
      const trimmed = query.trim()
      if (!trimmed) return []
      const byCategory = category ? ' AND e.category = ?' : ''
      const extra = category ? [category] : []
      const terms = trimmed.split(/\s+/).filter((term) => term.length >= 3)
      // The trigram tokenizer cannot match fewer than three characters; two-character
      // words are common in Chinese, so those fall back to a substring scan.
      if (terms.length === 0) {
        const words = trimmed.split(/\s+/)
        const like = words.map(() => "e.text LIKE ? ESCAPE '\\'").join(' OR ')
        return rows(
          `${SELECT} WHERE e.slot = ?${byCategory} AND (${like}) ORDER BY e.rowid DESC LIMIT ?`,
          key,
          ...extra,
          ...words.map((word) => `%${word.replace(/[\\%_]/g, (c) => `\\${c}`)}%`),
          limit,
        )
      }
      const match = terms.map((term) => `"${term.replace(/"/g, '""')}"`).join(' OR ')
      return rows(
        `${SELECT} JOIN entries_fts f ON f.rowid = e.rowid
          WHERE entries_fts MATCH ? AND e.slot = ?${byCategory}
          ORDER BY bm25(entries_fts) LIMIT ?`,
        match,
        key,
        ...extra,
        limit,
      )
    },
    corrections(slot, caps) {
      const found = rows(
        `${SELECT} WHERE e.slot = ? AND e.category = 'correction' ORDER BY e.day DESC, e.rowid DESC LIMIT ?`,
        slotKey(slot),
        caps.entries,
      )
      const kept: MemoryEntry[] = []
      let chars = 0
      for (const entry of found) {
        if (chars + entry.text.length > caps.chars) break
        chars += entry.text.length
        kept.push(entry)
      }
      return kept
    },
    size(slot) {
      return sizeOf(slotKey(slot))
    },
    summaries() {
      const found = db
        .prepare(
          `SELECT s.key, s.workspace, s.last_consolidated AS lastConsolidated,
             COUNT(e.id) AS entries, COALESCE(SUM(LENGTH(e.text)), 0) AS chars
           FROM slots s LEFT JOIN entries e ON e.slot = s.key
           GROUP BY s.key ORDER BY s.workspace`,
        )
        .all() as unknown as Array<{
        key: string
        workspace: string | null
        lastConsolidated: string | null
        entries: number
        chars: number
      }>
      const summary = (row: (typeof found)[number] | undefined, scope: Scope): SlotSummary => ({
        scope,
        workspace: scope === 'global' ? null : (row?.workspace ?? null),
        entries: Number(row?.entries ?? 0),
        chars: Number(row?.chars ?? 0),
        lastConsolidated: row?.lastConsolidated ?? null,
        overSoftLimit: Number(row?.chars ?? 0) > LIMITS[scope].soft,
      })
      return [
        ...found
          .filter((row) => row.key !== GLOBAL_KEY && Number(row.entries) > 0)
          .map((row) => summary(row, 'project')),
        summary(
          found.find((row) => row.key === GLOBAL_KEY),
          'global',
        ),
      ]
    },
    applySlice(sent, changes) {
      const sentIds = new Set(sent.map((entry) => entry.id))
      const named = [
        ...changes.keep,
        ...changes.rewrite.map((r) => r.id),
        ...changes.remove,
        ...changes.merge.flatMap((m) => m.ids),
      ]
      const stranger = named.find((id) => !sentIds.has(id))
      if (stranger)
        return { ok: false, reason: `The answer names ${stranger}, which was not sent.` }
      if (new Set(named).size !== named.length) {
        return { ok: false, reason: 'The answer names an entry more than once.' }
      }
      const missing = [...sentIds].find((id) => !named.includes(id))
      if (missing) return { ok: false, reason: `The answer leaves ${missing} unaccounted for.` }
      const texts = [...changes.rewrite.map((r) => r.text), ...changes.merge.map((m) => m.text)]
      for (const text of texts) {
        const refused = checkText(text)
        if (refused) return { ok: false, reason: refused }
      }
      const first = sent[0]
      if (!first) return { ok: true }
      const key = slotKey(slotOf(first))
      return transaction(() => {
        // Another Session may have changed the slice while the model worked on it.
        for (const entry of sent) {
          const current = get(entry.id)
          if (!current || current.text !== entry.text) {
            return {
              ok: false as const,
              reason: `${entry.id} changed while it was being consolidated.`,
            }
          }
        }
        const update = db.prepare('UPDATE entries SET text = ?, day = ? WHERE id = ?')
        for (const { id, text } of changes.rewrite) update.run(text.trim(), today(), id)
        const del = db.prepare('DELETE FROM entries WHERE id = ?')
        for (const id of changes.remove) del.run(id)
        for (const { ids, text } of changes.merge) {
          for (const id of ids) del.run(id)
          insert(key, first.category, text.trim())
        }
        return { ok: true as const }
      })
    },
    markConsolidated(slot) {
      ensureSlot(slot)
      db.prepare('UPDATE slots SET last_consolidated = ? WHERE key = ?').run(
        new Date().toISOString(),
        slotKey(slot),
      )
    },
    recordWrite(sessionId) {
      db.prepare('INSERT OR REPLACE INTO session_writes (session_id, at) VALUES (?, ?)').run(
        sessionId,
        new Date().toISOString(),
      )
    },
    hasWrite(sessionId) {
      return Boolean(db.prepare('SELECT 1 FROM session_writes WHERE session_id = ?').get(sessionId))
    },
    close() {
      db.close()
    },
  }
}
