// Shared Synara workbench materials: a white conversation beside a gray
// sidebar in light mode, charcoal surfaces in dark mode, the system UI
// face, and JetBrains Mono for code.

export type ColorScheme = 'light' | 'dark'

export type ThemeName = 'light' | 'dark' | 'solarized-light'

/** Whether a theme is drawn light or dark: the status bar and iOS follow it. */
export function schemeOf(theme: ThemeName): ColorScheme {
  return theme === 'dark' ? 'dark' : 'light'
}

const light = {
  background: '#ffffff',
  foreground: '#171717',
  card: '#ffffff',
  popover: '#ffffff',
  primary: '#171717',
  primaryForeground: '#ffffff',
  secondary: 'rgba(0, 0, 0, 0.03)',
  muted: 'rgba(0, 0, 0, 0.04)',
  mutedForeground: '#636363',
  accent: 'rgba(0, 0, 0, 0.04)',
  destructive: '#ef4444',
  destructiveForeground: '#c81e1e',
  border: 'rgba(0, 0, 0, 0.12)',
  input: 'rgba(0, 0, 0, 0.15)',
  ring: '#a3a3a3',
  sidebar: '#ebebeb',
  sidebarForeground: '#171717',
  sidebarAccent: 'rgba(0, 0, 0, 0.045)',
  sidebarBorder: 'rgba(0, 0, 0, 0.1)',
  codeBackground: 'rgba(0, 0, 0, 0.055)',
  // Inline code. React Native cannot pad or round a span inside text, so a
  // fill would be a bare box hugging the glyphs; mono type in a warm ink
  // reads as code without a pill.
  codeInline: '#9a5528',
  // Status only, the reference's own tints: emerald, amber and blue.
  success: '#10b981',
  attention: '#f59e0b',
  working: '#3b82f6',
  unread: '#0169cc',
  // Diff lines: the reference's added and removed green and red at 12%.
  addedBackground: 'rgba(0, 162, 64, 0.12)',
  removedBackground: 'rgba(224, 46, 42, 0.12)',
  backdrop: 'rgba(0, 0, 0, 0.3)',
}

export type Colors = typeof light

const dark: Colors = {
  background: '#18191b',
  foreground: '#f5f5f5',
  card: '#202124',
  popover: '#202124',
  primary: '#f5f5f5',
  primaryForeground: '#171717',
  secondary: 'rgba(255, 255, 255, 0.05)',
  muted: 'rgba(255, 255, 255, 0.05)',
  mutedForeground: '#9a9a9a',
  accent: 'rgba(255, 255, 255, 0.05)',
  destructive: '#f05454',
  destructiveForeground: '#f87575',
  border: 'rgba(255, 255, 255, 0.12)',
  input: 'rgba(255, 255, 255, 0.15)',
  ring: '#6b6b6b',
  sidebar: '#202124',
  sidebarForeground: '#f5f5f5',
  sidebarAccent: 'rgba(255, 255, 255, 0.06)',
  sidebarBorder: 'rgba(255, 255, 255, 0.1)',
  codeBackground: 'rgba(255, 255, 255, 0.06)',
  codeInline: '#e0a882',
  success: '#34d399',
  attention: '#fbbf24',
  working: '#60a5fa',
  unread: '#339cff',
  addedBackground: 'rgba(64, 201, 119, 0.14)',
  removedBackground: 'rgba(250, 66, 62, 0.14)',
  backdrop: 'rgba(0, 0, 0, 0.5)',
}

// Solarized Light+ in the same material: the page a step brighter than its
// panels, hairlines as low-alpha ink, the palette's blue as the tint.
const solarizedLight: Colors = {
  background: '#fbf7ea',
  foreground: '#4b5c62',
  card: '#f4efe2',
  popover: '#fdfaf1',
  primary: '#5c6f76',
  primaryForeground: '#fdfaf1',
  secondary: 'rgba(88, 110, 117, 0.06)',
  muted: 'rgba(88, 110, 117, 0.07)',
  mutedForeground: '#5b6e76',
  accent: 'rgba(88, 110, 117, 0.08)',
  destructive: '#dc322f',
  destructiveForeground: '#b32d2a',
  border: 'rgba(88, 110, 117, 0.14)',
  input: 'rgba(88, 110, 117, 0.18)',
  ring: '#b3a99b',
  sidebar: '#f7f2e5',
  sidebarForeground: '#44555c',
  sidebarAccent: 'rgba(88, 110, 117, 0.09)',
  sidebarBorder: 'rgba(88, 110, 117, 0.14)',
  codeBackground: 'rgba(88, 110, 117, 0.08)',
  codeInline: '#1f7f78',
  success: '#859900',
  attention: '#b58900',
  working: '#268bd2',
  unread: '#268bd2',
  addedBackground: 'rgba(133, 153, 0, 0.12)',
  removedBackground: 'rgba(220, 50, 47, 0.12)',
  backdrop: 'rgba(0, 0, 0, 0.3)',
}

export const COLORS: Record<ThemeName, Colors> = {
  light,
  dark,
  'solarized-light': solarizedLight,
}

/** The reference's --radius is a 10px basis: lg 10, md 8, sm 6. */
export const radius = { sm: 6, md: 8, lg: 10, xl: 14, full: 999 } as const

export const space = { xs: 4, sm: 8, md: 12, lg: 16, xl: 24 } as const

/**
 * The system's own interface face (SF Pro on iOS) and JetBrains Mono for
 * code, as the reference app uses them: one family object for every weight,
 * since the system font selects its weight from `fontWeight`.
 */
export const fonts = {
  sans: 'System',
  mono: 'JetBrainsMono_400Regular',
  monoMedium: 'JetBrainsMono_500Medium',
} as const

/** Phone type scale: 15px body, as before. Synara's 13px is desktop. */
export const fontSize = { xs: 12, sm: 13, base: 15, lg: 17, xl: 20 } as const
