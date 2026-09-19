import { useRef, useState, type KeyboardEvent } from 'react'
import { ArrowUp, Square } from 'lucide-react'
import { Button } from '@/components/ui/button'

export function InputBar({
  isRunning,
  disabled,
  onSend,
  onCancel,
  error,
}: {
  isRunning: boolean
  disabled: boolean
  onSend: (text: string) => void
  onCancel: () => void
  error: string | null
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
      className="shrink-0 border-t bg-background p-3"
      onSubmit={(event) => {
        event.preventDefault()
        submit()
      }}
    >
      {error ? (
        <p role="alert" className="mb-2 text-xs text-destructive-foreground">
          {error}
        </p>
      ) : null}
      <div className="flex items-end gap-2 rounded-xl border bg-card px-3 py-2 focus-within:ring-2 focus-within:ring-ring/40">
        <textarea
          ref={textarea}
          aria-label="Message"
          placeholder={isRunning ? 'Droid is working…' : 'Message Droid'}
          value={text}
          rows={1}
          disabled={disabled}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={onKeyDown}
          className="max-h-48 min-h-6 flex-1 resize-none bg-transparent text-sm leading-6 outline-none placeholder:text-muted-foreground field-sizing-content"
        />
        {isRunning ? (
          <Button
            type="button"
            size="icon-sm"
            variant="secondary"
            aria-label="Cancel"
            onClick={onCancel}
          >
            <Square aria-hidden className="fill-current" />
          </Button>
        ) : (
          <Button type="submit" size="icon-sm" aria-label="Send" disabled={!canSend}>
            <ArrowUp aria-hidden />
          </Button>
        )}
      </div>
    </form>
  )
}
