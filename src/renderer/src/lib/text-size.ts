// Text size scales the whole Client through the root font size, since every
// Tailwind size is in rem. Stored per browser like the theme.
import { createPreference } from './local-preference'

export const TEXT_SIZES = ['small', 'default', 'large', 'largest'] as const
export type TextSize = (typeof TEXT_SIZES)[number]

export const TEXT_SIZE_LABELS: Record<TextSize, string> = {
  small: 'Small',
  default: 'Default',
  large: 'Large',
  largest: 'Largest',
}

const ROOT_PX: Record<TextSize, number> = {
  small: 14.5,
  default: 16,
  large: 17.5,
  largest: 19,
}

export const textSize = createPreference<TextSize>('droi.textSize', 'default', {
  parse: (raw) => (TEXT_SIZES.includes(raw as TextSize) ? (raw as TextSize) : 'default'),
  serialize: String,
})

export function applyTextSize(size: TextSize = textSize.get()): void {
  document.documentElement.style.fontSize = `${ROOT_PX[size]}px`
}
