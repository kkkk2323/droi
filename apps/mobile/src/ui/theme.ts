// The web Client's design tokens (apps/desktop/src/renderer/src/styles/global.css),
// translated for React Native: the oklch colours as sRGB hex, the rem radii
// as points, and Geist as the per-weight families expo-font registers.

export type ColorScheme = 'light' | 'dark'

const light = {
  background: '#ffffff',
  foreground: '#161616',
  card: '#f0f0f0',
  popover: '#ffffff',
  primary: '#161616',
  primaryForeground: '#fafafa',
  secondary: '#ebebeb',
  muted: '#f0f0f0',
  mutedForeground: '#636363',
  accent: '#e6e6e6',
  destructive: '#e7000b',
  destructiveForeground: '#c20000',
  border: '#dedede',
  input: '#d7d7d7',
  ring: '#808080',
  sidebar: '#f0f0f0',
  sidebarForeground: '#2e2e2e',
  sidebarAccent: '#dfdfdf',
  sidebarBorder: '#dedede',
  code: '#af2938',
  codeBackground: '#f0f0f0',
  // Status only, as the web Client uses them: emerald-600, amber-600, sky-600
  // for Working, and sky-500 for the unread dot.
  success: '#009966',
  attention: '#dd7400',
  working: '#0084d1',
  unread: '#00a6f4',
  backdrop: 'rgba(0, 0, 0, 0.3)',
}

export type Colors = typeof light

const dark: Colors = {
  background: '#161616',
  foreground: '#eeeeee',
  card: '#222222',
  popover: '#1d1d1d',
  primary: '#eeeeee',
  primaryForeground: '#161616',
  secondary: '#292929',
  muted: '#222222',
  mutedForeground: '#989898',
  accent: '#2e2e2e',
  destructive: '#82181a',
  destructiveForeground: '#ff645f',
  border: '#2e2e2e',
  input: '#333333',
  ring: '#636363',
  sidebar: '#0e0e0e',
  sidebarForeground: '#bebebe',
  sidebarAccent: '#242424',
  sidebarBorder: '#242424',
  code: '#fb9797',
  codeBackground: '#262626',
  // Tailwind's emerald-400, amber-400, sky-400 and sky-500.
  success: '#00d492',
  attention: '#ffb900',
  working: '#00bcff',
  unread: '#00a6f4',
  backdrop: 'rgba(0, 0, 0, 0.5)',
}

export const COLORS: Record<ColorScheme, Colors> = { light, dark }

/** --radius is 0.625rem at the web Client's 16px root. */
export const radius = { sm: 6, md: 8, lg: 10, xl: 14, full: 999 } as const

export const space = { xs: 4, sm: 8, md: 12, lg: 16, xl: 24 } as const

export const fonts = {
  sans: 'Geist_400Regular',
  sansMedium: 'Geist_500Medium',
  sansSemiBold: 'Geist_600SemiBold',
  mono: 'GeistMono_400Regular',
  monoMedium: 'GeistMono_500Medium',
} as const

/** The web Client's html font-size is 14px; its text-sm and text-xs follow. */
export const fontSize = { xs: 12, sm: 13, base: 15, lg: 17, xl: 20 } as const
