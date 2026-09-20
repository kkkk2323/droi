import { useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { ArrowUp, Square, SquareSlash, Sparkles } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { filterSlashItems, slashQuery, type SlashItem } from '@/daemon/use-slash-items'

const NO_ITEMS: SlashItem[] = []

/**
 * The composer card: text on top, per-Session controls and the send button in
 * the footer row. Enter sends, Shift+Enter breaks the line.
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
  onSend: (text: string) => void
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
  const [caret, setCaret] = useState(0)
  const [highlight, setHighlight] = useState(0)
  const [dismissedFor, setDismissedFor] = useState<string | null>(null)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const canSend = !disabled && !isRunning && (allowEmpty || text.trim().length > 0)

  const query = slashQuery(text, caret)
  const suggestions =
    query !== null && dismissedFor !== text ? filterSlashItems(slashItems, query) : []
  const activeIndex = Math.min(highlight, Math.max(suggestions.length - 1, 0))

  const submit = () => {
    if (!canSend) return
    onSend(text)
    setText('')
    textarea.current?.focus()
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
      submit()
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
        className="relative flex flex-col rounded-2xl border bg-background shadow-composer transition-shadow focus-within:border-ring/40"
        onClick={(event) => {
          // Clicking the card's padding should still land in the textarea.
          if (event.target === event.currentTarget) textarea.current?.focus()
        }}
      >
        <textarea
          ref={textarea}
          aria-label="Message"
          placeholder={isRunning ? 'Droid is working…' : placeholder}
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
          <div className="ml-auto shrink-0 pl-1">
            {isRunning ? (
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
