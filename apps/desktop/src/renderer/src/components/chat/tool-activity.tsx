import { useId, useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import {
  Check,
  ChevronRight,
  CircleX,
  FileEdit,
  FilePlus,
  FileText,
  FolderSearch,
  Globe,
  Search,
  Terminal,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/utils'
import {
  readToolResult,
  toolInputText,
  toolSummary,
  truncateLines,
  type DiffLine,
} from '@droi/daemon-layer/tool-calls'
import type { ToolCall } from '@droi/daemon-layer/transcript'

const ICONS: Record<string, LucideIcon> = {
  Execute: Terminal,
  Read: FileText,
  Edit: FileEdit,
  Create: FilePlus,
  ApplyPatch: FileEdit,
  Grep: Search,
  Glob: FolderSearch,
  LS: FolderSearch,
  FetchUrl: Globe,
  WebSearch: Globe,
}

const RESULT_PREVIEW_LINES = 40

/**
 * A run of tool calls as one quiet cluster: a one-line header that folds the
 * list, and compact rows that open to their input and result. A row spins
 * while its result is still to come.
 */
export function ToolCluster({ calls }: { calls: ToolCall[] }) {
  const [open, setOpen] = useState(true)
  const panelId = useId()
  const pending = calls.filter((c) => c.result === null).length
  const label =
    pending > 0
      ? `Running ${calls.length === 1 ? (calls[0]?.use.name ?? 'a tool') : `${calls.length} tools`}`
      : `Used ${calls.length === 1 ? (calls[0]?.use.name ?? 'a tool') : `${calls.length} tools`}`

  // A plain disclosure, not Collapsible: it opens without motion, and every
  // cluster mounts open, where Collapsible reads computed styles, forcing a
  // style pass per cluster as the transcript scrolls.
  return (
    <div className="my-2">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        data-panel-open={open ? '' : undefined}
        onClick={() => setOpen(!open)}
        className="group -ml-1.5 flex h-6 items-center gap-1.5 rounded-md px-1.5 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        {pending > 0 ? <Spinner aria-hidden className="size-3" /> : null}
        <span>{label}</span>
        <ChevronRight
          aria-hidden
          className="size-3 transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
      </button>
      {open ? (
        <div id={panelId} className="ml-1.5 mt-1 flex flex-col gap-0.5 border-l pl-3">
          {calls.map((call) => (
            <ToolRow key={call.use.id} call={call} />
          ))}
        </div>
      ) : null}
    </div>
  )
}

function ToolRow({ call }: { call: ToolCall }) {
  const [open, setOpen] = useState(false)
  const Icon = ICONS[call.use.name] ?? Wrench
  const summary = toolSummary(call)
  const { text: result, pending, isError, diff, status } = readToolResult(call)

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen}>
      <Collapsible.Trigger
        className="group -ml-1.5 flex h-7 w-full items-center gap-2 rounded-md px-1.5 text-left text-[12.5px] outline-none transition-colors hover:bg-muted/70 focus-visible:ring-2 focus-visible:ring-ring/50"
        aria-label={`${call.use.name}: ${summary}`}
      >
        <Icon
          aria-hidden
          className={cn(
            'size-3.5 shrink-0 text-muted-foreground',
            isError && 'text-destructive-foreground',
          )}
        />
        <span className={cn('shrink-0 font-medium', isError && 'text-destructive-foreground')}>
          {call.use.name}
        </span>
        <span className="min-w-0 flex-1 truncate font-mono text-[11.5px] text-muted-foreground">
          {summary}
        </span>
        {diff ? (
          <span className="shrink-0 font-mono text-[11px] tabular-nums">
            <span className="text-emerald-600 dark:text-emerald-400">+{diff.added}</span>{' '}
            <span className="text-rose-600 dark:text-rose-400">−{diff.removed}</span>
          </span>
        ) : null}
        {pending ? (
          <Spinner role="status" aria-label="Running" className="size-3.5 text-muted-foreground" />
        ) : (
          <>
            {isError ? (
              <CircleX
                role="img"
                aria-label="Failed"
                className="size-3.5 shrink-0 text-destructive-foreground"
              />
            ) : (
              <Check
                role="img"
                aria-label="Succeeded"
                className="size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400"
              />
            )}
            <ChevronRight
              aria-hidden
              className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-[opacity,transform] duration-150 group-hover:opacity-100 group-focus-visible:opacity-100 group-data-[panel-open]:rotate-90 group-data-[panel-open]:opacity-100"
            />
          </>
        )}
      </Collapsible.Trigger>
      <Collapsible.Panel className="mb-1 mt-0.5 overflow-hidden rounded-lg border bg-card/60">
        {diff ? (
          <>
            <div className="border-b px-2.5 py-1.5 font-mono text-[11.5px] leading-5 break-all">
              <ToolInput call={call} />
            </div>
            <DiffView lines={diff.lines} />
          </>
        ) : (
          <pre className="max-h-96 overflow-auto p-2.5 font-mono text-[11.5px] leading-5 whitespace-pre-wrap break-words">
            <ToolInput call={call} />
            {status ? (
              <span
                className={cn(
                  'mt-1 flex items-center gap-1.5',
                  status.success
                    ? 'text-emerald-600 dark:text-emerald-400'
                    : 'text-destructive-foreground',
                )}
              >
                {status.success ? (
                  <Check aria-hidden className="size-3.5" />
                ) : (
                  <CircleX aria-hidden className="size-3.5" />
                )}
                {status.message ?? (status.success ? 'Succeeded' : 'Failed')}
              </span>
            ) : result ? (
              <>
                {'\n'}
                <span
                  className={cn('text-muted-foreground', isError && 'text-destructive-foreground')}
                >
                  {truncateLines(result, RESULT_PREVIEW_LINES)}
                </span>
              </>
            ) : null}
          </pre>
        )}
      </Collapsible.Panel>
    </Collapsible.Root>
  )
}

