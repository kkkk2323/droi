import { useEffect, useRef, useState } from 'react'
import type { PendingAskUserRequest, PendingPermission } from '@factory/droid-sdk'
import { Check, MessageCircleQuestion, Pencil, ShieldAlert, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { usePromptActions, usePrompts } from '@/daemon/use-prompts'
import { cn } from '@/lib/utils'

/** Every open Prompt for the Session; it stands in for the composer while one is open. */
export function PromptArea({ sessionId }: { sessionId: string }) {
  const prompts = usePrompts(sessionId)
  const actions = usePromptActions(sessionId)
  if (prompts.permissions.length === 0 && prompts.askUser.length === 0 && !actions.error)
    return null
  return (
    <div aria-live="polite" className="flex shrink-0 flex-col gap-2">
      {actions.error ? (
        <p role="alert" className="text-xs text-destructive-foreground">
          {actions.error}
        </p>
      ) : null}
      {prompts.permissions.map((permission) => (
        <PermissionCard
          key={permission.requestId}
          permission={permission}
          onAnswer={(option) => void actions.answerPermission(permission, option)}
        />
      ))}
      {prompts.askUser.map((request) => (
        <AskUserCard
          key={request.requestId}
          request={request}
          onAnswer={(answers) => void actions.answerQuestions(request, answers)}
        />
      ))}
    </div>
  )
}

export function PermissionCard({
  permission,
  onAnswer,
}: {
  permission: PendingPermission
  onAnswer: (selectedOption: string) => void
}) {
  const firstButton = useRef<HTMLButtonElement>(null)
  useEffect(() => firstButton.current?.focus(), [])
  const title = permission.toolUses.map((t) => t.toolUse.name).join(', ')

  return (
    <section
      role="group"
      aria-label={`Permission request: ${title}`}
      className="rounded-2xl border bg-background px-3.5 pt-3 pb-2.5"
    >
      <span className="flex items-center gap-1.5 text-[11px] font-semibold text-amber-600 dark:text-amber-400">
        <ShieldAlert aria-hidden className="size-3" />
        Permission
      </span>
      <p className="mt-1.5 text-[13px] leading-[18px] font-medium">Droid wants to run {title}</p>
      <ul className="mt-2 flex flex-col gap-1">
        {permission.toolUses.map((use) => (
          <li
            key={use.toolUse.id}
            className="rounded-lg bg-card px-2.5 py-1.5 font-mono text-xs leading-5 break-all"
          >
            <ToolDetails details={use.details} input={use.toolUse.input} />
          </li>
        ))}
      </ul>
      {/* One row per answer, like the question card's options; a click answers. */}
      <div className="mt-2.5 grid gap-1">
        {permission.options.map((option, index) => {
          const cancel = option.value === 'cancel'
          return (
            <button
              key={option.value}
              ref={index === 0 ? firstButton : undefined}
              type="button"
              onClick={() => onAnswer(option.value)}
              className={cn(
                'flex min-h-9 w-full items-center gap-2 rounded-lg border border-transparent bg-card px-2.5 py-1.5 text-left text-[12px] font-medium outline-none transition-colors hover:border-border focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring/30',
                cancel && 'text-muted-foreground',
              )}
            >
              <span className="min-w-0 flex-1">{option.label}</span>
              {cancel ? (
                <X aria-hidden className="size-3 shrink-0" />
              ) : (
                <Check aria-hidden className="size-3 shrink-0 text-primary" />
              )}
            </button>
          )
        })}
      </div>
    </section>
  )
}

function ToolDetails({ details, input }: { details: unknown; input: Record<string, unknown> }) {
  const d = details as Record<string, unknown>
  if (typeof d['fullCommand'] === 'string') return <>$ {d['fullCommand']}</>
  if (typeof d['filePath'] === 'string') return <>{d['filePath']}</>
  if (typeof input['command'] === 'string') return <>$ {input['command']}</>
  return <>{JSON.stringify(input)}</>
}

/**
 * One question at a time, in the style of Waku's user-input panel: topic and
 * progress on top, the options as full-width rows, a line for a custom answer,
 * Back/Next until the last question, where Answer sends everything.
 */
export function AskUserCard({
  request,
  onAnswer,
}: {
  request: PendingAskUserRequest
  onAnswer: (answers: Array<{ index: number; question: string; answer: string }>) => void
}) {
  const [step, setStep] = useState(0)
  const [chosen, setChosen] = useState<Record<number, string[]>>({})
  const [custom, setCustom] = useState<Record<number, string>>({})

  const question = request.questions[step]
  if (!question) return null
  const selected = chosen[question.index] ?? []
  const typed = custom[question.index] ?? ''
  const canContinue = selected.length > 0 || typed.trim().length > 0
  const last = step + 1 === request.questions.length

  const select = (option: string) => {
    setCustom((prev) => ({ ...prev, [question.index]: '' }))
    setChosen((prev) => {
      const current = prev[question.index] ?? []
      if (question.multiSelect) {
        return {
          ...prev,
          [question.index]: current.includes(option)
            ? current.filter((o) => o !== option)
            : [...current, option],
        }
      }
      return { ...prev, [question.index]: [option] }
    })
  }

  const type = (value: string) => {
    setCustom((prev) => ({ ...prev, [question.index]: value }))
    if (value.trim()) setChosen((prev) => ({ ...prev, [question.index]: [] }))
  }

  const advance = () => {
    if (!canContinue) return
    if (!last) {
      setStep(step + 1)
      return
    }
    onAnswer(
      request.questions.map((q) => {
        const own = custom[q.index]?.trim()
        return {
          index: q.index,
          question: q.question,
          answer: own ? own : (chosen[q.index] ?? []).join(', '),
        }
      }),
    )
  }

  return (
    <section
      role="group"
      aria-label="Droid has a question"
      className="rounded-2xl border bg-background px-3.5 pt-3 pb-2.5"
    >
      <div className="flex items-center gap-2">
        <span className="flex items-center gap-1.5 text-[11px] font-semibold text-muted-foreground">
          <MessageCircleQuestion aria-hidden className="size-3" />
          {question.topic}
        </span>
        {request.questions.length > 1 ? (
          <span className="flex h-[18px] items-center rounded-[5px] bg-card px-1.5 text-[10px] font-medium tabular-nums text-muted-foreground">
            {step + 1} / {request.questions.length}
          </span>
        ) : null}
      </div>
      <p className="mt-1.5 text-[13px] leading-[18px] font-medium whitespace-pre-wrap">
        {question.question}
      </p>
      {question.options.length > 0 ? (
        <div
          className="mt-2.5 grid gap-1"
          role={question.multiSelect ? 'group' : 'radiogroup'}
          aria-label={question.question}
        >
          {question.options.map((option) => {
            const checked = selected.includes(option)
            return (
              <button
                key={option}
                type="button"
                role={question.multiSelect ? 'checkbox' : 'radio'}
                aria-checked={checked}
                onClick={() => select(option)}
                className={cn(
                  'flex min-h-9 w-full items-center gap-2 rounded-lg border border-transparent bg-card px-2.5 py-1.5 text-left text-[12px] font-medium outline-none transition-colors hover:border-border focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring/30',
                  checked && 'border-primary/35 bg-primary/[0.08]',
                )}
              >
                <span className="min-w-0 flex-1">{option}</span>
                {checked ? <Check aria-hidden className="size-3 shrink-0 text-primary" /> : null}
              </button>
            )
          })}
        </div>
      ) : null}
      <div
        className={cn(
          'mt-1 flex h-[34px] items-center gap-2 rounded-lg border border-transparent bg-card px-2.5 focus-within:border-ring',
          typed.trim() && 'border-primary/35 bg-primary/[0.06]',
        )}
      >
        <Pencil
          aria-hidden
          className={cn('size-3 shrink-0 text-muted-foreground/60', typed.trim() && 'text-primary')}
        />
        <input
          type="text"
          aria-label={`Other answer for: ${question.question}`}
          placeholder="Or type your own answer"
          value={typed}
          onChange={(event) => type(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
              event.preventDefault()
              advance()
            }
          }}
          className="min-w-0 flex-1 bg-transparent text-[12px] outline-none placeholder:text-muted-foreground/60"
        />
      </div>
      <div className="mt-2 flex items-center gap-2">
        {step > 0 ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-7 px-2 text-[11px]"
            onClick={() => setStep(step - 1)}
          >
            Back
          </Button>
        ) : null}
        <div className="flex-1" />
        <Button
          type="button"
          size="sm"
          className="h-7 px-2.5 text-[11px]"
          disabled={!canContinue}
          onClick={advance}
        >
          {last ? 'Answer' : 'Next'}
        </Button>
      </div>
    </section>
  )
}
