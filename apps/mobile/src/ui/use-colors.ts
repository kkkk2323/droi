import { createPreference, usePreference } from '@droi/daemon-layer/local-preference'
import { useSyncExternalStore } from 'react'
import { Appearance } from 'react-native'
import { COLORS, type ColorScheme, type Colors } from './theme'

export type ThemeChoice = 'system' | ColorScheme
export const THEME_CHOICES: ThemeChoice[] = ['system', 'light', 'dark']

/** The phone's theme: the system's light or dark appearance, or one picked in Settings. */
export const themeChoice = createPreference<ThemeChoice>('droi.phoneTheme', 'system', {
  parse: (raw) => (THEME_CHOICES.includes(raw as ThemeChoice) ? (raw as ThemeChoice) : 'system'),
  serialize: String,
})

/**
 * Hands the choice to iOS too, so the keyboard and system sheets match. The
 * web build has no such override; its colours follow the choice alone.
 */
export function applyThemeChoice(choice: ThemeChoice = themeChoice.get()): void {
  if (typeof Appearance.setColorScheme !== 'function') return
  Appearance.setColorScheme(choice === 'system' ? 'unspecified' : choice)
}

// Not react-native's useColorScheme: on the web it subscribes again on every
// render, so a change that re-renders the tree mid-dispatch reaches only the
// first component. A stable subscription reaches them all.
function subscribeToSystem(listener: () => void): () => void {
  const subscription = Appearance.addChangeListener(listener)
  return () => subscription.remove()
}

function systemScheme(): ColorScheme {
  return Appearance.getColorScheme() === 'dark' ? 'dark' : 'light'
}

export function useColorSchemeName(): ColorScheme {
  const [choice] = usePreference(themeChoice)
  const system = useSyncExternalStore(subscribeToSystem, systemScheme, systemScheme)
  return choice === 'system' ? system : choice
}

export function useColors(): Colors {
  return COLORS[useColorSchemeName()]
}
