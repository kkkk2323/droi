import { useEffect, useRef, useState } from 'react'
import type { PendingAskUserRequest, PendingPermission } from '@factory/droid-sdk'
import { ShieldAlert, MessageCircleQuestion } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { usePromptActions, usePrompts } from '@/daemon/use-prompts'

/** Every open Prompt for the Session, stacked above the composer. */
export function PromptArea({ sessionId }: { sessionId: string }) {
  const prompts = usePrompts(sessionId)
  const actions = usePromptActions(sessionId)
  if (prompts.permissions.length === 0 && prompts.askUser.length === 0 && !actions.error)
    return null
  return (
    <div aria-live="polite" className="flex shrink-0 flex-col gap-2 pb-3">
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
      className="rounded-2xl border bg-card/60 p-4 shadow-composer"
    >
      <header className="mb-3 flex items-center gap-2 text-sm font-medium">
        <span className="flex size-7 items-center justify-center rounded-md bg-amber-500/15 text-amber-600 dark:text-amber-400">
          <ShieldAlert aria-hidden className="size-4" />
        </span>
        Droid wants to run {title}
      </header>
      <ul className="mb-3 flex flex-col gap-1.5">
        {permission.toolUses.map((use) => (
          <li
            key={use.toolUse.id}
            className="rounded-lg border bg-background px-3 py-2 font-mono text-xs leading-5 break-all"
          >
            <ToolDetails details={use.details} input={use.toolUse.input} />
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        {permission.options.map((option, index) => (
          <Button
            key={option.value}
            ref={index === 0 ? firstButton : undefined}
            size="sm"
            variant={option.value === 'cancel' ? 'outline' : index === 0 ? 'default' : 'secondary'}
            onClick={() => onAnswer(option.value)}
          >
            {option.label}
          </Button>
        ))}
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

export function AskUserCard({
  request,
  onAnswer,
}: {
  request: PendingAskUserRequest
  onAnswer: (answers: Array<{ index: number; question: string; answer: string }>) => void
}) {
  const [chosen, setChosen] = useState<Record<number, string[]>>({})
  const [custom, setCustom] = useState<Record<number, string>>({})
  const complete = request.questions.every(
    (q) => (chosen[q.index]?.length ?? 0) > 0 || (custom[q.index]?.trim().length ?? 0) > 0,
  )

  const toggle = (question: PendingAskUserRequest['questions'][number], option: string) => {
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

  const submit = () => {
    onAnswer(
      request.questions.map((q) => ({
        index: q.index,
        question: q.question,
        answer: [
          ...(chosen[q.index] ?? []),
          ...(custom[q.index]?.trim() ? [custom[q.index]!.trim()] : []),
        ].join(', '),
      })),
    )
  }

  return (
    <section
      role="group"
      aria-label="Droid has a question"
      className="rounded-2xl border bg-card/60 p-4 shadow-composer"
    >
      <header className="mb-3 flex items-center gap-2 text-sm font-medium">
        <span className="flex size-7 items-center justify-center rounded-md bg-sky-500/15 text-sky-600 dark:text-sky-400">
          <MessageCircleQuestion aria-hidden className="size-4" />
        </span>
        Droid has a question
      </header>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault()
          if (complete) submit()
        }}
      >
        {request.questions.map((question) => (
          <fieldset key={question.index} className="flex flex-col gap-1.5">
            <legend className="mb-1 text-sm">{question.question}</legend>
            {question.options.map((option) => {
              const id = `${request.requestId}-${question.index}-${option}`
              const checked = chosen[question.index]?.includes(option) ?? false
              return (
                <label key={option} htmlFor={id} className="flex items-center gap-2 text-sm">
                  <input
                    id={id}
                    type={question.multiSelect ? 'checkbox' : 'radio'}
                    name={`${request.requestId}-${question.index}`}
                    value={option}
                    checked={checked}
                    onChange={() => toggle(question, option)}
                    className="accent-primary"
                  />
                  {option}
                </label>
              )
            })}
            <input
              type="text"
              aria-label={`Other answer for: ${question.question}`}
              placeholder="Or type your own answer"
              value={custom[question.index] ?? ''}
              onChange={(event) =>
                setCustom((prev) => ({ ...prev, [question.index]: event.target.value }))
              }
              className="mt-1 h-8 rounded-lg border bg-background px-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            />
          </fieldset>
        ))}
        <div>
          <Button type="submit" size="sm" disabled={!complete}>
            Answer
          </Button>
        </div>
      </form>
    </section>
  )
}
