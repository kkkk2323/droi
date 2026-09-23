import { useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import {
  ArrowUpRight,
  Bot,
  Check,
  ChevronRight,
  CircleDashed,
  CircleSlash,
  CircleX,
} from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import {
  formatRunDuration,
  subagentName,
  useSubagentLink,
  type TaskState,
} from '@droi/daemon-layer/subagents'
import type { ToolCall } from '@droi/daemon-layer/transcript'
import { cn } from '@/lib/utils'
import { Markdown } from './markdown'

const STATE_LABELS: Record<TaskState, string> = {
  pending: 'Starting',
  running: 'Running',
  launched: 'Started in background',
  completed: 'Completed',
  failed: 'Failed',
  cancelled: 'Cancelled',
}

/**
 * A Task call: work handed to a subagent. Unlike a tool row it names the
 * subagent and its task, says how the run went, and opens the subagent's
 * Session; the prompt and the report fold underneath.
 */
export function SubagentCard({ call }: { call: ToolCall }) {
  const [open, setOpen] = useState(false)
  const link = useSubagentLink(call)
  const name = subagentName(link.request.subagentType)
  const label = `${name}: ${link.request.description || 'Task'}`
  const facts = [
    link.run?.toolUseCount != null ? plural(link.run.toolUseCount, 'tool') : null,
    link.run?.durationMs != null ? formatRunDuration(link.run.durationMs) : null,
  ].filter(Boolean)

  return (
    <Collapsible.Root
      open={open}
      onOpenChange={setOpen}
      render={<div role="group" aria-label={label} />}
      className="my-2 overflow-hidden rounded-xl border bg-card/60"
    >
      <div className="flex items-center gap-2.5 py-2 pr-2 pl-2.5">
        <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
          <Bot aria-hidden className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="flex min-w-0 items-baseline gap-1.5 text-[13px]">
            <span className="shrink-0 font-medium">{name}</span>
            <span className="truncate text-muted-foreground">{link.request.description}</span>
          </p>
          <p className="flex items-center gap-1.5 text-[11.5px] text-muted-foreground">
            <StateMark state={link.state} />
            {facts.map((fact) => (
              <span key={fact} className="flex items-center gap-1.5 tabular-nums">
                <span aria-hidden>·</span>
                {fact}
              </span>
            ))}
          </p>
        </div>
        {link.open ? (
          <button
            type="button"
            aria-label="Open subagent session"
            onClick={link.open}
            className="flex h-7 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            Open
            <ArrowUpRight aria-hidden className="size-3.5" />
          </button>
        ) : null}
      </div>
      <Collapsible.Trigger className="group flex h-7 w-full items-center gap-1 border-t px-3 text-left text-[11.5px] text-muted-foreground outline-none transition-colors hover:bg-muted/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset">
        <ChevronRight
          aria-hidden
          className="size-3 transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
        Details
      </Collapsible.Trigger>
      <Collapsible.Panel className="max-h-96 overflow-auto border-t px-3 py-2.5">
        <Section title="Prompt">
          <p className="text-[12.5px] leading-5 whitespace-pre-wrap break-words">
            {link.request.prompt}
          </p>
        </Section>
        {link.report ? (
          <Section title="Report">
            <Markdown text={link.report} className="text-[13px] leading-6" />
          </Section>
        ) : null}
      </Collapsible.Panel>
    </Collapsible.Root>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section aria-label={title} className="mb-2 last:mb-0">
      <h4 className="mb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
        {title}
      </h4>
      {children}
    </section>
  )
}

function StateMark({ state }: { state: TaskState }) {
  const text = STATE_LABELS[state]
  const running = state === 'running' || state === 'pending'
  return (
    <span
      role={running ? 'status' : undefined}
      className={cn(
        'flex items-center gap-1',
        running && 'text-sky-600 dark:text-sky-400',
        state === 'completed' && 'text-emerald-600 dark:text-emerald-400',
        state === 'failed' && 'text-destructive-foreground',
      )}
    >
      {running ? (
        <Spinner aria-hidden className="size-3" />
      ) : state === 'completed' ? (
        <Check aria-hidden className="size-3" />
      ) : state === 'failed' ? (
        <CircleX aria-hidden className="size-3" />
      ) : state === 'cancelled' ? (
        <CircleSlash aria-hidden className="size-3" />
      ) : (
        <CircleDashed aria-hidden className="size-3" />
      )}
      {text}
    </span>
  )
}

function plural(count: number, word: string): string {
  return `${count} ${count === 1 ? word : `${word}s`}`
}
