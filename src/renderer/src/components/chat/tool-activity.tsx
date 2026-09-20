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
