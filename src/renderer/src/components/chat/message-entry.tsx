import { useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import { Brain, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Markdown } from './markdown'
import { ToolActivity } from './tool-activity'
import type { TranscriptBlock, TranscriptEntry } from './transcript'

export function MessageEntry({
  entry,
  isStreaming,
}: {
  entry: TranscriptEntry
  isStreaming: boolean
}) {
  if (entry.role === 'user') {
    const text = entry.blocks
      .filter((b): b is Extract<TranscriptBlock, { kind: 'text' }> => b.kind === 'text')
      .map((b) => b.text)
      .join('\n')
    return (
      <article aria-label="You" className="flex justify-end px-4 py-2">
        <div className="max-w-[80%] whitespace-pre-wrap rounded-2xl rounded-br-md bg-secondary px-3.5 py-2 text-sm text-secondary-foreground">
          {text}
        </div>
      </article>
    )
  }

  return (
    <article
      aria-label="Assistant"
      className={cn('px-4 py-2', entry.isError && 'text-destructive-foreground')}
    >
      {entry.blocks.map((block) => {
        switch (block.kind) {
          case 'text':
            return <Markdown key={block.id} text={block.text} />
          case 'thinking':
            return (
              <ThinkingSection
                key={block.id}
                text={block.text}
                durationMs={block.durationMs}
                isStreaming={isStreaming}
              />
            )
          case 'tool':
            return <ToolActivity key={block.id} call={block.call} />
        }
      })}
      {isStreaming ? (
        <span
          aria-label="Assistant is typing"
          className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-foreground/70 align-middle"
        />
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
    <Collapsible.Root open={open} onOpenChange={setOpen} className="my-1.5">
      <Collapsible.Trigger className="group flex items-center gap-2 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50">
        <ChevronRight
          aria-hidden
          className="size-3 transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
        <Brain aria-hidden className="size-3.5" />
        <span>{label}</span>
      </Collapsible.Trigger>
      <Collapsible.Panel className="ml-[1.35rem] mt-1 border-l-2 border-border pl-3 text-xs text-muted-foreground">
        <Markdown text={text} className="text-xs" />
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
