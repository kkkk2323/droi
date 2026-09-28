// The `/compact` runs this Client started. The Daemon does not change a
// Session's working state for one (only an automatic compaction mid-turn
// reports `compacting_conversation`), so the sidebar's mark while it runs
// and the alert when it is done come from here, and only on the Client that
// asked. The log is one per Client; Session ids are unique across Daemons.
import { useEffect, useState, useSyncExternalStore } from 'react'

export interface CompactionDone {
  removedCount: number
  finishedAt: number
}

export interface Compactions {
  /** Sessions whose `/compact` is still running. */
  pending: ReadonlySet<string>
  /** The last `/compact` that finished for a Session, by the Session it left the user in. */
  finished: ReadonlyMap<string, CompactionDone>
}

/** `sessionId` is the Session that was compacted, `shownIn` where the user lands. */
type Listener = (done: { sessionId: string; shownIn: string } & CompactionDone) => void

export class CompactionLog {
  #pending = new Set<string>()
  #finished = new Map<string, CompactionDone>()
  #snapshot: Compactions = { pending: this.#pending, finished: this.#finished }
  #changed = new Set<() => void>()
  #done = new Set<Listener>()

  start(sessionId: string): void {
    this.#pending = new Set(this.#pending).add(sessionId)
    this.#publish()
  }

  /** `shownIn` is where the user lands: the Session itself, or the child of a handoff. */
  finish(sessionId: string, shownIn: string, removedCount: number, now = Date.now()): void {
    this.#forget(sessionId)
    const done = { removedCount, finishedAt: now }
    this.#finished = new Map(this.#finished).set(shownIn, done)
    this.#publish()
    for (const listener of this.#done) listener({ sessionId, shownIn, ...done })
  }

  fail(sessionId: string): void {
    this.#forget(sessionId)
    this.#publish()
  }

  #forget(sessionId: string): void {
    const pending = new Set(this.#pending)
    pending.delete(sessionId)
    this.#pending = pending
  }

  #publish(): void {
    this.#snapshot = { pending: this.#pending, finished: this.#finished }
    for (const listener of this.#changed) listener()
  }

  snapshot(): Compactions {
    return this.#snapshot
  }

  subscribe(listener: () => void): () => void {
    this.#changed.add(listener)
    return () => this.#changed.delete(listener)
  }

  /** Called once each time a `/compact` finishes. */
  onDone(listener: Listener): () => void {
    this.#done.add(listener)
    return () => this.#done.delete(listener)
  }
}

export const compactions = new CompactionLog()

export function useCompactions(log: CompactionLog = compactions): Compactions {
  return useSyncExternalStore(
    (listener) => log.subscribe(listener),
    () => log.snapshot(),
    () => log.snapshot(),
  )
}

/** How long the "compacted" notice stays after a `/compact` finishes. */
export const COMPACTED_NOTICE_MS = 8_000

/** The `/compact` that just finished in this Session, for the notice's few seconds. */
export function useCompactedNotice(sessionId: string): CompactionDone | null {
  const done = useCompactions().finished.get(sessionId) ?? null
  const [now, setNow] = useState(() => Date.now())
  const until = done ? done.finishedAt + COMPACTED_NOTICE_MS : 0
  useEffect(() => {
    const left = until - Date.now()
    if (left <= 0) return
    const timer = setTimeout(() => setNow(Date.now()), left)
    return () => clearTimeout(timer)
  }, [until])
  return done && until > now ? done : null
}