function ToolInput({ call }: { call: ToolCall }) {
  return <span className="text-foreground">{toolInputText(call)}</span>
}

// The gutter stays put while the code scrolls under it, so it needs a solid
// background: the panel's own (card at 60% over the page) with the line's tint on top.
const PANEL_BACKGROUND = 'color-mix(in oklab, var(--card) 60%, var(--background))'
const LINE_TINT: Partial<Record<DiffLine['type'], string>> = {
  added: 'color-mix(in oklab, var(--color-emerald-500) 10%, transparent)',
  removed: 'color-mix(in oklab, var(--color-rose-500) 10%, transparent)',
}

function gutterBackground(type: DiffLine['type']): string {
  const tint = LINE_TINT[type]
  return tint ? `linear-gradient(${tint}, ${tint}), ${PANEL_BACKGROUND}` : PANEL_BACKGROUND
}

function DiffView({ lines }: { lines: DiffLine[] }) {
  const width = String(Math.max(1, ...lines.map((l) => Math.max(l.old ?? 0, l.new ?? 0)))).length
  return (
    <pre
      aria-label="Diff"
      className="max-h-96 overflow-auto py-1 font-mono text-[11.5px] leading-5"
    >
      {lines.map((line) => (
        <span
          key={`${line.type}:${line.old ?? ''}:${line.new ?? ''}`}
          data-type={line.type}
          className={cn(
            'flex min-w-max',
            line.type === 'added' && 'bg-emerald-500/10 text-emerald-800 dark:text-emerald-200',
            line.type === 'removed' && 'bg-rose-500/10 text-rose-800 dark:text-rose-200',
          )}
        >
          <span
            aria-hidden
            className="sticky left-0 z-10 flex shrink-0 select-none gap-1.5 pl-2.5 pr-2 text-muted-foreground/60 tabular-nums"
            style={{ background: gutterBackground(line.type) }}
          >
            <span className="text-right" style={{ minWidth: `${width}ch` }}>
              {line.old ?? ''}
            </span>
            <span className="text-right" style={{ minWidth: `${width}ch` }}>
              {line.new ?? ''}
            </span>
          </span>
          <span aria-hidden className="w-4 shrink-0 select-none">
            {line.type === 'added' ? '+' : line.type === 'removed' ? '−' : ' '}
          </span>
          <span className="whitespace-pre pr-3">{line.content}</span>
        </span>
      ))}
    </pre>
  )
}
