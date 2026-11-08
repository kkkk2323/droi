// Autonomy in the Clients' words (off, low, medium, high) and the Kernel's (ask, edits,
// safe, all), as the window maps them.
import type { Autonomy } from './kernel-wire'

const KERNEL_LEVELS: Record<string, Autonomy> = {
  off: 'ask',
  low: 'edits',
  medium: 'safe',
  high: 'all',
}

/** A Client's level in the Kernel's words; one it does not know is `ask`, the level that asks most. */
export function kernelAutonomy(level: string | null | undefined): Autonomy {
  return (level && KERNEL_LEVELS[level]) || 'ask'
}

/** A Kernel level in the Clients' words. */
export function clientAutonomy(level: string | null | undefined): string | null {
  if (!level) return null
  for (const [client, kernel] of Object.entries(KERNEL_LEVELS)) if (kernel === level) return client
  return null
}
