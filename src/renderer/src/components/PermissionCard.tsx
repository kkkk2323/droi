import React from 'react'
import { ShieldAlert, FileCode, FileEdit, Play, Search, Globe, Terminal } from 'lucide-react'
import type { PendingPermissionRequest } from '@/state/appReducer'
import type { DroidPermissionOption } from '@/types'

type PermissionResponseParams = {
  selectedOption: DroidPermissionOption
  autoLevel?: 'low' | 'medium' | 'high'
}

function permissionLabel(opt: DroidPermissionOption): string {
  switch (opt) {
    case 'proceed_once':
      return 'Proceed once'
    case 'proceed_always':
      return 'Proceed always'
    case 'proceed_auto_run':
      return 'Auto-run'
    case 'proceed_auto_run_low':
      return 'Auto-run (Low)'
    case 'proceed_auto_run_medium':
      return 'Auto-run (Medium)'
    case 'proceed_auto_run_high':
      return 'Auto-run (High)'
    case 'proceed_edit':
      return 'Proceed edit'
    case 'cancel':
      return 'Cancel'
  }
}

function normalizePermissionToolName(value: unknown): string {
  const raw = typeof value === 'string' ? value.trim() : ''
  if (!raw) return ''
  const parts = raw.split('.')
  return parts[parts.length - 1] || raw
}

function extractPermissionToolName(item: unknown): string {
  const raw = (item as any)?.toolUse || item
  if (!raw || typeof raw !== 'object') return ''
  const directName = normalizePermissionToolName((raw as any).name)
  if (directName) return directName
  const toolName = normalizePermissionToolName((raw as any).toolName)
  if (toolName) return toolName
  const recipientName = normalizePermissionToolName((raw as any).recipient_name)
  if (recipientName) return recipientName
  return ''
}

function extractPermissionToolInput(item: unknown): Record<string, unknown> {
  const raw = (item as any)?.toolUse || item
  if (!raw || typeof raw !== 'object') return {}
  const input = (raw as any).input
  if (input && typeof input === 'object' && !Array.isArray(input))
    return input as Record<string, unknown>
  const parameters = (raw as any).parameters
  if (parameters && typeof parameters === 'object' && !Array.isArray(parameters))
    return parameters as Record<string, unknown>
  return {}
}

function parsePermissionToolUse(
  item: unknown,
): { name: string; input: Record<string, unknown> } | null {
  const name = extractPermissionToolName(item)
  if (!name) return null
  return { name, input: extractPermissionToolInput(item) }
}

function getToolUseIcon(name: string): React.ReactNode {
  if (/exit\s?spec/i.test(name)) return <FileCode className="size-3.5" />
  if (/edit|create|write|multiedit/i.test(name)) return <FileEdit className="size-3.5" />
  if (/execute/i.test(name)) return <Play className="size-3.5" />
  if (/read|glob|ls/i.test(name)) return <Search className="size-3.5" />
  if (/grep/i.test(name)) return <Search className="size-3.5" />
  if (/fetch|web/i.test(name)) return <Globe className="size-3.5" />
  return <Terminal className="size-3.5" />
}

function formatParamValue(value: unknown): string {
  if (typeof value === 'string') return value.length > 120 ? value.slice(0, 120) + '...' : value
  if (typeof value === 'boolean' || typeof value === 'number') return String(value)
  try {
    const s = JSON.stringify(value)
    return s.length > 120 ? s.slice(0, 120) + '...' : s
  } catch {
    return String(value)
  }
}

