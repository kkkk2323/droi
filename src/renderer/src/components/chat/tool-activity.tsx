import { useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import {
  ChevronRight,
  FileEdit,
  FilePlus,
  FileText,
  FolderSearch,
  Globe,
  Loader2,
  Search,
  Terminal,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { toolResultText, type ToolCall } from './transcript'

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
  const pending = calls.filter((c) => c.result === null).length
  const label =
    pending > 0
      ? `Running ${calls.length === 1 ? (calls[0]?.use.name ?? 'a tool') : `${calls.length} tools`}`
      : `Used ${calls.length === 1 ? (calls[0]?.use.name ?? 'a tool') : `${calls.length} tools`}`

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="my-2">
      <Collapsible.Trigger className="group -ml-1.5 flex h-6 items-center gap-1.5 rounded-md px-1.5 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50">
        {pending > 0 ? <Loader2 aria-hidden className="size-3 animate-spin" /> : null}
        <span>{label}</span>
        <ChevronRight
          aria-hidden
          className="size-3 transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
      </Collapsible.Trigger>
      <Collapsible.Panel className="ml-1.5 mt-1 flex flex-col gap-0.5 border-l pl-3">
        {calls.map((call) => (
          <ToolRow key={call.use.id} call={call} />
        ))}
      </Collapsible.Panel>
    </Collapsible.Root>
  )
}

function ToolRow({ call }: { call: ToolCall }) {
  const [open, setOpen] = useState(false)
  const Icon = ICONS[call.use.name] ?? Wrench
  const summary = toolSummary(call)
  const result = toolResultText(call.result)
  const diff = parseDiffResult(result)
  const pending = call.result === null
  const isError = call.result?.isError === true

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
          <Loader2
            role="status"
            aria-label="Running"
            className="size-3.5 shrink-0 animate-spin text-muted-foreground"
          />
        ) : (
          <ChevronRight
            aria-hidden
            className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-[opacity,transform] duration-150 group-hover:opacity-100 group-focus-visible:opacity-100 group-data-[panel-open]:rotate-90 group-data-[panel-open]:opacity-100"
          />
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
            {result ? (
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
  const input = call.use.input
  const command = typeof input['command'] === 'string' ? input['command'] : null
  if (command) return <span className="text-foreground">$ {command}</span>
  const path =
    typeof input['file_path'] === 'string'
      ? input['file_path']
      : typeof input['path'] === 'string'
        ? input['path']
        : null
  if (path) return <span className="text-foreground">{path}</span>
  return <span className="text-foreground">{JSON.stringify(input, null, 2)}</span>
}

export interface DiffLine {
  type: 'unchanged' | 'added' | 'removed'
  content: string
  /** Line numbers in the old and new file; a removed line has no new one. */
  old: number | null
  new: number | null
}

export interface DiffResult {
  lines: DiffLine[]
  added: number
  removed: number
}

/**
 * The Daemon's Edit / Create results are JSON with `diffLines`; anything
 * else (plain text, errors, other tools) is shown as it came.
 */
export function parseDiffResult(text: string): DiffResult | null {
  if (!text.startsWith('{') || !text.includes('"diffLines"')) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null
  const raw = (parsed as { diffLines?: unknown }).diffLines
  if (!Array.isArray(raw)) return null
  const lines: DiffLine[] = []
  for (const entry of raw) {
    if (typeof entry !== 'object' || entry === null) continue
    const e = entry as { type?: unknown; content?: unknown; lineNumber?: unknown }
    if (e.type !== 'unchanged' && e.type !== 'added' && e.type !== 'removed') continue
    const numbers = (e.lineNumber ?? {}) as { old?: unknown; new?: unknown }
    lines.push({
      type: e.type,
      content: typeof e.content === 'string' ? e.content : '',
      old: typeof numbers.old === 'number' ? numbers.old : null,
      new: typeof numbers.new === 'number' ? numbers.new : null,
    })
  }
  return {
    lines,
    added: lines.filter((l) => l.type === 'added').length,
    removed: lines.filter((l) => l.type === 'removed').length,
  }
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
            className="sticky left-0 flex shrink-0 select-none gap-1.5 bg-inherit pl-2.5 pr-2 text-muted-foreground/60 tabular-nums"
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

export function toolSummary(call: ToolCall): string {
  const input = call.use.input
  for (const key of ['summary', 'command', 'file_path', 'path', 'pattern', 'url', 'query']) {
    const value = input[key]
    if (typeof value === 'string' && value.trim()) return firstLine(value)
  }
  return ''
}

function firstLine(text: string): string {
  const line = text.split('\n')[0] ?? ''
  return line.length > 120 ? `${line.slice(0, 117)}…` : line
}

function truncateLines(text: string, max: number): string {
  const lines = text.split('\n')
  if (lines.length <= max) return text
  return `${lines.slice(0, max).join('\n')}\n… ${lines.length - max} more lines`
}
