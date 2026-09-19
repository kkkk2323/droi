import { useMemo } from 'react'
import { Virtuoso } from 'react-virtuoso'
import type { FactoryDroidMessage } from '@factory/droid-sdk'
import { MessageEntry } from './message-entry'
import { buildTranscript, type TranscriptEntry } from './transcript'

interface ListContext {
  /** Id of the entry that is still being streamed by the Daemon, if any. */
  streamingEntryId: string | null
}

function renderEntry(_index: number, entry: TranscriptEntry, context: ListContext) {
  return <MessageEntry entry={entry} isStreaming={entry.id === context.streamingEntryId} />
}

/** Virtualised transcript that starts at, and follows, the latest message. */
export function MessageList({
  messages,
  isStreaming,
}: {
  messages: readonly FactoryDroidMessage[]
  /** True while the Daemon is producing assistant text; marks the last assistant entry. */
  isStreaming: boolean
}) {
  const entries = useMemo(() => buildTranscript(messages), [messages])
  const last = entries[entries.length - 1]
  const streamingEntryId = isStreaming && last?.role === 'assistant' ? last.id : null

  if (entries.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        No messages yet.
      </div>
    )
  }

  return (
    <Virtuoso<TranscriptEntry, ListContext>
      role="log"
      aria-label="Transcript"
      className="h-full"
      data={entries}
      context={{ streamingEntryId }}
      computeItemKey={(_, entry) => entry.id}
      initialTopMostItemIndex={entries.length - 1}
      followOutput="smooth"
      alignToBottom
      increaseViewportBy={{ top: 600, bottom: 600 }}
      itemContent={renderEntry}
    />
  )
}
