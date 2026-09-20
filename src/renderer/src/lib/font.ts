// The UI font: the vendored Geist pair, or the platform's own (San Francisco
// on Apple devices). Stored per browser like the theme; the CSS switches the
// font variables on the root's data-font attribute.
import { createPreference } from './local-preference'

export const FONTS = ['geist', 'system'] as const
export type Font = (typeof FONTS)[number]

export const FONT_LABELS: Record<Font, string> = {
  geist: 'Geist',
  system: 'System',
}

export function parseFont(raw: string): Font {
  return FONTS.includes(raw as Font) ? (raw as Font) : 'geist'
}

export const font = createPreference<Font>('droi.font', 'geist', {
  parse: parseFont,
  serialize: String,
})

export function applyFont(choice: Font = font.get()): void {
  if (choice === 'geist') delete document.documentElement.dataset.font
  else document.documentElement.dataset.font = choice
}