function PermissionToolUseCard({ item }: { item: unknown }) {
  const parsed = parsePermissionToolUse(item)
  if (!parsed) return null

  const name: string = parsed.name || 'Unknown'
  const input: Record<string, unknown> = parsed.input || {}

  if (/exit\s?spec/i.test(name)) return null

  if (/execute/i.test(name) && typeof input.command === 'string') {
    return (
      <div className="space-y-1.5">
        <div className="flex items-center gap-2">
          {getToolUseIcon(name)}
          <span className="text-xs font-medium text-foreground">Execute</span>
        </div>
        <pre className="whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5 text-zinc-300">
          <span className="text-zinc-500">$ </span>
          {String(input.command)}
        </pre>
      </div>
    )
  }

  if (/edit|create|write|multiedit/i.test(name)) {
    const filePath = typeof input.file_path === 'string' ? input.file_path : ''
    const fileName = filePath ? filePath.split('/').slice(-2).join('/') : ''
    return (
      <div className="space-y-1.5">
        <div className="flex items-center gap-2">
          {getToolUseIcon(name)}
          <span className="text-xs font-medium text-foreground">{name}</span>
          {fileName && (
            <span className="text-[11px] font-mono text-muted-foreground truncate">{fileName}</span>
          )}
        </div>
        {typeof input.old_str === 'string' && (
          <pre className="max-h-32 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5">
            {fileName && <div className="mb-1 text-zinc-500">{fileName}</div>}
            {String(input.old_str)
              .split('\n')
              .map((line, i) => (
                <div key={`o${i}`} className="text-red-400/80">
                  <span className="select-none text-red-600/50">- </span>
                  {line}
                </div>
              ))}
            {typeof input.new_str === 'string' &&
              String(input.new_str)
                .split('\n')
                .map((line, i) => (
                  <div key={`n${i}`} className="text-emerald-400/80">
                    <span className="select-none text-emerald-600/50">+ </span>
                    {line}
                  </div>
                ))}
          </pre>
        )}
        {typeof input.content === 'string' && !input.old_str && (
          <pre className="max-h-32 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-zinc-50 dark:bg-zinc-900 px-3 py-2 text-[11px] leading-5 text-zinc-700 dark:text-zinc-300">
            {input.content.length > 300 ? input.content.slice(0, 300) + '...' : input.content}
          </pre>
        )}
      </div>
    )
  }

  const entries = Object.entries(input).filter(([, v]) => v !== undefined && v !== null && v !== '')
  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-2">
        {getToolUseIcon(name)}
        <span className="text-xs font-medium text-foreground">{name}</span>
      </div>
      {entries.length > 0 ? (
        <div className="rounded-md bg-zinc-50 dark:bg-zinc-900 px-3 py-2 space-y-1">
          {entries.map(([key, val]) => (
            <div key={key} className="flex gap-2 text-[11px]">
              <span className="shrink-0 text-muted-foreground">{key}:</span>
              <span className="text-zinc-700 dark:text-zinc-300 break-all">
                {formatParamValue(val)}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <div className="rounded-md bg-zinc-50 dark:bg-zinc-900 px-3 py-2 text-[11px] text-muted-foreground">
          No parameters
        </div>
      )}
    </div>
  )
}

interface PermissionCardProps {
  request: PendingPermissionRequest
  onRespond: (params: PermissionResponseParams) => void
}

export function PermissionCard({ request, onRespond }: PermissionCardProps) {
  const permissionToolUses = Array.isArray(request.toolUses) ? request.toolUses : []

  return (
    <footer className="shrink-0 px-4 pb-4">
      <div className="mx-auto flex max-w-3xl flex-col rounded-2xl border border-amber-400/50 bg-card shadow-sm overflow-hidden max-h-[70vh]">
        <div className="flex items-center gap-2 px-4 !py-3">
          <ShieldAlert className="size-4 shrink-0 text-amber-500" />
          <span className="text-xs font-medium text-foreground">Permission required</span>
          <span className="ml-auto text-[11px] text-muted-foreground">
            Droid is requesting permission to use tools.
          </span>
        </div>

        {permissionToolUses.length > 0 && (
          <div className="min-h-0 flex-1 overflow-auto px-4 pb-2 space-y-3">
            {permissionToolUses.map((item, i) => (
              <PermissionToolUseCard key={i} item={item} />
            ))}
          </div>
        )}

        <div className="shrink-0 flex flex-wrap items-center gap-2 px-3 pb-2">
          {request.options.map((opt) => (
            <button
              key={opt}
              type="button"
              className={`rounded-lg px-3 py-1.5 text-xs transition-all active:scale-[0.98] ${
                opt === 'cancel'
                  ? 'text-destructive-foreground hover:bg-destructive/10'
                  : 'bg-foreground text-background hover:bg-foreground/80'
              }`}
              onClick={() => onRespond({ selectedOption: opt })}
            >
              {permissionLabel(opt)}
            </button>
          ))}
        </div>
      </div>
    </footer>
  )
}
