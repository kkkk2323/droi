import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react'
import type { TranscriptEntry } from '@droi/daemon-layer/transcript'
import { cn } from '@/lib/utils'

/** One message the user sent, as the rail shows it. */
export interface RailItem {
  /** The message's id. */
  id: string
  /**
   * Position of the user's entry in the transcript. A message from before the
   * loaded ones takes the position of the first loaded row after it.
   */
  index: number
  /** False for a message from before the loaded ones: jumping there loads it first. */
  loaded: boolean
  prompt: string
  /** The start of Droid's reply to it; empty until one arrives, and while not loaded. */
  response: string
}

/** Height of one mark; the rail scrolls once marks outgrow it. */
const PITCH = 10
/** Padding above the first mark and below the last (py-1.5). */
const INSET = 6
/** A mark this close to a scrollable end sits under the fade. */
const FADE = 24
const RESPONSE_CHARS = 300

function textOf(entry: TranscriptEntry): string {
  return entry.blocks
    .flatMap((block) => (block.kind === 'text' ? [block.text] : []))
    .join(' ')
    .replace(/\s+/g, ' ')
    .trim()
}

/**
 * The items for the loaded entries, with the user's messages from before the
 * loaded ones (`unloaded`) placed ahead of entry `at`.
 */
export function railItems(
  entries: readonly TranscriptEntry[],
  unloaded: readonly TranscriptEntry[] = [],
  at = 0,
): RailItem[] {
  const items: RailItem[] = []
  const prompt = (entry: TranscriptEntry) => textOf(entry) || 'Attached image'
  const addUnloaded = () => {
    for (const entry of unloaded) {
      items.push({ id: entry.id, index: at, loaded: false, prompt: prompt(entry), response: '' })
    }
  }
  entries.forEach((entry, index) => {
    if (index === at) addUnloaded()
    if (entry.role === 'user') {
      items.push({ id: entry.id, index, loaded: true, prompt: prompt(entry), response: '' })
      return
    }
    const last = items[items.length - 1]
    // After the unloaded messages the first rows can be the middle of a reply.
    if (last?.loaded && !last.response) last.response = textOf(entry).slice(0, RESPONSE_CHARS)
  })
  if (at >= entries.length) addUnloaded()
  return items
}

/** The message whose turn holds the given transcript row; the last one when unknown. */
export function activeItem(items: readonly RailItem[], row: number | null): number {
  if (row === null) return items.length - 1
  let active = 0
  items.forEach((item, position) => {
    if (item.index <= row) active = position
  })
  return active
}

/**
 * A ladder of marks along the transcript's right edge, one per message the
 * user sent. Hovering or focusing a mark previews the message and the start
 * of its reply; clicking it jumps there, loading the way first for a message
 * from before the loaded ones. The arrow keys move between marks.
 */
