// Client-side preferences that never reach the Daemon or the Desktop Shell.
// Stored per browser in localStorage, so a paired phone and the desktop
// window each keep their own.
import { useSyncExternalStore } from 'react'

export interface LocalPreference<T> {
  get(): T
  set(value: T): void
  use(): [T, (value: T) => void]
}

export function createBooleanPreference(key: string, fallback: boolean): LocalPreference<boolean> {
  const listeners = new Set<() => void>()
  let value: boolean | null = null

  const get = (): boolean => {
    if (value === null) {
      try {
        const stored = localStorage.getItem(key)
        value = stored === null ? fallback : stored === 'true'
      } catch {
        value = fallback
      }
    }
    return value
  }

  const set = (next: boolean): void => {
    value = next
    try {
      localStorage.setItem(key, String(next))
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

export const sidebarVisible = createBooleanPreference('droi.sidebar', true)
export const showArchivedSessions = createBooleanPreference('droi.showArchived', false)
