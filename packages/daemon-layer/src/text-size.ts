// The Client's text size, kept per device like the theme.
import { createPreference } from './local-preference'

export const TEXT_SIZES = ['small', 'default', 'large', 'largest'] as const
export type TextSize = (typeof TEXT_SIZES)[number]

export const TEXT_SIZE_LABELS: Record<TextSize, string> = {
  small: 'Small',
  default: 'Default',
  large: 'Large',
  largest: 'Largest',
}

/** How much each size scales the Client's text, relative to the default. */
export const TEXT_SCALE: Record<TextSize, number> = {
  small: 0.90625,
  default: 1,
  large: 1.09375,
  largest: 1.1875,
}

export const textSize = createPreference<TextSize>('droi.textSize', 'default', {
  parse: (raw) => (TEXT_SIZES.includes(raw as TextSize) ? (raw as TextSize) : 'default'),
  serialize: String,
})
