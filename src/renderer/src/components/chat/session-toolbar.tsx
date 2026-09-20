import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Archive, ArchiveRestore, Check, ChevronDown, Pencil, ShieldCheck, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  AUTONOMY_LEVELS,
  useSessionSettings,
  useSessionSettingsActions,
} from '@/daemon/use-session-settings'
import { cn } from '@/lib/utils'

const EFFORT_LABELS: Record<string, string> = {
  none: 'None',
  dynamic: 'Dynamic',
  off: 'Off',
  minimal: 'Minimal',
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'Extra high',
}

const AUTONOMY_LABELS: Record<string, string> = {
  off: 'Ask for everything',
  low: 'Low autonomy',
  medium: 'Medium autonomy',
  high: 'High autonomy',
}

/** Editable title for the page header. */
export function SessionTitle({
  sessionId,
  title,
}: {
  sessionId: string
  title: string
}): ReactNode {
  const actions = useSessionSettingsActions(sessionId)
  return <EditableTitle title={title} onRename={(next) => void actions.rename(next)} />
}

/** Archive / unarchive control for the page header. */
export function ArchiveButton({
  sessionId,
  archived,
  onArchived,
}: {
  sessionId: string
  archived: boolean
  onArchived: () => void
}) {
  const actions = useSessionSettingsActions(sessionId)
  return archived ? (
    <Button
      size="icon-sm"
      variant="ghost"
      aria-label="Unarchive session"
      onClick={() => void actions.unarchive()}
    >
      <ArchiveRestore aria-hidden />
    </Button>
  ) : (
    <Button
      size="icon-sm"
      variant="ghost"
      aria-label="Archive session"
      onClick={() => void actions.archive().then(onArchived)}
    >
      <Archive aria-hidden />
    </Button>
  )
}

/** Model, reasoning effort and autonomy for the composer's footer row. */
export function SessionSettingsBar({ sessionId }: { sessionId: string }) {
  const settings = useSessionSettings(sessionId)
  const actions = useSessionSettingsActions(sessionId)
  const model = settings.models.find((m) => m.id === settings.modelId)
  const efforts =
    model?.reasoningEfforts ?? (settings.reasoningEffort ? [settings.reasoningEffort] : [])

  return (
    <div className="relative flex min-w-0 flex-1 flex-wrap items-center gap-0.5">
      <Select
        label="Model"
        value={settings.modelId ?? ''}
        onChange={(value) => void actions.setModel(value)}
        options={settings.models.map((m) => ({
          value: m.id,
          label: m.label,
          disabled: m.disabled,
        }))}
      />
      <Select
        label="Reasoning effort"
        value={settings.reasoningEffort ?? ''}
        onChange={(value) => void actions.setReasoningEffort(value)}
        options={efforts.map((e) => ({ value: e, label: EFFORT_LABELS[e] ?? e }))}
      />
      <Select
        label="Autonomy"
        icon={<ShieldCheck aria-hidden className="size-3.5" />}
        value={settings.autonomyLevel ?? ''}
        onChange={(value) => void actions.setAutonomyLevel(value)}
        options={AUTONOMY_LEVELS.map((level) => ({
          value: level,
          label: AUTONOMY_LABELS[level] ?? level,
        }))}
      />
      {actions.error ? (
        <p
          role="alert"
          className="absolute bottom-full left-0 mb-2 rounded-md bg-destructive/10 px-2 py-1 text-xs text-destructive-foreground"
        >
          {actions.error}
        </p>
      ) : null}
    </div>
  )
}

/**
 * A native <select> dressed as a quiet text button: the current value reads
 * like a label, the list is the platform's own.
 */
function Select({
  label,
  icon,
  value,
  options,
  onChange,
}: {
  label: string
  icon?: ReactNode
  value: string
  options: Array<{ value: string; label: string; disabled?: boolean }>
  onChange: (value: string) => void
}) {
  const known = options.some((o) => o.value === value)
  const current = options.find((o) => o.value === value)?.label ?? value ?? label
  return (
    <span
      className={cn(
        'relative inline-flex h-7 max-w-52 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors',
        'hover:bg-muted hover:text-foreground has-[select:focus-visible]:ring-2 has-[select:focus-visible]:ring-ring/50',
        options.length === 0 && 'opacity-50',
      )}
    >
      {icon}
      <span className="truncate">{current || label}</span>
      <ChevronDown aria-hidden className="size-3 shrink-0 opacity-60" />
      <select
        aria-label={label}
        value={value}
        disabled={options.length === 0}
        onChange={(event) => onChange(event.target.value)}
        className="absolute inset-0 cursor-pointer opacity-0 outline-none"
      >
        {!known && value ? <option value={value}>{value}</option> : null}
        {options.map((option) => (
          <option key={option.value} value={option.value} disabled={option.disabled}>
            {option.label}
          </option>
        ))}
      </select>
    </span>
  )
}

function EditableTitle({ title, onRename }: { title: string; onRename: (title: string) => void }) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(title)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (editing) input.current?.select()
  }, [editing])

  if (!editing) {
    return (
      <div className="group flex min-w-0 items-center gap-1">
        <h2 className="truncate text-sm font-medium">{title}</h2>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Rename session"
          className="text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
          onClick={() => {
            setDraft(title)
            setEditing(true)
          }}
        >
          <Pencil aria-hidden />
        </Button>
      </div>
    )
  }

  const commit = () => {
    const next = draft.trim()
    setEditing(false)
    if (next && next !== title) onRename(next)
  }

  return (
    <form
      className="flex min-w-0 flex-1 items-center gap-1"
      onSubmit={(event) => {
        event.preventDefault()
        commit()
      }}
    >
      <input
        ref={input}
        aria-label="Session title"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') setEditing(false)
        }}
        className="h-7 min-w-0 flex-1 max-w-md rounded-md border bg-background px-2 text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      />
      <Button type="submit" size="icon-xs" variant="ghost" aria-label="Save title">
        <Check aria-hidden />
      </Button>
      <Button
        type="button"
        size="icon-xs"
        variant="ghost"
        aria-label="Cancel rename"
        onClick={() => setEditing(false)}
      >
        <X aria-hidden />
      </Button>
    </form>
  )
}
