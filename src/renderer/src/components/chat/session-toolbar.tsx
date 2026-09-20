import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Archive, ArchiveRestore, Check, Pencil, ShieldCheck, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select, type SelectGroup } from '@/components/ui/select'
import {
  AUTONOMY_LEVELS,
  useSessionSettings,
  useSessionSettingsActions,
  type ModelChoice,
} from '@/daemon/use-session-settings'

const PROVIDER_LABELS: Record<string, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  google: 'Google',
  xai: 'xAI',
  factory: 'Factory',
  'generic-chat-completion-api': 'Custom',
}

/** Models grouped by provider, in the Daemon's order; the Auto router leads. */
export function groupModels(models: ModelChoice[]): SelectGroup[] {
  const groups = new Map<string, SelectGroup>()
  for (const model of models) {
    const key = model.provider ?? ''
    let group = groups.get(key)
    if (!group) {
      const label = model.provider === null ? 'Auto' : (PROVIDER_LABELS[key] ?? capitalize(key))
      group = { label, options: [] }
      groups.set(key, group)
    }
    group.options.push({ value: model.id, label: model.label, disabled: model.disabled })
  }
  return [...groups.values()]
}

function capitalize(word: string): string {
  return word.charAt(0).toUpperCase() + word.slice(1)
}

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
        quiet
        label="Model"
        value={settings.modelId ?? ''}
        onChange={(value) => void actions.setModel(value)}
        groups={groupModels(settings.models)}
      />
      <Select
        quiet
        label="Reasoning effort"
        value={settings.reasoningEffort ?? ''}
        onChange={(value) => void actions.setReasoningEffort(value)}
        options={efforts.map((e) => ({ value: e, label: EFFORT_LABELS[e] ?? e }))}
      />
      <Select
        quiet
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
