import { useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { ArrowUp, Square } from 'lucide-react'
import { Button } from '@/components/ui/button'

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
}: {
  isRunning: boolean
  disabled: boolean
  onSend: (text: string) => void
  onCancel: () => void
  error: string | null
  footer?: ReactNode
}) {
  const [text, setText] = useState('')
  const textarea = useRef<HTMLTextAreaElement>(null)
  const canSend = !disabled && !isRunning && text.trim().length > 0

  const submit = () => {
    if (!canSend) return
    onSend(text)
    setText('')
    textarea.current?.focus()
  }

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      submit()
    }
  }

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
        className="flex flex-col rounded-2xl border bg-background shadow-composer transition-shadow focus-within:border-ring/40"
        onClick={(event) => {
          // Clicking the card's padding should still land in the textarea.
          if (event.target === event.currentTarget) textarea.current?.focus()
        }}
      >
        <textarea
          ref={textarea}
          aria-label="Message"
          placeholder={isRunning ? 'Droid is working…' : 'Ask anything'}
          value={text}
          rows={1}
          disabled={disabled}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={onKeyDown}
          className="max-h-56 min-h-6 w-full resize-none bg-transparent px-4 pt-3.5 pb-1 text-sm leading-6 outline-none placeholder:text-muted-foreground field-sizing-content"
        />
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
                aria-label="Send"
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
