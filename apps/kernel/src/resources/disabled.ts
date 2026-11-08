import type { BunSqliteDatabase } from '../storage/facade.ts'

/**
 * The skills a person turned off, by name.
 *
 * A setting, not a cache: the table is not in the index's reset, and nothing rebuilds it. It lives in the
 * kernel rather than the Host so every client reads one list (`skills.list`), and the prompt and the slash
 * expansion read the same one without asking anybody.
 */
export class DisabledSkills {
  readonly #db: BunSqliteDatabase
  #names: Set<string>

  private constructor(db: BunSqliteDatabase, names: Set<string>) {
    this.#db = db
    this.#names = names
  }

  /** Read the table, which `migrate` has created. */
  static async open(db: BunSqliteDatabase): Promise<DisabledSkills> {
    const rows = await db.all<{ name: string }>('SELECT name FROM droi_disabled_skills')
    return new DisabledSkills(db, new Set(rows.map((row) => row.name)))
  }

  names(): ReadonlySet<string> {
    return this.#names
  }

  async set(name: string, disabled: boolean, at = Date.now()): Promise<void> {
    if (disabled) {
      await this.#db.run(
        'INSERT INTO droi_disabled_skills (name, at) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET at = excluded.at',
        name,
        at,
      )
    } else {
      await this.#db.run('DELETE FROM droi_disabled_skills WHERE name = ?', name)
    }
    const next = new Set(this.#names)
    if (disabled) next.add(name)
    else next.delete(name)
    this.#names = next
  }
}
