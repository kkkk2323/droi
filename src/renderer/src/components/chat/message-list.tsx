import { useMemo } from 'react'
import { Virtuoso } from 'react-virtuoso'
import type { FactoryDroidMessage } from '@factory/droid-sdk'
import { MessageEntry } from './message-entry'
import { buildTranscript, type TranscriptEntry } from './transcript'

interface ListContext {
  streamingMessageIds: ReadonlySet<string>
}

function renderEntry(_index: number, entry: TranscriptEntry, context: ListContext) {
  return <MessageEntry entry={entry} isStreaming={context.streamingMessageIds.has(entry.id)} />
}

/** Virtualised transcript that starts at, and follows, the latest message. */
export function MessageList({
  messages,
  streamingMessageIds,
}: {
  messages: readonly FactoryDroidMessage[]
  streamingMessageIds: ReadonlySet<string>
}) {
  const entries = useMemo(() => buildTranscript(messages), [messages])

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
      context={{ streamingMessageIds }}
      computeItemKey={(_, entry) => entry.id}
      initialTopMostItemIndex={entries.length - 1}
      followOutput="smooth"
      alignToBottom
      increaseViewportBy={{ top: 600, bottom: 600 }}
      itemContent={renderEntry}
    />
  )
}
