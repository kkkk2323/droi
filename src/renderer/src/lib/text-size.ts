// Text size scales the whole Client through the root font size, since every
// Tailwind size is in rem. Stored per browser like the theme.
import { textSize, type TextSize } from '@droi/daemon-layer/text-size'

const ROOT_PX: Record<TextSize, number> = {
  small: 14.5,
  default: 16,
  large: 17.5,
  largest: 19,
}

export function applyTextSize(size: TextSize = textSize.get()): void {
  document.documentElement.style.fontSize = `${ROOT_PX[size]}px`
}
