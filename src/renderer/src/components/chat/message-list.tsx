import { Virtuoso } from 'react-virtuoso'
import type { FactoryDroidMessage } from '@factory/droid-sdk'
import { MessageEntry } from './message-entry'
import { buildTranscript, type TranscriptEntry } from './transcript'
import { COLUMN } from './column'
import { prefersReducedMotion } from '@/lib/use-media-query'
import { cn } from '@/lib/utils'

interface ListContext {
  /** Id of the entry that is still being streamed by the Daemon, if any. */
  streamingEntryId: string | null
  /** Ids of the entries that close a turn; they carry the timestamp. */
  turnEndIds: ReadonlySet<string>
  /** What the Daemon is doing; shown under the last entry while not idle. */
  activity: string
}

const WORKING_LABELS: Record<string, string> = {
  idle: '',
  thinking: 'Thinking',
  streaming_assistant_message: 'Responding',
  waiting_for_tool_confirmation: 'Waiting for your approval',
  executing_tool: 'Running a tool',
  compacting_conversation: 'Compacting',
}

function ListPadding() {
  return <div aria-hidden className="h-3" />
}

/** The live turn's closing row, in the transcript where the reply will land. */
function ActivityRow({ context }: { context?: ListContext }) {
  const label = context?.activity ?? ''
  return (
    <div
      role="status"
      aria-label="Session activity"
      className={cn(COLUMN, 'flex items-center gap-2 pb-4 text-[13px] text-muted-foreground')}
    >
      {label ? (
        <>
          <span aria-hidden className="flex items-center gap-0.5">
            <span className="size-1 animate-bounce rounded-full bg-current [animation-delay:-0.3s]" />
            <span className="size-1 animate-bounce rounded-full bg-current [animation-delay:-0.15s]" />
            <span className="size-1 animate-bounce rounded-full bg-current" />
          </span>
          {label}
        </>
      ) : null}
    </div>
  )
}

function renderEntry(_index: number, entry: TranscriptEntry, context: ListContext) {
  return (
    <MessageEntry
      entry={entry}
      isStreaming={entry.id === context.streamingEntryId}
      showTime={context.turnEndIds.has(entry.id)}
    />
  )
}

/** Assistant entries followed by a user turn, plus the last one once the Daemon rests. */
export function turnEndIds(entries: readonly TranscriptEntry[], running: boolean): Set<string> {
  const ids = new Set<string>()
  entries.forEach((entry, index) => {
    if (entry.role !== 'assistant') return
    const next = entries[index + 1]
    if (next ? next.role === 'user' : !running) ids.add(entry.id)
  })
  return ids
}

/** Virtualised transcript that starts at, and follows, the latest message. */
export function MessageList({
  messages,
  workingState,
}: {
  messages: readonly FactoryDroidMessage[]
  /** The Daemon's working state for this Session. */
  workingState: string
}) {
  const entries = buildTranscript(messages)
  const last = entries[entries.length - 1]
  const running = workingState !== 'idle'
  const isStreaming = workingState === 'streaming_assistant_message'
  const streamingEntryId = isStreaming && last?.role === 'assistant' ? last.id : null
  const context: ListContext = {
    streamingEntryId,
    turnEndIds: turnEndIds(entries, running),
    activity: WORKING_LABELS[workingState] ?? workingState,
  }

  if (entries.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        What should Droid work on?
      </div>
    )
  }

  return (
    <Virtuoso<TranscriptEntry, ListContext>
      role="log"
      aria-label="Transcript"
      className="h-full"
      data={entries}
      context={context}
      computeItemKey={(_, entry) => entry.id}
      initialTopMostItemIndex={entries.length - 1}
      followOutput={prefersReducedMotion() ? 'auto' : 'smooth'}
      components={{ Header: ListPadding, Footer: ActivityRow }}
      increaseViewportBy={{ top: 600, bottom: 600 }}
      itemContent={renderEntry}
    />
  )
}
