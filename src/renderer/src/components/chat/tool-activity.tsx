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

/** One tool call and its result, collapsed to a single summary line. */
export function ToolActivity({ call }: { call: ToolCall }) {
  const [open, setOpen] = useState(false)
  const Icon = ICONS[call.use.name] ?? Wrench
  const summary = toolSummary(call)
  const result = toolResultText(call.result)
  const pending = call.result === null
  const isError = call.result?.isError === true

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="my-1.5">
      <Collapsible.Trigger
        className={cn(
          'group flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-xs text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
          isError && 'text-destructive-foreground',
        )}
        aria-label={`${call.use.name}: ${summary}`}
      >
        <ChevronRight
          aria-hidden
          className="size-3 shrink-0 transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
        {pending ? (
          <Loader2 aria-hidden className="size-3.5 shrink-0 animate-spin" />
        ) : (
          <Icon aria-hidden className="size-3.5 shrink-0" />
        )}
        <span className="font-medium text-foreground/90">{call.use.name}</span>
        <span className="truncate font-mono">{summary}</span>
      </Collapsible.Trigger>
      <Collapsible.Panel className="ml-[1.35rem] mt-1 overflow-hidden rounded-md border bg-muted/40 text-xs">
        <pre className="max-h-96 overflow-auto p-3 font-mono whitespace-pre-wrap break-words">
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
          ) : pending ? (
            <span className="text-muted-foreground">Running…</span>
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
