// Client-side preferences that never reach the Daemon or the Desktop Shell.
// Stored per browser in localStorage, so a paired phone and the desktop
// window each keep their own.
import { useSyncExternalStore } from 'react'

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
        const stored = localStorage.getItem(key)
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
      localStorage.setItem(key, codec.serialize(next))
    } catch {
      // Private mode: the choice still holds for this page.
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

function createStringListPreference(key: string): LocalPreference<string[]> {
  return createPreference<string[]>(key, [], {
    parse: (raw) => {
      const parsed: unknown = JSON.parse(raw)
      return Array.isArray(parsed) ? parsed.filter((v) => typeof v === 'string') : []
    },
    serialize: JSON.stringify,
  })
}

function createStringPreference(key: string): LocalPreference<string | null> {
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

export const sidebarVisible = createBooleanPreference('droi.sidebar', true)
export const showArchivedSessions = createBooleanPreference('droi.showArchived', false)
/** Model ids starred in the picker, in the order they were starred. */
export const favoriteModels = createStringListPreference('droi.favoriteModels')
/** The Session open when the Client was last used; opened again on launch. */
export const lastSessionId = createStringPreference('droi.lastSession')
/** Workspace groups the user folded away in the sidebar. */
export const foldedWorkspaces = createStringListPreference('droi.foldedWorkspaces')
/** Workspace groups and Sessions kept at the top of the sidebar. */
export const pinnedWorkspaces = createStringListPreference('droi.pinnedWorkspaces')
export const pinnedSessions = createStringListPreference('droi.pinnedSessions')
/** The app the header's "open in" button opens the Workspace in; the last one picked. */
export const openInApp = createStringPreference('droi.openInApp')
