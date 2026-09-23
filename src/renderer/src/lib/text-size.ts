// Text size scales the whole Client through the root font size, since every
// Tailwind size is in rem. Stored per browser like the theme.
import { TEXT_SCALE, textSize, type TextSize } from '@droi/daemon-layer/text-size'

export function applyTextSize(size: TextSize = textSize.get()): void {
  document.documentElement.style.fontSize = `${16 * TEXT_SCALE[size]}px`
}
