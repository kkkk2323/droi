import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { Virtuoso, type StateSnapshot, type VirtuosoHandle } from 'react-virtuoso'
import { ArrowDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { MessageEntry } from './message-entry'
import { activeItem, railItems, TurnRail, type RailItem } from './turn-rail'
import {
  turnEnds,
  workingLabel,
  type TranscriptEntry,
  type TurnEnd,
} from '@droi/daemon-layer/transcript'
import { COLUMN } from './column'
import { prefersReducedMotion } from '@/lib/use-media-query'
import { cn } from '@/lib/utils'

interface ListContext {
  /** Id of the entry that is still being streamed by the Daemon, if any. */
  streamingEntryId: string | null
  /** The entries that close a turn, by id; they carry the time and how long it took. */
  turnEnds: ReadonlyMap<string, TurnEnd>
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

/** How long a freshly opened list may take to land at the end before it is shown anyway. */
const SETTLE_TIMEOUT_MS = 400

/** How long "Scroll to latest" keeps going after rows below grow on the way. */
const SEEK_TIMEOUT_MS = 3000

/** How many frames a jump waits for the rows it loaded to reach the list. */
const LAND_FRAMES = 30

const NO_EARLIER: ReadonlyArray<readonly TranscriptEntry[]> = []
const NO_ENTRIES: readonly TranscriptEntry[] = []

/**
 * Where each list was left, by `stateKey`, for as long as the Client runs.
 * A list left at the end keeps its measured row heights: opened without them
 * it draws its rows at estimated heights first and then jumps. A list left
 * part way up keeps the row at the top of the viewport instead: rows above it
 * that were never measured sit at estimated heights, which differ from one
 * mount to the next, so a pixel offset would land somewhere else.
 */
type SavedList =
  | { atBottom: true; state: StateSnapshot; scrollTop: number }
  | { atBottom: false; anchorId: string; offset: number }
const savedLists = new Map<string, SavedList>()

/** The first row showing at the top of the scroller, and how far it is scrolled past. */
function topRow(scroller: HTMLElement): { index: number; offset: number } | null {
  const top = scroller.getBoundingClientRect().top
  for (const row of scroller.querySelectorAll<HTMLElement>('[data-index]')) {
    const rect = row.getBoundingClientRect()
    if (rect.bottom > top) return { index: Number(row.dataset['index']), offset: top - rect.top }
  }
  return null
}

/** How far down the viewport the reader's eye is taken to be, as a fraction of its height. */
const READING_LINE = 0.25

/** The row under the reading line. */
function readingRow(scroller: HTMLElement): number | null {
  const rect = scroller.getBoundingClientRect()
  const line = rect.top + rect.height * READING_LINE
  let found: number | null = null
  for (const row of scroller.querySelectorAll<HTMLElement>('[data-index]')) {
    if (row.getBoundingClientRect().top > line) break
    found = Number(row.dataset['index'])
  }
  return found
}

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
        turnEnd={context.turnEnds.get(entry.id) ?? null}
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
  stateKey,
  olderUserMessages = NO_ENTRIES,
  loadUntil,
}: {
  transcript: readonly TranscriptEntry[]
  /** The Sessions this one continues after compactions, oldest first, shown above it. */
  earlier?: ReadonlyArray<readonly TranscriptEntry[]>
  /** The Daemon's working state for this Session. */
  workingState: string
  lead?: ReactNode
  /** Bumped when the user sends; the list then scrolls to the end whatever the position. */
  scrollToEndKey?: number
  /**
   * Remembers the list under this key when it unmounts. Coming back, it opens
   * where the reader left it; one left at the end opens at the (new) end.
   */
  stateKey?: string
  /** The user's messages from before the loaded ones of this Session, oldest first. */
  olderUserMessages?: readonly TranscriptEntry[]
  /** Loads this Session's earlier messages up to one of them; says whether it is loaded now. */
  loadUntil?: (messageId: string) => Promise<boolean>
}) {
  const virtuoso = useRef<VirtuosoHandle>(null)
  const parts = [...earlier, transcript]
  const entries = parts.flat()
  const earlierCount = entries.length - (parts[parts.length - 1]?.length ?? 0)
  // Rows arrive at the front from earlier Sessions and from older pages of this
  // one; both are counted from the row that was first when the list opened.
  // A row that is gone (rewound, say) makes the current first row the mark.
  const [firstOpened, setFirstOpened] = useState<string | null>(transcript[0]?.id ?? null)
  let prepended = firstOpened === null ? -1 : entries.findIndex((e) => e.id === firstOpened)
  if (prepended < 0) {
    const next = transcript[0]?.id ?? null
    if (next !== firstOpened) setFirstOpened(next)
    prepended = earlierCount
  }
  const latestEntries = useRef(entries)
  useLayoutEffect(() => {
    latestEntries.current = entries
  })
  const [restored] = useState(() => {
    const saved = stateKey ? savedLists.get(stateKey) : undefined
    if (!saved || saved.atBottom) return saved
    const index = entries.findIndex((entry) => entry.id === saved.anchorId)
    // The row is gone (rewound, say): open at the end.
    return index < 0 ? undefined : { ...saved, index }
  })
  const restoredMidway = restored !== undefined && !restored.atBottom
  const [atBottom, setAtBottom] = useState(!restoredMidway)
  const rail = useMemo(
    () =>
      railItems(
        [...earlier, transcript].flat(),
        olderUserMessages,
        earlier.reduce((count, part) => count + part.length, 0),
      ),
    [earlier, transcript, olderUserMessages],
  )
  const [readRow, setReadRow] = useState<number | null>(null)
  // A message from before the loaded ones the reader jumped to; its rows are on their way.
  const [jumpingTo, setJumpingTo] = useState<string | null>(null)
  // Virtuoso's followOutput fires on a count change only; a streaming reply
  // grows the last entry for seconds without one. Follow height changes too,
  // unless the reader has scrolled away (judged on their scroll events, so
  // content growing under a pinned viewport does not count as leaving).
  const scroller = useRef<HTMLElement | null>(null)
  const following = useRef(!restoredMidway)
  // Virtuoso puts a list reopened part way up back in place over several
  // frames, and on a slow machine passes the estimated end on the way. That is
  // not the reader arriving there; such a list follows again only after the
  // reader has scrolled it.
  const restoring = useRef(restoredMidway)
  // Reading the geometry forces a layout; scroll events come several per
  // frame while Virtuoso is adding rows, so read once per frame.
  const scrollFrame = useRef<number | null>(null)
  const onScroll = useRef(() => {
    if (scrollFrame.current !== null) return
    scrollFrame.current = requestAnimationFrame(() => {
      scrollFrame.current = null
      const el = scroller.current
      if (!el) return
      if (!restoring.current)
        following.current = el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_THRESHOLD
      setReadRow(readingRow(el))
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
  // Rows below the viewport that were never measured sit at estimated
  // heights; a scroll aims at that estimated end and stops short once they
  // take their real heights (and Virtuoso's height corrections cut a smooth
  // scroll off). From far away jump rather than glide, then finish the trip
  // once the scroll comes to rest, unless the reader takes the scroll over.
  // Virtuoso moves scrollTop itself while it corrects heights, so only the
  // reader's input, not the scroll position, says they did.
  const seekFrame = useRef<number | null>(null)
  const stopSeeking = () => {
    if (seekFrame.current !== null) cancelAnimationFrame(seekFrame.current)
    seekFrame.current = null
  }
  useEffect(
    () => () => {
      if (seekFrame.current !== null) cancelAnimationFrame(seekFrame.current)
    },
    [],
  )
  // Closing a row the reader has scrolled down past shortens the list, and
  // the browser pulls scrollTop back to the new end. Virtuoso takes that pull
  // for the reader scrolling up and "compensates" by the whole height the row
  // lost, which throws the view up past the end. Hold the clicked disclosure
  // where it was clicked for a moment instead; the end then lands in view.
  // Only a disclosure: other buttons (loading earlier messages) move on purpose.
  const holdFrame = useRef<number | null>(null)
  const releaseHold = () => {
    if (holdFrame.current !== null) cancelAnimationFrame(holdFrame.current)
    holdFrame.current = null
  }
  useEffect(
    () => () => {
      if (holdFrame.current !== null) cancelAnimationFrame(holdFrame.current)
    },
    [],
  )
  const holdClicked = (event: MouseEvent<HTMLElement>) => {
    releaseHold()
    const el = scroller.current
    const target = event.target instanceof Element ? event.target : null
    if (!el || !target || !el.contains(target)) return
    const control = target.closest('[aria-expanded]')
    if (!control) return
    const top = control.getBoundingClientRect().top
    const start = performance.now()
    holdFrame.current = requestAnimationFrame(function hold() {
      holdFrame.current = null
      if (!control.isConnected || performance.now() - start > USER_RESIZE_WINDOW_MS) return
      const shift = control.getBoundingClientRect().top - top
      if (Math.abs(shift) > 1) el.scrollTop += shift
      holdFrame.current = requestAnimationFrame(hold)
    })
  }
  const readerScrolls = () => {
    restoring.current = false
    stopSeeking()
    releaseHold()
  }
  const readerActs = () => {
    touched()
    readerScrolls()
  }
  const seekEnd = () => {
    stopSeeking()
    const from = scroller.current
    const far = from
      ? from.scrollHeight - from.scrollTop - from.clientHeight > 2 * from.clientHeight
      : false
    scrollToEnd(virtuoso.current, far ? 'auto' : 'smooth')
    const start = performance.now()
    let last = -1
    let still = 0
    seekFrame.current = requestAnimationFrame(function check() {
      seekFrame.current = null
      const el = scroller.current
      if (!el || performance.now() - start > SEEK_TIMEOUT_MS) return
      still = el.scrollTop === last ? still + 1 : 0
      last = el.scrollTop
      // Arriving at the estimated end is not arriving: the rows there are
      // measured on the next frames and push the end further down.
      if (still >= 3) {
        if (el.scrollHeight - el.scrollTop - el.clientHeight <= 2) return
        scrollToEnd(virtuoso.current, 'auto')
        still = 0
      }
      seekFrame.current = requestAnimationFrame(check)
    })
  }
  // Like the end, a row far up sits among estimated heights: aim, then aim
  // again once the scroll is still, until the row is at the top or the list
  // can scroll no further.
  const seekRow = (index: number) => {
    stopSeeking()
    setReadRow(index)
    const el = scroller.current
    if (!el) return
    const offsetOf = () => {
      const row = el.querySelector(`[data-index="${index}"]`)
      return row ? row.getBoundingClientRect().top - el.getBoundingClientRect().top : null
    }
    const before = offsetOf()
    const near = before !== null && Math.abs(before) < 2 * el.clientHeight
    virtuoso.current?.scrollToIndex({
      index,
      align: 'start',
      behavior: near && !prefersReducedMotion() ? 'smooth' : 'auto',
    })
    const start = performance.now()
    let last = -1
    let still = 0
    seekFrame.current = requestAnimationFrame(function check() {
      seekFrame.current = null
      if (performance.now() - start > SEEK_TIMEOUT_MS) return
      still = el.scrollTop === last ? still + 1 : 0
      last = el.scrollTop
      if (still >= 3) {
        const offset = offsetOf()
        const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight <= 2
        if (offset !== null && (Math.abs(offset) <= 2 || (offset > 0 && atEnd))) return
        virtuoso.current?.scrollToIndex({ index, align: 'start' })
        still = 0
      }
      seekFrame.current = requestAnimationFrame(check)
    })
  }
  const jump = (item: RailItem) => {
    if (item.loaded) {
      seekRow(item.index)
      return
    }
    if (!loadUntil || jumpingTo) return
    setJumpingTo(item.id)
    void loadUntil(item.id).then((loaded) => {
      // The loaded rows reach the list on a render after the load.
      let frames = 0
      const land = () => {
        const index = latestEntries.current.findIndex((entry) => entry.id === item.id)
        if (index < 0 && loaded && ++frames < LAND_FRAMES) {
          requestAnimationFrame(land)
          return
        }
        setJumpingTo(null)
        if (index >= 0) seekRow(index)
      }
      land()
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
    if (restoredMidway) return
    const timer = setTimeout(() => scrollToEnd(virtuoso.current, 'auto'), 150)
    return () => clearTimeout(timer)
  }, [restoredMidway])
  // Virtuoso scrolls to the initial row once, at estimated heights; when the
  // rows take their real heights later than it waits (a busy machine), the
  // row ends up elsewhere. Keep it where it was left until the reader scrolls.
  useEffect(() => {
    if (!restored || restored.atBottom) return
    const { index, offset } = restored
    const start = performance.now()
    let frame = requestAnimationFrame(function hold() {
      const el = scroller.current
      if (!el || !restoring.current || performance.now() - start > SEEK_TIMEOUT_MS) return
      const row = el.querySelector(`[data-index="${index}"]`)
      const at = row ? el.getBoundingClientRect().top - row.getBoundingClientRect().top : null
      if (at === null || Math.abs(at - offset) > 1) {
        virtuoso.current?.scrollToIndex({ index, align: 'start', offset })
      }
      frame = requestAnimationFrame(hold)
    })
    return () => cancelAnimationFrame(frame)
  }, [restored])
  // Until then the list shows rows at estimated heights, part way up the
  // conversation, and jumps; it stays hidden until it has landed at the end.
  const [settled, setSettled] = useState(restoredMidway)
  const rowsRendered = useRef(false)
  useEffect(() => {
    if (settled) return
    const start = performance.now()
    let frame = requestAnimationFrame(function check() {
      const el = scroller.current
      const landed =
        rowsRendered.current && el && el.scrollHeight - el.scrollTop - el.clientHeight <= 2
      if (landed || performance.now() - start > SETTLE_TIMEOUT_MS) setSettled(true)
      else frame = requestAnimationFrame(check)
    })
    return () => cancelAnimationFrame(frame)
  }, [settled])
  // Rows are saved by position, and rows brought in above shift every
  // position; the view that opens next starts without them, so such a list is
  // not saved.
  useLayoutEffect(() => {
    if (!stateKey) return
    const handle = virtuoso
    return () => {
      if (prepended > 0) {
        savedLists.delete(stateKey)
        return
      }
      const el = scroller.current
      if (!following.current) {
        const row = el ? topRow(el) : null
        const anchor = row ? latestEntries.current[row.index] : undefined
        if (row && anchor) {
          savedLists.set(stateKey, { atBottom: false, anchorId: anchor.id, offset: row.offset })
          return
        }
      }
      // The snapshot's scrollTop leaves out the header; the scroller's own does not.
      const scrollTop = el?.scrollTop ?? 0
      handle.current?.getState((state) =>
        savedLists.set(stateKey, { atBottom: true, state, scrollTop }),
      )
    }
  }, [stateKey, prepended])
  const last = entries[entries.length - 1]
  const running = workingState !== 'idle'
  const isStreaming = workingState === 'streaming_assistant_message'
  const streamingEntryId = isStreaming && last?.role === 'assistant' ? last.id : null
  const context: ListContext = {
    streamingEntryId,
    turnEnds: turnEnds(entries, running),
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
    <div
      className={cn('@container relative h-full', !settled && 'invisible')}
      onPointerDownCapture={readerActs}
      onKeyDownCapture={readerActs}
      onClickCapture={holdClicked}
      onWheelCapture={readerScrolls}
      onTouchMoveCapture={readerScrolls}
    >
      <Virtuoso<TranscriptEntry, ListContext>
        ref={virtuoso}
        role="log"
        aria-label="Transcript"
        // The thin scrollbar (global.css) takes room on the right; an equal
        // gutter on the left keeps the column centred, in line with the composer.
        className="h-full [scrollbar-gutter:stable_both-edges]"
        data={entries}
        context={context}
        computeItemKey={(_, entry) => entry.id}
        firstItemIndex={INDEX_BASE - prepended}
        initialTopMostItemIndex={
          !restored
            ? entries.length - 1
            : restored.atBottom
              ? undefined
              : { index: restored.index, align: 'start', offset: restored.offset }
        }
        restoreStateFrom={restored?.atBottom ? restored.state : undefined}
        initialScrollTop={restored?.atBottom ? restored.scrollTop : undefined}
        itemsRendered={(items) => {
          rowsRendered.current = items.length > 0
        }}
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
      <TurnRail items={rail} active={activeItem(rail, readRow)} loading={jumpingTo} onJump={jump} />
      {!atBottom ? (
        <Button
          size="icon-sm"
          variant="outline"
          aria-label="Scroll to latest"
          onClick={seekEnd}
          className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-background shadow-composer"
        >
          <ArrowDown aria-hidden />
        </Button>
      ) : null}
    </div>
  )
}
