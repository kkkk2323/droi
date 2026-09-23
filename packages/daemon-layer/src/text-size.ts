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

export const textSize = createPreference<TextSize>('droi.textSize', 'default', {
  parse: (raw) => (TEXT_SIZES.includes(raw as TextSize) ? (raw as TextSize) : 'default'),
  serialize: String,
})
