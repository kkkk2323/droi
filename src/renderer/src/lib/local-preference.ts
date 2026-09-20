// Client-side preferences that never reach the Daemon or the Desktop Shell.
// Stored per browser in localStorage, so a paired phone and the desktop
// window each keep their own.
import { useSyncExternalStore } from 'react'

export interface LocalPreference<T> {
  get(): T
  set(value: T): void
  use(): [T, (value: T) => void]
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

  return {
    get,
    set,
    use: () => [useSyncExternalStore(subscribe, get, () => fallback), set],
  }
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

export const sidebarVisible = createBooleanPreference('droi.sidebar', true)
export const showArchivedSessions = createBooleanPreference('droi.showArchived', false)
/** Model ids starred in the picker, in the order they were starred. */
export const favoriteModels = createStringListPreference('droi.favoriteModels')
