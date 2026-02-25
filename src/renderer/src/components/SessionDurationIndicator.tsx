import { useMessages } from '@/store'
import { Clock } from 'lucide-react'

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.round(seconds % 60)
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`
  const hours = Math.floor(minutes / 60)
  const remainingMinutes = minutes % 60
  return `${hours}h ${remainingMinutes}m`
}

export function SessionDurationIndicator({ className }: { className?: string }) {
  const messages = useMessages()

  let total = 0
  for (const msg of messages) {
    if (msg.endTimestamp && msg.timestamp > 0) {
      total += msg.endTimestamp - msg.timestamp
    }
  }

  if (total === 0) return null

  return (
    <span
      className={`flex items-center gap-1 text-[11px] tabular-nums text-muted-foreground ${className || ''}`}
      title="Total session duration"
    >
      <Clock className="size-3" />
      <span>{formatDuration(total)}</span>
    </span>
  )
}
