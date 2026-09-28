// Light is the default; the choice is per browser (localStorage), so a paired
// phone and the desktop window can differ.
import { useSyncExternalStore } from 'react'

export type Theme = 'light' | 'dark' | 'solarized-light'

export const THEMES: readonly Theme[] = ['light', 'dark', 'solarized-light']

export const THEME_LABELS: Record<Theme, string> = {
  light: 'Light',
  dark: 'Dark',
  'solarized-light': 'Solarized Light+',
}

/** Whether a theme is drawn light or dark: `.dark` and the `dark:` variants follow it. */
export function appearanceOf(theme: Theme): 'light' | 'dark' {
  return theme === 'dark' ? 'dark' : 'light'
}

const STORAGE_KEY = 'droi.theme'
const listeners = new Set<() => void>()

function isTheme(value: unknown): value is Theme {
  return THEMES.includes(value as Theme)
}

function read(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    return isTheme(stored) ? stored : 'light'
  } catch {
    return 'light'
  }
}

function apply(theme: Theme): void {
  const root = document.documentElement
  root.dataset['theme'] = theme
  root.classList.toggle('dark', appearanceOf(theme) === 'dark')
}

/** Applies the stored theme to <html>; call once before the first render. */
export function applyStoredTheme(): void {
  apply(read())
}

export function setTheme(theme: Theme): void {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // Private mode: the toggle still works for this page.
  }
  apply(theme)
  for (const listener of listeners) listener()
}

export function useTheme(): [Theme, (theme: Theme) => void] {
  const theme = useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => void listeners.delete(listener)
    },
    (): Theme => {
      const current = document.documentElement.dataset['theme']
      return isTheme(current) ? current : 'light'
    },
    (): Theme => 'light',
  )
  return [theme, setTheme]
}
