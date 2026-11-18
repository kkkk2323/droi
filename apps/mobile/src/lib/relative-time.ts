/** How long ago, as the computer's sidebar says it: now, 5m, 3h, 2d, then the date. */
export function relativeTime(epochSeconds: number, now = Date.now()): string {
  const minutes = Math.floor(Math.max(0, now - epochSeconds * 1000) / 60_000)
  if (minutes < 1) return 'now'
  if (minutes < 60) return `${minutes}m`
  if (minutes < 24 * 60) return `${Math.floor(minutes / 60)}h`
  if (minutes < 30 * 24 * 60) return `${Math.floor(minutes / (24 * 60))}d`
  return new Date(epochSeconds * 1000).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
  })
}
