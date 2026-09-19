import { useEffect, useRef, useState } from 'react'
import { Archive, ArchiveRestore, Check, Pencil, X } from 'lucide-react'
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

/** Session header: editable title, per-Session settings, archive. */
export function SessionToolbar({
  sessionId,
  title,
  archived,
  onArchived,
}: {
  sessionId: string
  title: string
  archived: boolean
  onArchived: () => void
}) {
  const settings = useSessionSettings(sessionId)
  const actions = useSessionSettingsActions(sessionId)
  const model = settings.models.find((m) => m.id === settings.modelId)
  const efforts =
    model?.reasoningEfforts ?? (settings.reasoningEffort ? [settings.reasoningEffort] : [])

  return (
    <header className="relative flex min-h-10 shrink-0 flex-wrap items-center gap-x-2 gap-y-1 border-b px-3 py-1">
      <EditableTitle title={title} onRename={(next) => void actions.rename(next)} />
      <div className="ml-auto flex flex-wrap items-center gap-1.5">
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
          value={settings.autonomyLevel ?? ''}
          onChange={(value) => void actions.setAutonomyLevel(value)}
          options={AUTONOMY_LEVELS.map((level) => ({
            value: level,
            label: AUTONOMY_LABELS[level] ?? level,
          }))}
        />
        {archived ? (
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="Unarchive session"
            onClick={() => void actions.unarchive()}
          >
            <ArchiveRestore aria-hidden />
          </Button>
        ) : (
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="Archive session"
            onClick={() => void actions.archive().then(onArchived)}
          >
            <Archive aria-hidden />
          </Button>
        )}
      </div>
      {actions.error ? (
        <p
          role="alert"
          className="absolute right-3 top-11 rounded-md bg-destructive/10 px-2 py-1 text-xs text-destructive-foreground"
        >
          {actions.error}
        </p>
      ) : null}
    </header>
  )
}

function Select({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: Array<{ value: string; label: string; disabled?: boolean }>
  onChange: (value: string) => void
}) {
  const known = options.some((o) => o.value === value)
  return (
    <select
      aria-label={label}
      value={value}
      disabled={options.length === 0}
      onChange={(event) => onChange(event.target.value)}
      className={cn(
        'h-7 max-w-40 rounded-md border bg-background px-1.5 text-xs text-foreground outline-none',
        'hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-50',
      )}
    >
      {!known && value ? <option value={value}>{value}</option> : null}
      {options.map((option) => (
        <option key={option.value} value={option.value} disabled={option.disabled}>
          {option.label}
        </option>
      ))}
    </select>
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
      <div className="flex min-w-0 items-center gap-1">
        <h2 className="truncate text-sm font-medium">{title}</h2>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Rename session"
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
      className="flex min-w-0 items-center gap-1"
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
        className="h-7 min-w-0 flex-1 rounded-md border bg-background px-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
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
