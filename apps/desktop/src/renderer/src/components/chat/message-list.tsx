import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso'
import { ArrowDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { MessageEntry } from './message-entry'
import { turnEndIds, workingLabel, type TranscriptEntry } from '@droi/daemon-layer/transcript'
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
  /** Rendered above the first entry (a "continued from" link, say). */
  lead: ReactNode
  /** Entries that start a continued Session, below the one it continues. */
  boundaryIds: ReadonlySet<string>
}

// Virtuoso keeps the scroll position across a prepend when the first item's
// index goes down by the number of items added, so indices count down from here.
const INDEX_BASE = 1_000_000

/** How far above the bottom, in px, still counts as reading the latest output. */
const FOLLOW_THRESHOLD = 120

/** A height change this soon after a click or key press in the list is the reader's. */
const USER_RESIZE_WINDOW_MS = 500

const NO_EARLIER: ReadonlyArray<readonly TranscriptEntry[]> = []

function ListHeader({ context }: { context?: ListContext }) {
  return (
    <>
      <div aria-hidden className="h-3" />
      {context?.lead}
    </>
  )
}

function Boundary() {
  return (
    <div
      role="separator"
      aria-label="Context compacted here"
      className={cn(COLUMN, 'flex items-center gap-3 py-3 text-[11px] text-muted-foreground')}
    >
      <span className="h-px flex-1 bg-border" />
      Context compacted here; the conversation continues in a new session
      <span className="h-px flex-1 bg-border" />
    </div>
  )
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
    <>
      {context.boundaryIds.has(entry.id) ? <Boundary /> : null}
      <MessageEntry
        entry={entry}
        isStreaming={entry.id === context.streamingEntryId}
        showTime={context.turnEndIds.has(entry.id)}
      />
    </>
  )
}

function scrollToEnd(handle: VirtuosoHandle | null, behavior: 'auto' | 'smooth') {
  handle?.scrollToIndex({
    index: 'LAST',
    align: 'end',
    behavior: prefersReducedMotion() ? 'auto' : behavior,
  })
}

/** Virtualised transcript that starts at, and follows, the latest message. */
export function MessageList({
  transcript,
  earlier = NO_EARLIER,
  workingState,
  lead = null,
  scrollToEndKey = 0,
}: {
  transcript: readonly TranscriptEntry[]
  /** The Sessions this one continues after compactions, oldest first, shown above it. */
  earlier?: ReadonlyArray<readonly TranscriptEntry[]>
  /** The Daemon's working state for this Session. */
  workingState: string
  lead?: ReactNode
  /** Bumped when the user sends; the list then scrolls to the end whatever the position. */
  scrollToEndKey?: number
}) {
  const virtuoso = useRef<VirtuosoHandle>(null)
  const [atBottom, setAtBottom] = useState(true)
  // Virtuoso's followOutput fires on a count change only; a streaming reply
  // grows the last entry for seconds without one. Follow height changes too,
  // unless the reader has scrolled away (judged on their scroll events, so
  // content growing under a pinned viewport does not count as leaving).
  const scroller = useRef<HTMLElement | null>(null)
  const following = useRef(true)
  // Reading the geometry forces a layout; scroll events come several per
  // frame while Virtuoso is adding rows, so read once per frame.
  const scrollFrame = useRef<number | null>(null)
  const onScroll = useRef(() => {
    if (scrollFrame.current !== null) return
    scrollFrame.current = requestAnimationFrame(() => {
      scrollFrame.current = null
      const el = scroller.current
      if (el)
        following.current = el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_THRESHOLD
    })
  }).current
  const attachScroller = (el: HTMLElement | Window | null) => {
    scroller.current?.removeEventListener('scroll', onScroll)
    scroller.current = el instanceof HTMLElement ? el : null
    scroller.current?.addEventListener('scroll', onScroll, { passive: true })
  }
  // Opening a tool row or a reasoning block also changes the height. That is
  // the reader's own doing: the row stays where it was clicked instead of the
  // list jumping to the end, and following resumes only from the bottom.
  const lastTouched = useRef(0)
  const touched = () => {
    lastTouched.current = performance.now()
  }
  const onHeightChange = () => {
    if (!following.current) return
    // The list's DOM takes the new height on the next frame.
    requestAnimationFrame(() => {
      const el = scroller.current
      if (!el || !following.current) return
      if (performance.now() - lastTouched.current < USER_RESIZE_WINDOW_MS) {
        following.current = el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_THRESHOLD
        return
      }
      el.scrollTop = el.scrollHeight
    })
  }
  useEffect(() => {
    if (!scrollToEndKey) return
    // The sent message is appended a tick after the send; scroll once now and
    // once after it has landed.
    scrollToEnd(virtuoso.current, 'smooth')
    const timer = setTimeout(() => scrollToEnd(virtuoso.current, 'smooth'), 120)
    return () => clearTimeout(timer)
  }, [scrollToEndKey])
  // Opening a Session lands on its latest message. The initial index gets
  // there before entries have their real heights (markdown, images), so
  // re-pin once they have settled.
  useEffect(() => {
    const timer = setTimeout(() => scrollToEnd(virtuoso.current, 'auto'), 150)
    return () => clearTimeout(timer)
  }, [])
  const parts = [...earlier, transcript]
  const entries = parts.flat()
  const earlierCount = entries.length - (parts[parts.length - 1]?.length ?? 0)
  const last = entries[entries.length - 1]
  const running = workingState !== 'idle'
  const isStreaming = workingState === 'streaming_assistant_message'
  const streamingEntryId = isStreaming && last?.role === 'assistant' ? last.id : null
  const context: ListContext = {
    streamingEntryId,
    turnEndIds: turnEndIds(entries, running),
    activity: workingLabel(workingState),
    lead,
    boundaryIds: new Set(parts.slice(1).flatMap((part) => (part[0] ? [part[0].id] : []))),
  }

  if (entries.length === 0 && !lead) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        What should Droid work on?
      </div>
    )
  }

  return (
    <div className="relative h-full" onPointerDownCapture={touched} onKeyDownCapture={touched}>
      <Virtuoso<TranscriptEntry, ListContext>
        ref={virtuoso}
        role="log"
        aria-label="Transcript"
        className="h-full"
        data={entries}
        context={context}
        computeItemKey={(_, entry) => entry.id}
        firstItemIndex={INDEX_BASE - earlierCount}
        initialTopMostItemIndex={entries.length - 1}
        followOutput={prefersReducedMotion() ? 'auto' : 'smooth'}
        // The panels above the composer resize the viewport; a few pixels off
        // the bottom must still count as "following".
        atBottomThreshold={FOLLOW_THRESHOLD}
        atBottomStateChange={setAtBottom}
        scrollerRef={attachScroller}
        totalListHeightChanged={onHeightChange}
        components={{ Header: ListHeader, Footer: ActivityRow }}
        increaseViewportBy={{ top: 600, bottom: 600 }}
        itemContent={renderEntry}
      />
      {!atBottom ? (
        <Button
          size="icon-sm"
          variant="outline"
          aria-label="Scroll to latest"
          onClick={() => scrollToEnd(virtuoso.current, 'smooth')}
          className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-background shadow-composer"
        >
          <ArrowDown aria-hidden />
        </Button>
      ) : null}
    </div>
  )
}
