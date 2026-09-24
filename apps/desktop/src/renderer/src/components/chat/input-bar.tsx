import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { ArrowUp, Plus, Square, SquareSlash, Sparkles, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  filterSlashItems,
  pickedSlashItem,
  slashQuery,
  type SlashItem,
} from '@droi/daemon-layer/use-slash-items'
import { attachmentUrl, type ImageAttachment } from '@droi/daemon-layer/attachments'
import { pathsAsText, readImageAttachment, transferFiles } from '@/lib/attachments'
import { loadDraft, saveDraft } from '@droi/daemon-layer/drafts'
import { uuid } from '@droi/daemon-layer/uuid'
import { cn } from '@/lib/utils'
import type { QueuePlacement } from '@droi/daemon-layer/use-turn'

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
  draftKey,
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
  /** Keeps what is typed while the user is away from this Session. */
  draftKey?: string
}) {
  const [text, setText] = useState(() => (draftKey ? loadDraft(draftKey).text : ''))
  const [images, setImages] = useState<ImageAttachment[]>(() =>
    draftKey ? loadDraft(draftKey).images : [],
  )
  useEffect(() => {
    if (draftKey) saveDraft(draftKey, { text, images })
  }, [draftKey, text, images])
  const [caret, setCaret] = useState(0)
  const [highlight, setHighlight] = useState(0)
  const [dismissedFor, setDismissedFor] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  // Pastes still being read/shrunk; a send in the meantime goes out once they land.
  const [pendingFiles, setPendingFiles] = useState(0)
  const pendingRef = useRef(0)
  const heldSubmit = useRef<{ text: string; placement?: QueuePlacement } | null>(null)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const hasContent = text.trim().length > 0 || images.length > 0
  const canSend = !disabled && (allowEmpty || hasContent || pendingFiles > 0)

  // A picked command or skill shows as a tag; the textarea edits what follows it.
  const picked = pickedSlashItem(text, slashItems)
  const prefix = picked ? text.slice(0, text.length - picked.rest.length) : ''
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
    // The name becomes the tag, so the caret starts the empty textarea.
    pendingCaret.current = 0
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

  const insertAtCaret = (insert: string) => {
    const value = text.slice(prefix.length)
    const start = textarea.current?.selectionStart ?? value.length
    const end = textarea.current?.selectionEnd ?? value.length
    const before = value.slice(0, start)
    const after = value.slice(end)
    const spaced =
      (before === '' || /\s$/.test(before) ? '' : ' ') + insert + (/^\s/.test(after) ? '' : ' ')
    pendingCaret.current = before.length + spaced.length
    setText(prefix + before + spaced + after)
    setCaret(prefix.length + pendingCaret.current)
  }

  /**
   * Images become attachments; any other file goes in as its full path, which
   * only the Local Client can read. Returns whether anything was taken.
   */
  const takeFiles = (transfer: DataTransfer | null): boolean => {
    const files = transferFiles(transfer)
    const pathFor = window.droiShell?.pathForFile
    const paths = pathFor ? pathsAsText(files.others, pathFor) : ''
    if (paths) insertAtCaret(paths)
    addFiles(files.images)
    return files.images.length > 0 || paths !== ''
  }

  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    if (takeFiles(event.clipboardData)) event.preventDefault()
  }

  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragging(false)
    takeFiles(event.dataTransfer)
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
    const el = event.currentTarget
    if (picked && event.key === 'Backspace' && el.selectionStart === 0 && el.selectionEnd === 0) {
      event.preventDefault()
      setText(picked.rest)
      return
    }
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      submit(event.metaKey || event.ctrlKey ? 'end_of_turn' : undefined)
    }
  }

  const syncCaret = () => setCaret(prefix.length + (textarea.current?.selectionStart ?? 0))

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
          'relative flex flex-col rounded-2xl border bg-background transition-colors',
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
        <div className="flex items-start">
          {picked ? (
            <SlashTag
              item={picked.item}
              onRemove={() => {
                setText(picked.rest)
                textarea.current?.focus()
              }}
            />
          ) : null}
          <textarea
            ref={textarea}
            aria-label="Message"
            placeholder={
              isRunning
                ? 'Queue a message… (⌘↩ inserts it now)'
                : (picked?.item.argumentHint ?? placeholder)
            }
            value={text.slice(prefix.length)}
            rows={1}
            disabled={disabled}
            onChange={(event) => {
              setText(prefix + event.target.value)
              setCaret(prefix.length + (event.target.selectionStart ?? event.target.value.length))
              setHighlight(0)
            }}
            onKeyDown={onKeyDown}
            onKeyUp={syncCaret}
            onClick={syncCaret}
            onPaste={onPaste}
            // An iOS keyboard may push the document up; put it back once it goes.
            onBlur={() => window.scrollTo(0, 0)}
            aria-autocomplete="list"
            aria-controls={suggestions.length > 0 ? 'slash-suggestions' : undefined}
            aria-expanded={suggestions.length > 0}
            aria-activedescendant={
              suggestions[activeIndex] ? `slash-${suggestions[activeIndex].name}` : undefined
            }
            className={cn(
              'max-h-56 min-h-6 w-full min-w-0 flex-1 resize-none bg-transparent px-4 pt-3.5 pb-1 text-sm leading-6 outline-none placeholder:text-muted-foreground field-sizing-content',
              picked && 'pl-2',
            )}
          />
        </div>
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
          {/* The picker is the way in on a phone; paste and drop cover the desktop. */}
          <input
            ref={fileInput}
            type="file"
            accept="image/*"
            multiple
            hidden
            aria-label="Choose images"
            onChange={(event) => {
              addFiles(Array.from(event.target.files ?? []))
              event.target.value = ''
            }}
          />
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            className="rounded-full text-muted-foreground"
            aria-label="Add image"
            disabled={disabled}
            onClick={() => fileInput.current?.click()}
          >
            <Plus aria-hidden />
          </Button>
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
                {/* Stop alone while nothing is typed; Queue appears with the first character. */}
                {canSend ? (
                  <Button
                    type="submit"
                    size="icon-sm"
                    className="rounded-full"
                    aria-label="Queue"
                    title="Send after this turn (↩); ⌘↩ hands it to the running turn"
                  >
                    <ArrowUp aria-hidden />
                  </Button>
                ) : null}
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

/** The command or skill the message will run, so a pick is not mistaken for plain text. */
function SlashTag({ item, onRemove }: { item: SlashItem; onRemove: () => void }) {
  const kind = item.kind === 'skill' ? 'Skill' : 'Command'
  const Icon = item.kind === 'skill' ? Sparkles : SquareSlash
  return (
    <span
      role="group"
      aria-label={`${kind} ${item.name}`}
      title={item.description || undefined}
      className="mt-3.5 ml-3 inline-flex h-6 max-w-[45%] shrink-0 items-center gap-1 rounded-md bg-sky-500/10 pr-0.5 pl-1.5 text-[13px] font-medium text-sky-700 dark:text-sky-300"
    >
      <Icon aria-hidden className="size-3.5 shrink-0" />
      <span className="truncate">{item.name}</span>
      <button
        type="button"
        aria-label={`Remove ${kind.toLowerCase()} ${item.name}`}
        onClick={onRemove}
        className="flex size-5 shrink-0 items-center justify-center rounded opacity-60 outline-none transition-opacity hover:opacity-100 focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <X aria-hidden className="size-3" />
      </button>
    </span>
  )
}
