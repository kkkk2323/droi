// What sits around the composer: the Session's task list and the messages the
// Daemon is holding for a running turn, above it; the context meter beside
// the workspace name under it.
import { useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import { Check, ChevronRight, Circle, Clock, CornerDownLeft, Loader2, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { formatTokens, type ContextUsage } from '@/daemon/use-context-usage'
import {
  queuedText,
  useQueuedMessageActions,
  useQueuedMessages,
} from '@/daemon/use-queued-messages'
import { useTodos, type TodoItem } from '@/daemon/use-todos'
import { cn } from '@/lib/utils'

export function TodoPanel({ sessionId }: { sessionId: string }) {
  const todos = useTodos(sessionId)
  const [open, setOpen] = useState(false)
  if (todos.length === 0) return null
  const done = todos.filter((t) => t.status === 'completed').length
  const current =
    todos.find((t) => t.status === 'in_progress') ?? todos.find((t) => t.status === 'pending')
  const allDone = done === todos.length

  return (
    <Collapsible.Root
      open={open}
      onOpenChange={setOpen}
      className="mb-2 rounded-xl border bg-card/60 text-sm"
    >
      <Collapsible.Trigger
        aria-label={`Tasks, ${done} of ${todos.length} done`}
        className="group flex w-full items-center gap-2 rounded-xl px-3 py-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <ChevronRight
          aria-hidden
          className="size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
        <span className="shrink-0 font-medium tabular-nums text-muted-foreground">
          {done}/{todos.length}
        </span>
        <span className="min-w-0 flex-1 truncate">
          {allDone ? 'All tasks done' : current?.content}
        </span>
        {!allDone && current?.status === 'in_progress' ? (
          <Loader2 aria-hidden className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
        ) : null}
      </Collapsible.Trigger>
      <Collapsible.Panel>
        <ul aria-label="Tasks" className="flex flex-col gap-1 px-3 pb-2.5">
          {todos.map((todo) => (
            <TodoRow key={todo.id} todo={todo} />
          ))}
        </ul>
      </Collapsible.Panel>
    </Collapsible.Root>
  )
}

function TodoRow({ todo }: { todo: TodoItem }) {
  return (
    <li
      data-status={todo.status}
      className={cn(
        'flex items-start gap-2 leading-5',
        todo.status === 'completed' &&
          'text-muted-foreground line-through decoration-muted-foreground/50',
      )}
    >
      {todo.status === 'completed' ? (
        <Check
          aria-label="Done"
          className="mt-1 size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400"
        />
      ) : todo.status === 'in_progress' ? (
        <Loader2
          aria-label="In progress"
          className="mt-1 size-3.5 shrink-0 animate-spin text-muted-foreground"
        />
      ) : (
        <Circle aria-label="Pending" className="mt-1 size-3.5 shrink-0 text-muted-foreground/60" />
      )}
      <span className="min-w-0 flex-1">{todo.content}</span>
    </li>
  )
}

export function QueuedMessages({ sessionId }: { sessionId: string }) {
  const queued = useQueuedMessages(sessionId)
  const actions = useQueuedMessageActions(sessionId)
  if (queued.length === 0 && !actions.error) return null
  return (
    <div className="mb-2 flex flex-col gap-1">
      {actions.error ? (
        <p role="alert" className="px-1 text-xs text-destructive-foreground">
          {actions.error}
        </p>
      ) : null}
      <ul aria-label="Queued messages" className="flex flex-col gap-1">
        {queued.map((message) => {
          const steer = message.kind !== 'daemon_queued_end_of_loop'
          return (
            <li
              key={message.requestId}
              className="flex items-center gap-2 rounded-xl border border-dashed bg-card/40 px-3 py-1.5 text-sm"
            >
              {steer ? (
                <CornerDownLeft aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
              ) : (
                <Clock aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
              )}
              <span className="min-w-0 flex-1 truncate">{queuedText(message)}</span>
              <span className="shrink-0 text-[11px] text-muted-foreground">
                {steer ? 'Next' : 'Queued'}
              </span>
              <Button
                size="icon-xs"
                variant="ghost"
                aria-label="Remove queued message"
                className="text-muted-foreground"
                onClick={() => void actions.remove(message.requestId)}
              >
                <X aria-hidden />
              </Button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/** Context window fill, as a small ring plus figures. */
export function ContextMeter({ usage }: { usage: ContextUsage | null }) {
  if (!usage) return null
  const percent = Math.round(usage.ratio * 100)
  const radius = 5.5
  const circumference = 2 * Math.PI * radius
  return (
    <span
      role="meter"
      aria-label="Context used"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      aria-valuetext={`${percent}% of context used: ${formatTokens(usage.usedTokens)} of ${formatTokens(usage.budgetTokens)} tokens`}
      title={`${usage.usedTokens.toLocaleString()} / ${usage.budgetTokens.toLocaleString()} tokens`}
      className="ml-auto flex shrink-0 items-center gap-1.5 tabular-nums"
    >
      <svg aria-hidden viewBox="0 0 14 14" className="size-3.5 -rotate-90">
        <circle cx="7" cy="7" r={radius} fill="none" strokeWidth="2" className="stroke-border" />
        <circle
          cx="7"
          cy="7"
          r={radius}
          fill="none"
          strokeWidth="2"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - usage.ratio)}
          className={cn(
            usage.ratio > 0.9
              ? 'stroke-destructive'
              : usage.ratio > 0.7
                ? 'stroke-amber-500'
                : 'stroke-foreground/60',
          )}
        />
      </svg>
      <span>
        {formatTokens(usage.usedTokens)} / {formatTokens(usage.budgetTokens)} · {percent}%
      </span>
    </span>
  )
}
