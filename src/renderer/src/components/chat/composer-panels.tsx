// What sits around the composer: the Session's task list and the messages the
// Daemon is holding for a running turn, on a shelf tucked under its top edge;
// the context meter beside the workspace name under it.
import { useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import {
  ChevronDown,
  Circle,
  CircleCheck,
  Clock,
  CornerDownLeft,
  ListChecks,
  Loader2,
  X,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { formatTokens, type ContextUsage } from '@/daemon/use-context-usage'
import {
  queuedText,
  useQueuedMessageActions,
  useQueuedMessages,
} from '@/daemon/use-queued-messages'
import { useTodos, type TodoItem } from '@/daemon/use-todos'
import { cn } from '@/lib/utils'

/**
 * The task list and the queued messages, in one card tucked against the
 * composer's top edge (rounded top corners only, open bottom) so the two read
 * as one piece with it.
 */
export function ComposerShelf({ sessionId }: { sessionId: string }) {
  const todos = useTodos(sessionId)
  const queued = useQueuedMessages(sessionId)
  const actions = useQueuedMessageActions(sessionId)
  const done = todos.filter((t) => t.status === 'completed').length
  // A finished list has nothing left to steer; it stays in the transcript's tool rows.
  const showTodos = todos.length > 0 && done < todos.length
  if (!showTodos && queued.length === 0 && !actions.error) return null
  return (
    <div className="px-3.5">
      <div className="flex flex-col divide-y overflow-hidden rounded-t-xl border border-b-0 bg-background">
        {showTodos ? <TodoPanel todos={todos} done={done} /> : null}
        {queued.length > 0 || actions.error ? (
          <QueuedMessages
            queued={queued}
            error={actions.error}
            onRemove={(id) => void actions.remove(id)}
          />
        ) : null}
      </div>
    </div>
  )
}

function TodoPanel({ todos, done }: { todos: TodoItem[]; done: number }) {
  const [open, setOpen] = useState(false)
  const current =
    todos.find((t) => t.status === 'in_progress') ?? todos.find((t) => t.status === 'pending')

  // Folded: the task under way, with the count. Open: the count, then the list.
  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="py-1">
      <Collapsible.Trigger
        aria-label={`Tasks, ${done} of ${todos.length} done`}
        className="group flex h-[30px] w-full items-center gap-2 pr-1.5 pl-3 text-left text-[12.5px] outline-none transition-colors hover:bg-accent focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset"
      >
        {open ? (
          <>
            <ListChecks aria-hidden className="size-3 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate text-muted-foreground">Tasks</span>
          </>
        ) : (
          <>
            <StatusIcon status={current?.status ?? 'pending'} />
            <span className="min-w-0 flex-1 truncate">{current?.content}</span>
          </>
        )}
        <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
          {done}/{todos.length}
        </span>
        <span className="grid size-6 shrink-0 place-items-center text-muted-foreground">
          <ChevronDown
            aria-hidden
            className="size-3 transition-transform duration-150 group-data-[panel-open]:rotate-180"
          />
        </span>
      </Collapsible.Trigger>
      <Collapsible.Panel>
        <ul aria-label="Tasks" className="flex flex-col pb-1">
          {todos.map((todo) => (
            <li
              key={todo.id}
              data-status={todo.status}
              className={cn(
                'flex min-h-[26px] items-start gap-2 py-[3px] pr-3 pl-3 text-[12.5px] leading-5',
                todo.status === 'completed' && 'text-muted-foreground',
              )}
            >
              <span className="flex h-5 items-center">
                <StatusIcon status={todo.status} />
              </span>
              <span className="min-w-0 flex-1">{todo.content}</span>
            </li>
          ))}
        </ul>
      </Collapsible.Panel>
    </Collapsible.Root>
  )
}

function StatusIcon({ status }: { status: TodoItem['status'] }) {
  if (status === 'completed')
    return <CircleCheck aria-label="Done" className="size-3 shrink-0 text-muted-foreground" />
  if (status === 'in_progress')
    return (
      <Loader2
        aria-label="In progress"
        className="size-3 shrink-0 animate-spin text-sky-600 dark:text-sky-400"
      />
    )
  return <Circle aria-label="Pending" className="size-3 shrink-0 text-muted-foreground/50" />
}

function QueuedMessages({
  queued,
  error,
  onRemove,
}: {
  queued: ReturnType<typeof useQueuedMessages>
  error: string | null
  onRemove: (requestId: string) => void
}) {
  return (
    <div className="py-1">
      {error ? (
        <p role="alert" className="px-3 py-1 text-xs text-destructive-foreground">
          {error}
        </p>
      ) : null}
      <ul aria-label="Queued messages" className="flex flex-col">
        {queued.map((message) => {
          const steer = message.kind !== 'daemon_queued_end_of_loop'
          return (
            <li
              key={message.requestId}
              className="flex h-[30px] items-center gap-2 pr-1.5 pl-3 text-[12.5px] hover:bg-accent"
            >
              {steer ? (
                <CornerDownLeft aria-hidden className="size-3 shrink-0 text-muted-foreground" />
              ) : (
                <Clock aria-hidden className="size-3 shrink-0 text-muted-foreground" />
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
                onClick={() => onRemove(message.requestId)}
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