export function TurnRail({
  items,
  active,
  loading,
  onJump,
}: {
  items: readonly RailItem[]
  /** Position in `items` of the turn being read. */
  active: number
  /** The message whose earlier messages are loading for a jump. */
  loading: string | null
  onJump: (item: RailItem) => void
}) {
  const [preview, setPreview] = useState<number | null>(null)
  const [view, setView] = useState({ top: 0, fadeTop: false, fadeBottom: false })
  const scroller = useRef<HTMLDivElement>(null)
  // The rail does not move under a pointer that is working it.
  const pointerInside = useRef(false)
  const previewId = useId()

  const measure = () => {
    const el = scroller.current
    if (!el) return
    const top = el.scrollTop
    const fadeTop = top > 1
    const fadeBottom = top < el.scrollHeight - el.clientHeight - 1
    setView((v) =>
      v.top === top && v.fadeTop === fadeTop && v.fadeBottom === fadeBottom
        ? v
        : { top, fadeTop, fadeBottom },
    )
  }
  const shownRail = items.length >= 2
  useLayoutEffect(measure, [items.length])
  // The window resizing changes which ends can scroll.
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [shownRail])
  useEffect(() => {
    const el = scroller.current
    if (!el || pointerInside.current) return
    const center = INSET + active * PITCH + PITCH / 2
    if (center < el.scrollTop + FADE || center > el.scrollTop + el.clientHeight - FADE)
      el.scrollTop = center - el.clientHeight / 2
  }, [active, items.length])

  if (!shownRail) return null

  const focusMark = (position: number) => {
    const marks = scroller.current?.querySelectorAll<HTMLButtonElement>('button')
    marks?.[Math.max(0, Math.min(items.length - 1, position))]?.focus()
  }
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>, position: number) => {
    const next =
      event.key === 'ArrowUp'
        ? position - 1
        : event.key === 'ArrowDown'
          ? position + 1
          : event.key === 'Home'
            ? 0
            : event.key === 'End'
              ? items.length - 1
              : null
    if (next === null) return
    event.preventDefault()
    focusMark(next)
  }

  const shown = preview === null ? undefined : items[preview]
  return (
    <nav
      aria-label="Your messages"
      className="pointer-events-none absolute inset-y-0 right-2 z-10 hidden w-7 items-center @min-[53rem]:flex"
    >
      <div
        className="pointer-events-auto relative flex max-h-[min(420px,60%)] w-full flex-col"
        onPointerEnter={() => {
          pointerInside.current = true
        }}
        onPointerLeave={() => {
          pointerInside.current = false
          setPreview(null)
        }}
      >
        <div
          ref={scroller}
          onScroll={measure}
          className={cn(
            'min-h-0 overflow-y-auto overscroll-contain py-1.5 [scrollbar-width:none]',
            view.fadeTop && view.fadeBottom
              ? '[mask-image:linear-gradient(to_bottom,transparent,#000_24px,#000_calc(100%-24px),transparent)]'
              : view.fadeTop
                ? '[mask-image:linear-gradient(to_bottom,transparent,#000_24px)]'
                : view.fadeBottom
                  ? '[mask-image:linear-gradient(to_bottom,#000_calc(100%-24px),transparent)]'
                  : null,
          )}
        >
          {items.map((item, position) => {
            const current = position === active
            return (
              <button
                // By row, not by message: with stable keys the marks moved down
                // as a page of earlier messages came in, and the transcript's
                // own correction for those rows then fell short.
                key={item.loaded ? item.index : item.id}
                type="button"
                tabIndex={current ? 0 : -1}
                aria-label={`Jump to message ${position + 1}`}
                aria-current={current ? 'true' : undefined}
                aria-busy={loading === item.id || undefined}
                aria-describedby={preview === position ? previewId : undefined}
                onPointerMove={() => setPreview(position)}
                onFocus={() => setPreview(position)}
                onBlur={() => setPreview(null)}
                onKeyDown={(event) => onKeyDown(event, position)}
                onClick={() => onJump(item)}
                className="group flex h-2.5 w-full shrink-0 cursor-pointer items-center justify-end outline-none"
              >
                <span
                  aria-hidden
                  className={cn(
                    'h-0.5 w-5 origin-right rounded-full transition-[scale,background-color] duration-150',
                    loading === item.id
                      ? 'bg-info motion-safe:animate-pulse'
                      : current
                        ? 'bg-foreground'
                        : preview === position
                          ? 'scale-x-90 bg-muted-foreground'
                          : 'scale-x-60 bg-muted-foreground/40',
                    'group-focus-visible:scale-x-100 group-focus-visible:bg-ring',
                  )}
                />
              </button>
            )
          })}
        </div>
        {shown ? (
          <div
            id={previewId}
            role="tooltip"
            style={{ top: INSET + (preview ?? 0) * PITCH + PITCH / 2 - view.top }}
            className="pointer-events-none absolute right-full mr-2.5 w-72 -translate-y-1/2 rounded-xl border bg-popover px-3 py-2.5 text-popover-foreground shadow-composer"
          >
            <div className="line-clamp-2 text-[13px] font-medium wrap-anywhere">{shown.prompt}</div>
            {loading === shown.id ? (
              <div className="mt-1 text-xs leading-5 text-muted-foreground">
                Loading earlier messages…
              </div>
            ) : shown.response ? (
              <div className="mt-1 line-clamp-3 text-xs leading-5 wrap-anywhere text-muted-foreground">
                {shown.response}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </nav>
  )
}
