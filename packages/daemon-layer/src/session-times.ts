// The computer's Session Times, through its Gateway (ADR 0032): when the user last sent
// each Session a message, when its last turn ended and when a Client last showed it. They
// order the Session list (sessions.ts) and mark a Session unread on every Client alike:
// showing a Session here reads it on the computer too. A Gateway of an older Droi keeps
// none; the list then dates Sessions by the Daemon and each Client keeps its own marks.
import { useCallback, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import type { DaemonConnection } from './connection'
import {
  gatewaySessionTimesUrl,
  gatewaySessionViewedUrl,
  type SessionTimes,
  type SessionTimesSnapshot,
} from './gateway'

export type { SessionTimes }

export interface SessionTimesSource {
  /**
   * Every Session's Times for `after` 0, else what changed after that revision, once
   * something has or the Gateway's long poll has passed. Null from a Gateway that keeps none.
   */
  read(after: number, signal?: AbortSignal): Promise<SessionTimesSnapshot | null>
  /** Tells the computer that the Session is on screen. */
  viewed(sessionId: string): Promise<void>
}

export function createSessionTimes(
  config: { gatewayUrl: string; pairingToken: string | null },
  fetchImpl: typeof fetch,
): SessionTimesSource {
  const token = () => config.pairingToken ?? ''
  return {
    async read(after, signal) {
      const response = await fetchImpl(gatewaySessionTimesUrl(config.gatewayUrl, token(), after), {
        cache: 'no-store',
        ...(signal ? { signal } : {}),
      })
      if (response.status === 404) return null
      if (!response.ok) throw new Error(`The computer did not answer (HTTP ${response.status}).`)
      return parseSnapshot(await response.json())
    },
    async viewed(sessionId) {
      const response = await fetchImpl(
        gatewaySessionViewedUrl(config.gatewayUrl, token(), sessionId),
        { method: 'POST', cache: 'no-store' },
      )
      if (!response.ok && response.status !== 404) {
        throw new Error(`The computer did not note it (HTTP ${response.status}).`)
      }
    },
  }
}

const time = (value: unknown): number | undefined =>
  typeof value === 'number' && value > 0 ? value : undefined

function parseSnapshot(body: unknown): SessionTimesSnapshot {
  const b = (typeof body === 'object' && body !== null ? body : {}) as Record<string, unknown>
  const sessions: Record<string, SessionTimes> = {}
  const listed = b['sessions']
  if (typeof listed === 'object' && listed !== null) {
    for (const [id, raw] of Object.entries(listed as Record<string, unknown>)) {
      const t = (typeof raw === 'object' && raw !== null ? raw : {}) as Record<string, unknown>
      const times: SessionTimes = {}
      const sentAt = time(t['sentAt'])
      const endedAt = time(t['endedAt'])
      const viewedAt = time(t['viewedAt'])
      if (sentAt) times.sentAt = sentAt
      if (endedAt) times.endedAt = endedAt
      if (viewedAt) times.viewedAt = viewedAt
      sessions[id] = times
    }
  }
  return {
    rev: typeof b['rev'] === 'number' ? b['rev'] : 0,
    full: b['full'] === true,
    sessions,
  }
}

/** The Times with what a snapshot says: its own in place of a full one's, or its changes. */
export function applySnapshot(
  times: ReadonlyMap<string, SessionTimes>,
  snapshot: SessionTimesSnapshot,
): Map<string, SessionTimes> {
  const next = snapshot.full ? new Map<string, SessionTimes>() : new Map(times)
  for (const [id, t] of Object.entries(snapshot.sessions)) next.set(id, t)
  return next
}

/** A turn that ended after a Client last showed the Session. */
export function isUnread(times: SessionTimes | undefined): boolean {
  return (times?.endedAt ?? 0) > (times?.viewedAt ?? 0)
}

export interface SessionTimesState {
  times: ReadonlyMap<string, SessionTimes>
  /** The computer keeps them; false before its first answer and from an older Droi. */
  kept: boolean
}

const NO_TIMES: SessionTimesState = { times: new Map(), kept: false }

/** The wait before asking a computer that did not answer again. */
const RETRY_MS = 2_000

export interface TimesStore {
  subscribe(listener: () => void): () => void
  getSnapshot(): SessionTimesState
  viewed(sessionId: string): void
}

const stores = new WeakMap<DaemonConnection, TimesStore>()

function timesStore(connection: DaemonConnection): TimesStore {
  let store = stores.get(connection)
  if (!store) {
    store = createTimesStore(connection)
    stores.set(connection, store)
  }
  return store
}

/** What useSessionTimes reads, one per connection; exported for its tests. */
export function createTimesStore(
  connection: Pick<DaemonConnection, 'sessionTimes' | 'getState' | 'subscribe'>,
): TimesStore {
  const listeners = new Set<() => void>()
  let times: ReadonlyMap<string, SessionTimes> = new Map()
  let rev = 0
  let kept = false
  /**
   * The Sessions shown here, at when by this phone's clock, ahead of the computer's answer
   * that has them. Once the computer has noted one (noted), its next answer with that
   * Session replaces the guess: the two clocks need not agree, and a turn that ends just
   * after must still read as unread.
   */
  const shown = new Map<string, number>()
  const noted = new Set<string>()
  let snapshot: SessionTimesState | null = null
  let loop: AbortController | null = null
  let detach: (() => void) | null = null

  const changed = () => {
    snapshot = null
    for (const listener of listeners) listener()
  }
  const follow = async (signal: AbortSignal) => {
    while (!signal.aborted) {
      try {
        const answer = await connection.sessionTimes.read(rev, signal)
        if (signal.aborted) return
        if (answer === null) {
          if (kept) {
            kept = false
            changed()
          }
          return
        }
        const news = answer.full === true || Object.keys(answer.sessions).length > 0
        if (news) times = applySnapshot(times, answer)
        rev = answer.rev
        for (const id of noted) {
          if (answer.full === true || id in answer.sessions) {
            noted.delete(id)
            shown.delete(id)
          }
        }
        if (news || !kept) {
          kept = true
          changed()
        }
      } catch {
        if (signal.aborted) return
        await new Promise((resolve) => setTimeout(resolve, RETRY_MS))
      }
    }
  }
  // Follow while anyone listens and the connection is up; a socket that comes back asks
  // again from the revision it had, and a computer that restarted answers in full.
  const sync = () => {
    const want = listeners.size > 0 && connection.getState().status === 'connected'
    if (want && !loop) {
      loop = new AbortController()
      void follow(loop.signal)
    } else if (!want && loop) {
      loop.abort()
      loop = null
    }
  }

  return {
    subscribe(listener) {
      listeners.add(listener)
      if (!detach) detach = connection.subscribe(sync)
      sync()
      return () => {
        listeners.delete(listener)
        if (listeners.size === 0) {
          detach?.()
          detach = null
        }
        sync()
      }
    },
    getSnapshot() {
      if (snapshot) return snapshot
      if (!kept) {
        snapshot = NO_TIMES
        return snapshot
      }
      if (shown.size === 0) {
        snapshot = { times, kept }
        return snapshot
      }
      const merged = new Map(times)
      for (const [id, at] of shown) {
        const t = merged.get(id) ?? {}
        if (at > (t.viewedAt ?? 0)) merged.set(id, { ...t, viewedAt: at })
      }
      snapshot = { times: merged, kept }
      return snapshot
    },
    viewed(sessionId) {
      const at = Date.now()
      shown.set(sessionId, at)
      noted.delete(sessionId)
      changed()
      void connection.sessionTimes.viewed(sessionId).then(
        () => {
          if (shown.get(sessionId) === at) noted.add(sessionId)
        },
        () => {},
      )
    },
  }
}

/** The connected computer's Session Times, and how to tell it a Session is on screen. */
export function useSessionTimes(): SessionTimesState & { viewed: (sessionId: string) => void } {
  const connection = useDaemonConnection()
  const store = timesStore(connection)
  const subscribe = useCallback((listener: () => void) => store.subscribe(listener), [store])
  const getSnapshot = useCallback(() => store.getSnapshot(), [store])
  const state = useSyncExternalStore(subscribe, getSnapshot, () => NO_TIMES)
  return { ...state, viewed: store.viewed }
}
