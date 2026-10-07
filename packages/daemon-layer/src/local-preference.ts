// Client-side preferences that never reach the Daemon or the Desktop Shell.
// Stored per device (localStorage in a browser, app storage on the phone), so
// a paired phone and the desktop window each keep their own.
import { useSyncExternalStore } from 'react'
import { DEFAULT_WORKTREE_LIFECYCLE, isWorktreeLifecycle, type WorktreeLifecycle } from './worktree'

/** Synchronous key-value storage the Client supplies; a subset of the Web Storage API. */
export interface PreferenceStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

function memoryStorage(): PreferenceStorage {
  const values = new Map<string, string>()
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => void values.set(key, value),
    removeItem: (key) => void values.delete(key),
  }
}

let storage: PreferenceStorage = memoryStorage()

/**
 * Where preferences and drafts are kept. Call before anything reads one:
 * a preference reads its stored value once, on first use.
 */
export function setPreferenceStorage(next: PreferenceStorage): void {
  storage = next
}

export function preferenceStorage(): PreferenceStorage {
  return storage
}

export interface LocalPreference<T> {
  get(): T
  set(value: T): void
  subscribe(listener: () => void): () => void
  readonly fallback: T
}

export function createPreference<T>(
  key: string,
  fallback: T,
  codec: { parse: (raw: string) => T; serialize: (value: T) => string },
): LocalPreference<T> {
  const listeners = new Set<() => void>()
  let value: { current: T } | null = null

  const get = (): T => {
    if (value === null) {
      try {
        const stored = storage.getItem(key)
        value = { current: stored === null ? fallback : codec.parse(stored) }
      } catch {
        value = { current: fallback }
      }
    }
    return value.current
  }

  const set = (next: T): void => {
    value = { current: next }
    try {
      storage.setItem(key, codec.serialize(next))
    } catch {
      // Storage unavailable (private mode): the choice still holds for this run.
    }
    for (const listener of listeners) listener()
  }

  const subscribe = (listener: () => void) => {
    listeners.add(listener)
    return () => void listeners.delete(listener)
  }

  return { get, set, subscribe, fallback }
}

/** React binding; a real hook so the React Compiler sees it as one. */
export function usePreference<T>(preference: LocalPreference<T>): [T, (value: T) => void] {
  const value = useSyncExternalStore(
    preference.subscribe,
    preference.get,
    () => preference.fallback,
  )
  return [value, preference.set]
}

export function createBooleanPreference(key: string, fallback: boolean): LocalPreference<boolean> {
  return createPreference(key, fallback, { parse: (raw) => raw === 'true', serialize: String })
}

export function createStringListPreference(key: string): LocalPreference<string[]> {
  return createPreference<string[]>(key, [], {
    parse: (raw) => {
      const parsed: unknown = JSON.parse(raw)
      return Array.isArray(parsed) ? parsed.filter((v) => typeof v === 'string') : []
    },
    serialize: JSON.stringify,
  })
}

export function createStringPreference(key: string): LocalPreference<string | null> {
  return createPreference<string | null>(key, null, {
    parse: (raw) => raw || null,
    serialize: (value) => value ?? '',
  })
}

/** Toggle one entry of a list preference. */
export function toggleListed(preference: LocalPreference<string[]>, entry: string): void {
  const current = preference.get()
  preference.set(current.includes(entry) ? current.filter((e) => e !== entry) : [...current, entry])
}

/** Whether the New session page starts in a fresh Git worktree (see worktree.ts). */
export const worktreeByDefault = createBooleanPreference('droi.worktreeByDefault', false)
/** The lifecycle the last worktree was made with. */
export const worktreeLifecyclePreference = createPreference<WorktreeLifecycle>(
  'droi.worktreeLifecycle',
  DEFAULT_WORKTREE_LIFECYCLE,
  {
    parse: (raw) => (isWorktreeLifecycle(raw) ? raw : DEFAULT_WORKTREE_LIFECYCLE),
    serialize: String,
  },
)

export const showArchivedSessions = createBooleanPreference('droi.showArchived', false)
/** Model ids starred in the picker, in the order they were starred. */
export const favoriteModels = createStringListPreference('droi.favoriteModels')
/** The Session open when the Client was last used; opened again on launch. */
export const lastSessionId = createStringPreference('droi.lastSession')
/** Workspace groups the user folded away in the Session list. */
export const foldedWorkspaces = createStringListPreference('droi.foldedWorkspaces')
/** Workspace groups and Sessions kept at the top of the Session list. */
export const pinnedWorkspaces = createStringListPreference('droi.pinnedWorkspaces')
export const pinnedSessions = createStringListPreference('droi.pinnedSessions')
/** How the Session list orders Workspaces and the Sessions in them (see sessions.ts). */
export const workspaceSort = createStringPreference('droi.workspaceSort')
export const sessionSort = createStringPreference('droi.sessionSort')
/** Workspace keys in the order the user dragged them into. */
export const manualWorkspaceOrder = createStringListPreference('droi.workspaceOrder')
/** When this Client first saw each Session, in ms; the Daemon lists no creation time. */
export const sessionsFirstSeen = createPreference<Record<string, number>>(
  'droi.sessionsFirstSeen',
  {},
  {
    parse: (raw) => {
      const parsed: unknown = JSON.parse(raw)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {}
      return Object.fromEntries(
        Object.entries(parsed).filter(
          (entry): entry is [string, number] => typeof entry[1] === 'number',
        ),
      )
    },
    serialize: JSON.stringify,
  },
)
