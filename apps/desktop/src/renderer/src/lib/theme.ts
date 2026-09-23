// Light is the default; the choice is per browser (localStorage), so a paired
// phone and the desktop window can differ.
import { useSyncExternalStore } from 'react'

export type Theme = 'light' | 'dark'

const STORAGE_KEY = 'droi.theme'
const listeners = new Set<() => void>()

function read(): Theme {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'dark' ? 'dark' : 'light'
  } catch {
    return 'light'
  }
}

/** Applies the stored theme to <html>; call once before the first render. */
export function applyStoredTheme(): void {
  document.documentElement.classList.toggle('dark', read() === 'dark')
}

export function setTheme(theme: Theme): void {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // Private mode: the toggle still works for this page.
  }
  document.documentElement.classList.toggle('dark', theme === 'dark')
  for (const listener of listeners) listener()
}

export function useTheme(): [Theme, (theme: Theme) => void] {
  const theme = useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => void listeners.delete(listener)
    },
    (): Theme => (document.documentElement.classList.contains('dark') ? 'dark' : 'light'),
    (): Theme => 'light',
  )
  return [theme, setTheme]
}
