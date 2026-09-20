import {
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { ArrowUp, Square, SquareSlash, Sparkles, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { filterSlashItems, slashQuery, type SlashItem } from '@/daemon/use-slash-items'
import {
  attachmentUrl,
  imageFiles,
  readImageAttachment,
  type ImageAttachment,
} from '@/lib/attachments'
import { uuid } from '@/lib/uuid'
import { cn } from '@/lib/utils'
import type { QueuePlacement } from '@/daemon/use-turn'

const NO_ITEMS: SlashItem[] = []

export interface Submission {
  text: string
  images: ImageAttachment[]
  /** Set when the Session was busy: Enter queues, ⌘/Ctrl+Enter injects. */
  placement?: QueuePlacement
}

/**
 * The composer card: attachments and text on top, per-Session controls and the
 * send button in the footer row. Enter sends, Shift+Enter breaks the line.
 * While a turn runs, Enter queues the message for after the turn and
 * ⌘/Ctrl+Enter hands it to the running turn.
 */
export function InputBar({
  isRunning,
  disabled,
  onSend,
  onCancel,
  error,
  footer,
  placeholder = 'Ask anything',
  allowEmpty = false,
  sendLabel = 'Send',
  slashItems = NO_ITEMS,
}: {
  isRunning: boolean
  disabled: boolean
  onSend: (submission: Submission) => void
  onCancel: () => void
  error: string | null
  footer?: ReactNode
  placeholder?: string
  /** Let the button fire with nothing typed (starting a Session without a first message). */
  allowEmpty?: boolean
  sendLabel?: string
  /** Commands and skills offered when the message starts with "/". */
  slashItems?: SlashItem[]
}) {
  const [text, setText] = useState('')
  const [images, setImages] = useState<ImageAttachment[]>([])
  const [caret, setCaret] = useState(0)
  const [highlight, setHighlight] = useState(0)
  const [dismissedFor, setDismissedFor] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  // Pastes still being read/shrunk; a send in the meantime goes out once they land.
  const [pendingFiles, setPendingFiles] = useState(0)
  const pendingRef = useRef(0)
  const heldSubmit = useRef<{ text: string; placement?: QueuePlacement } | null>(null)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const hasContent = text.trim().length > 0 || images.length > 0
  const canSend = !disabled && (allowEmpty || hasContent || pendingFiles > 0)

  const query = slashQuery(text, caret)
  const suggestions =
    query !== null && dismissedFor !== text ? filterSlashItems(slashItems, query) : []
  const activeIndex = Math.min(highlight, Math.max(suggestions.length - 1, 0))

  const dispatch = (submission: Submission) => {
    onSend(submission)
    setText('')
    setImages([])
    textarea.current?.focus()
  }
  const placementFor = (placement?: QueuePlacement) =>
    isRunning ? { placement: placement ?? 'end_of_loop' } : {}

  const submit = (placement?: QueuePlacement) => {
    if (!canSend) return
    if (pendingRef.current > 0) {
      heldSubmit.current = { text, placement }
      setText('')
      return
    }
    dispatch({ text, images, ...placementFor(placement) })
  }

  // The caret must land after the inserted name in the same commit as the
  // text, or keys typed right after the pick go to the old position.
  const pendingCaret = useRef<number | null>(null)
  useLayoutEffect(() => {
    const el = textarea.current
    if (pendingCaret.current === null || !el) return
    el.focus()
    el.setSelectionRange(pendingCaret.current, pendingCaret.current)
    pendingCaret.current = null
  })

  const accept = (item: SlashItem) => {
    const next = `/${item.name} `
    pendingCaret.current = next.length
    setText(next)
    setCaret(next.length)
    setHighlight(0)
  }

  const addFiles = (files: File[]) => {
    if (files.length === 0) return
    pendingRef.current += files.length
    setPendingFiles(pendingRef.current)
    void Promise.all(files.map((file) => readImageAttachment(file, uuid())))
      .catch((cause: unknown) => {
        console.error('Could not read pasted image', cause)
        return [] as ImageAttachment[]
      })
      .then((added) => {
        pendingRef.current -= files.length
        setPendingFiles(pendingRef.current)
        setImages((current) => {
          const next = [...current, ...added]
          const held = heldSubmit.current
          if (held && pendingRef.current === 0) {
            heldSubmit.current = null
            // Deferred to leave the state update pure.
            queueMicrotask(() =>
              dispatch({ text: held.text, images: next, ...placementFor(held.placement) }),
            )
            return []
          }
          return next
        })
      })
  }

  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = imageFiles(event.clipboardData)
    if (files.length === 0) return
    event.preventDefault()
    addFiles(files)
  }

  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragging(false)
    addFiles(imageFiles(event.dataTransfer))
  }

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (suggestions.length > 0) {
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setHighlight((activeIndex + 1) % suggestions.length)
        return
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        setHighlight((activeIndex - 1 + suggestions.length) % suggestions.length)
        return
      }
      if (event.key === 'Tab' || (event.key === 'Enter' && !event.shiftKey)) {
        event.preventDefault()
        accept(suggestions[activeIndex]!)
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        setDismissedFor(text)
        return
      }
    }
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      submit(event.metaKey || event.ctrlKey ? 'end_of_turn' : undefined)
    }
  }

  const syncCaret = () => setCaret(textarea.current?.selectionStart ?? 0)

  return (
    <form
      aria-label="Message composer"
      className="flex flex-col gap-1.5"
      onSubmit={(event) => {
        event.preventDefault()
        submit()
      }}
    >
      {error ? (
        <p role="alert" className="px-1 text-xs text-destructive-foreground">
          {error}
        </p>
      ) : null}
      <div
        className={cn(
          'relative flex flex-col rounded-2xl border bg-background transition-colors focus-within:border-foreground/25',
          dragging && 'border-primary/60 bg-primary/5',
        )}
        onClick={(event) => {
          // Clicking the card's padding should still land in the textarea.
          if (event.target === event.currentTarget) textarea.current?.focus()
        }}
        onDragOver={(event) => {
          if (!event.dataTransfer.types.includes('Files')) return
          event.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
      >
        {images.length > 0 ? (
          <ul aria-label="Attachments" className="flex flex-wrap gap-2 px-3 pt-3">
            {images.map((image) => (
              <li key={image.id} className="group relative">
                <img
                  src={attachmentUrl(image)}
                  alt={image.name}
                  className="size-16 rounded-lg border object-cover"
                />
                <button
                  type="button"
                  aria-label={`Remove ${image.name}`}
                  onClick={() => setImages((current) => current.filter((i) => i.id !== image.id))}
                  className="absolute -top-1.5 -right-1.5 flex size-5 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                >
                  <X aria-hidden className="size-3" />
                </button>
              </li>
            ))}
          </ul>
        ) : null}
        <textarea
          ref={textarea}
          aria-label="Message"
          placeholder={isRunning ? 'Queue a message… (⌘↩ inserts it now)' : placeholder}
          value={text}
          rows={1}
          disabled={disabled}
          onChange={(event) => {
            setText(event.target.value)
            setCaret(event.target.selectionStart ?? event.target.value.length)
            setHighlight(0)
          }}
          onKeyDown={onKeyDown}
          onKeyUp={syncCaret}
          onClick={syncCaret}
          onPaste={onPaste}
          aria-autocomplete="list"
          aria-controls={suggestions.length > 0 ? 'slash-suggestions' : undefined}
          aria-expanded={suggestions.length > 0}
          aria-activedescendant={
            suggestions[activeIndex] ? `slash-${suggestions[activeIndex].name}` : undefined
          }
          className="max-h-56 min-h-6 w-full resize-none bg-transparent px-4 pt-3.5 pb-1 text-sm leading-6 outline-none placeholder:text-muted-foreground field-sizing-content"
        />
        {suggestions.length > 0 ? (
          <ul
            id="slash-suggestions"
            role="listbox"
            aria-label="Commands and skills"
            className="absolute inset-x-2 bottom-full mb-2 max-h-72 overflow-y-auto rounded-xl border bg-popover p-1 text-popover-foreground shadow-lg"
          >
            {suggestions.map((item, index) => (
              <li
                key={item.name}
                id={`slash-${item.name}`}
                role="option"
                aria-selected={index === activeIndex}
                data-highlighted={index === activeIndex || undefined}
                onMouseMove={() => setHighlight(index)}
                onMouseDown={(event) => {
                  // Keep focus in the textarea; the click only picks.
                  event.preventDefault()
                  accept(item)
                }}
                className="flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px] data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
              >
                {item.kind === 'skill' ? (
                  <Sparkles aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <SquareSlash aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                )}
                <span className="shrink-0 font-medium">/{item.name}</span>
                {item.argumentHint ? (
                  <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                    {item.argumentHint}
                  </span>
                ) : null}
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {item.description}
                </span>
                <span className="shrink-0 text-[11px] text-muted-foreground">
                  {item.kind === 'skill' ? 'Skill' : 'Command'}
                </span>
              </li>
            ))}
          </ul>
        ) : null}
        <div className="flex items-center gap-1 px-2 pb-2 pt-1">
          {footer}
          <div className="ml-auto flex shrink-0 items-center gap-1 pl-1">
            {isRunning ? (
              <>
                <Button
                  type="button"
                  size="icon-sm"
                  variant="secondary"
                  className="rounded-full"
                  aria-label="Cancel"
                  onClick={onCancel}
                >
                  <Square aria-hidden className="size-3 fill-current" />
                </Button>
                <Button
                  type="submit"
                  size="icon-sm"
                  className="rounded-full"
                  aria-label="Queue"
                  title="Send after this turn (↩); ⌘↩ hands it to the running turn"
                  disabled={!canSend}
                >
                  <ArrowUp aria-hidden />
                </Button>
              </>
            ) : (
              <Button
                type="submit"
                size="icon-sm"
                className="rounded-full"
                aria-label={sendLabel}
                disabled={!canSend}
              >
                <ArrowUp aria-hidden />
              </Button>
            )}
          </div>
        </div>
      </div>
    </form>
  )
}
