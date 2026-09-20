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

/** One tool call and its result as a card: a summary row, details on demand. */
export function ToolActivity({ call }: { call: ToolCall }) {
  const [open, setOpen] = useState(false)
  const Icon = ICONS[call.use.name] ?? Wrench
  const summary = toolSummary(call)
  const result = toolResultText(call.result)
  const pending = call.result === null
  const isError = call.result?.isError === true

  return (
    <Collapsible.Root
      open={open}
      onOpenChange={setOpen}
      className={cn(
        'my-2 overflow-hidden rounded-xl border bg-card/60 text-sm',
        isError && 'border-destructive/30',
      )}
    >
      <Collapsible.Trigger
        className="group flex w-full items-center gap-3 px-3 py-2 text-left outline-none transition-colors hover:bg-card focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
        aria-label={`${call.use.name}: ${summary}`}
      >
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
          {pending ? (
            <Loader2 aria-hidden className="size-3.5 animate-spin" />
          ) : (
            <Icon aria-hidden className="size-3.5" />
          )}
        </span>
        <span className="flex min-w-0 flex-1 items-baseline gap-2">
          <span className={cn('shrink-0 font-medium', isError && 'text-destructive-foreground')}>
            {call.use.name}
          </span>
          <span className="truncate font-mono text-xs text-muted-foreground">{summary}</span>
        </span>
        <ChevronRight
          aria-hidden
          className="size-4 shrink-0 text-muted-foreground transition-transform duration-150 group-data-[panel-open]:rotate-90"
        />
      </Collapsible.Trigger>
      <Collapsible.Panel className="border-t bg-background">
        <pre className="max-h-96 overflow-auto p-3 font-mono text-xs leading-5 whitespace-pre-wrap break-words">
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
