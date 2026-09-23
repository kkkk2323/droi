import { useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import { ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Markdown } from './markdown'
import { ToolCluster } from './tool-activity'
import type { TranscriptBlock, TranscriptEntry } from './transcript'
import { COLUMN } from './column'

export function MessageEntry({
  entry,
  isStreaming,
  showTime,
}: {
  entry: TranscriptEntry
  isStreaming: boolean
  /** Only the entry that closes a turn carries a timestamp. */
  showTime: boolean
}) {
  if (entry.role === 'user') {
    const text = entry.blocks
      .filter((b): b is Extract<TranscriptBlock, { kind: 'text' }> => b.kind === 'text')
      .map((b) => b.text)
      .join('\n')
    const images = entry.blocks.filter(
      (b): b is Extract<TranscriptBlock, { kind: 'image' }> => b.kind === 'image',
    )
    return (
      <article aria-label="You" className={cn(COLUMN, 'flex flex-col items-end gap-1.5 py-3')}>
        {images.length > 0 ? (
          <div className="flex max-w-[85%] flex-wrap justify-end gap-1.5">
            {images.map((image) => (
              <img
                key={image.id}
                src={image.src}
                alt="Attached image"
                className="max-h-48 max-w-full rounded-xl border object-contain"
              />
            ))}
          </div>
        ) : null}
        {text ? (
          <div className="max-w-[85%] whitespace-pre-wrap wrap-anywhere rounded-2xl bg-secondary px-4 py-2.5 text-[15px] leading-6 text-secondary-foreground">
            {text}
          </div>
        ) : null}
      </article>
    )
  }

  return (
    <article
      aria-label="Assistant"
      className={cn(COLUMN, 'py-3', entry.isError && 'text-destructive-foreground')}
    >
      {entry.blocks.map((block) => {
        switch (block.kind) {
          case 'text':
            return <Markdown key={block.id} text={block.text} />
          case 'image':
            return (
              <img
                key={block.id}
                src={block.src}
                alt="Image from Droid"
                className="my-2 max-h-72 max-w-full rounded-xl border object-contain"
              />
            )
          case 'thinking':
            return (
              <ThinkingSection
                key={block.id}
                text={block.text}
                durationMs={block.durationMs}
                isStreaming={isStreaming}
              />
            )
          case 'tools':
            return <ToolCluster key={block.id} calls={block.calls} />
        }
      })}
      {/* The caret follows text only; after a tool cluster it read as a stray
          grey block, and the activity row below already says what is going on. */}
      {isStreaming && entry.blocks.at(-1)?.kind === 'text' ? (
        <span
          aria-label="Assistant is typing"
          className="ml-0.5 inline-block h-4 w-2 animate-pulse rounded-sm bg-foreground/60 align-middle"
        />
      ) : null}
      {showTime && entry.createdAt ? (
        <time
          dateTime={new Date(entry.createdAt).toISOString()}
          className="mt-2 block text-xs text-muted-foreground"
        >
          {formatTimestamp(entry.createdAt)}
        </time>
      ) : null}
    </article>
  )
}

function ThinkingSection({
  text,
  durationMs,
  isStreaming,
}: {
  text: string
  durationMs: number | undefined
  isStreaming: boolean
}) {
  const [open, setOpen] = useState(false)
  const label = durationMs
    ? `Reasoned for ${formatDuration(durationMs)}`
    : isStreaming
      ? 'Reasoning…'
      : 'Reasoning'
  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="my-2">
      <Collapsible.Trigger className="group -ml-1.5 flex items-center gap-1.5 rounded-md px-1.5 py-1 text-[13px] text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50">
        <span className={cn(isStreaming && !durationMs && 'animate-pulse')}>{label}</span>
        <ChevronRight
          aria-hidden
          className="size-3.5 transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
      </Collapsible.Trigger>
      <Collapsible.Panel className="mt-1 border-l-2 border-border pl-4 text-muted-foreground">
        <Markdown text={text} className="text-sm leading-6" />
      </Collapsible.Panel>
    </Collapsible.Root>
  )
}

function formatDuration(ms: number): string {
  if (ms < 1_000) return `${ms} ms`
  const seconds = Math.round(ms / 1_000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

function formatTimestamp(ms: number, now = new Date()): string {
  const date = new Date(ms)
  const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  const sameDay = date.toDateString() === now.toDateString()
  if (sameDay) return time
  return `${date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} ${time}`
}
