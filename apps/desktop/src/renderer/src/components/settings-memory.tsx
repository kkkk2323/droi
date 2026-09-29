import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, FolderOpen, RotateCcw } from 'lucide-react'
import { pickableModels } from '@droi/daemon-layer/session-defaults'
import { useSessionDefaults } from '@droi/daemon-layer/use-session-defaults'
import { ModelPicker } from '@/components/chat/model-picker'
import { Button } from '@/components/ui/button'
import { SettingRow, Switch } from '@/components/ui/setting-row'
import { Spinner } from '@/components/ui/spinner'
import type { MemoryBridge, MemoryRow } from '@shared/memory'
import type { ShellSettingsBridge, ShellSettingsSnapshot } from '@shared/shell-settings'

export const MEMORY_OVERVIEW_KEY = ['memory-overview'] as const

type Props = {
  snapshot: ShellSettingsSnapshot
  bridge: ShellSettingsBridge
  onSaved: (s: ShellSettingsSnapshot) => void
}

/**
 * Memory (ADR 0010): the Desktop Shell attaches it to its own Daemon only, so
 * the switch lives here and turning it either way restarts the Daemon.
 */
export function MemoryTab({ memory, ...props }: Props & { memory: MemoryBridge }) {
  return (
    <>
      <MemorySwitchRow {...props} />
      <MemoryModelRow {...props} />
      <MemoriesSection memory={memory} settings={props.bridge} />
    </>
  )
}

/** Shared with the corner card, which reads the same overview. */
export function useMemoryOverview(
  memory: MemoryBridge,
  settings: ShellSettingsBridge,
  enabled = true,
) {
  const queryClient = useQueryClient()
  useEffect(
    () =>
      settings.onChange(
        () => void queryClient.invalidateQueries({ queryKey: MEMORY_OVERVIEW_KEY }),
      ),
    [settings, queryClient],
  )
  // Sessions write to Memory in their own processes; nothing tells the Shell.
  return useQuery({
    queryKey: MEMORY_OVERVIEW_KEY,
    queryFn: () => memory.overview(),
    enabled,
    refetchInterval: 60_000,
  })
}

function MemoriesSection({
  memory,
  settings,
}: {
  memory: MemoryBridge
  settings: ShellSettingsBridge
}) {
  const overview = useMemoryOverview(memory, settings)
  const [reset, setReset] = useState(false)
  const rows = overview.data?.rows ?? []
  return (
    <section aria-labelledby="memories-heading" className="flex flex-col gap-3">
      <h3 id="memories-heading" className="mt-3 px-1 text-xs font-medium text-muted-foreground">
        Memories on this computer
      </h3>
      {overview.error ? (
        <p role="alert" className="text-sm text-destructive-foreground">
          Memory did not load: {overview.error.message}
        </p>
      ) : !overview.data ? (
        <p className="text-sm text-muted-foreground">Loading Memory…</p>
      ) : (
        <ul aria-label="Memories" className="flex flex-col gap-3">
          {rows.map((row) => (
            <MemoryRowItem key={row.workspace ?? ''} row={row} memory={memory} />
          ))}
        </ul>
      )}
      <div className="flex flex-wrap gap-2 px-1">
        <Button type="button" variant="outline" size="sm" onClick={() => void memory.openFolder()}>
          <FolderOpen aria-hidden />
          Open memory folder
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => void memory.resetPrompts().then(() => setReset(true))}
        >
          <RotateCcw aria-hidden />
          Reset prompts to default
        </Button>
        {reset ? (
          <p role="status" className="self-center text-xs text-muted-foreground">
            The consolidation and extraction prompts are back to Droi’s.
          </p>
        ) : null}
      </div>
    </section>
  )
}

function formatChars(chars: number): string {
  return chars < 1_000 ? String(chars) : `${Math.round(chars / 1_000)}k`
}

function MemoryRowItem({ row, memory }: { row: MemoryRow; memory: MemoryBridge }) {
  const queryClient = useQueryClient()
  const [result, setResult] = useState<string | null>(null)
  const name = row.workspace ?? 'Global Memory'
  const consolidate = async () => {
    setResult(null)
    try {
      const { applied, rejected } = await memory.consolidate(row.workspace)
      setResult(
        rejected === 0
          ? `Consolidated ${applied} ${applied === 1 ? 'category' : 'categories'}.`
          : `Consolidated ${applied}; ${rejected} left unchanged because the model’s answer did not hold up.`,
      )
    } catch (error) {
      setResult(error instanceof Error ? error.message : String(error))
    } finally {
      void queryClient.invalidateQueries({ queryKey: MEMORY_OVERVIEW_KEY })
    }
  }
  return (
    <li aria-label={name}>
      <SettingRow
        title={
          row.workspace ? (
            <span className="block truncate font-mono text-[13px]" title={row.workspace}>
              {row.workspace}
            </span>
          ) : (
            'Global Memory'
          )
        }
        description={
          <>
            {row.entries} {row.entries === 1 ? 'entry' : 'entries'} · {formatChars(row.chars)} of{' '}
            {formatChars(row.softLimit)} characters ·{' '}
            {row.lastConsolidated
              ? `consolidated ${new Date(row.lastConsolidated).toLocaleDateString()}`
              : 'never consolidated'}
            {row.overSoftLimit ? (
              <span className="mt-1 flex items-center gap-1 text-attention">
                <CircleAlert aria-hidden className="size-3.5" />
                Over its size; consolidate it to keep writes going.
              </span>
            ) : null}
          </>
        }
        control={
          <Button
            type="button"
            size="sm"
            variant={row.overSoftLimit ? 'default' : 'outline'}
            disabled={row.consolidating || row.entries < 2}
            aria-label={`Consolidate ${name}`}
            onClick={() => void consolidate()}
          >
            {row.consolidating ? <Spinner aria-hidden /> : null}
            {row.consolidating ? 'Consolidating…' : 'Consolidate'}
          </Button>
        }
      >
        {result ? (
          <p role="status" className="text-xs text-muted-foreground">
            {result}
          </p>
        ) : null}
      </SettingRow>
    </li>
  )
}

function MemorySwitchRow({ snapshot, bridge, onSaved }: Props) {
  // The switch asks first: a restart stops every turn that is running.
  const [pending, setPending] = useState<boolean | null>(null)
  const [saving, setSaving] = useState(false)
  const apply = async (memoryEnabled: boolean) => {
    setSaving(true)
    try {
      onSaved(await bridge.update({ memoryEnabled }))
      setPending(null)
    } finally {
      setSaving(false)
    }
  }
  return (
    <SettingRow
      title={
        <span className="flex items-center gap-2">
          Memory
          <span className="rounded-full border px-1.5 text-[11px] font-normal text-muted-foreground">
            Beta
          </span>
        </span>
      }
      description={
        snapshot.memoryEnabled
          ? 'On: Droid can record and search what it learns about you and each Workspace, and recalls your corrections in every new Session. Only Droi’s Daemon has it; the droid CLI is untouched.'
          : 'Off: Droid starts every Session with no recollection of earlier ones. Turn it on to let it remember preferences, conventions and past mistakes on this computer.'
      }
      control={
        // The switch keeps reporting the setting as it is until the restart is
        // confirmed; the question below carries the requested state.
        <Switch
          aria-label="Memory"
          checked={snapshot.memoryEnabled}
          disabled={saving}
          onCheckedChange={(next) => setPending(next === snapshot.memoryEnabled ? null : next)}
        />
      }
    >
      {pending !== null ? (
        <div
          role="group"
          aria-label={pending ? 'Turn Memory on' : 'Turn Memory off'}
          aria-live="polite"
          className="flex flex-wrap items-center gap-2"
        >
          <p className="min-w-48 flex-1 text-[13px] text-muted-foreground">
            Turn Memory {pending ? 'on' : 'off'}? The Daemon restarts to{' '}
            {pending ? 'attach' : 'detach'} it. Sessions that are working stop.
          </p>
          <Button size="sm" disabled={saving} onClick={() => void apply(pending)}>
            Restart Daemon
          </Button>
          <Button size="sm" variant="ghost" disabled={saving} onClick={() => setPending(null)}>
            Cancel
          </Button>
        </div>
      ) : null}
    </SettingRow>
  )
}

function MemoryModelRow({ snapshot, bridge, onSaved }: Props) {
  const { models } = useSessionDefaults()
  return (
    <SettingRow
      title="Memory model"
      description="Runs Droi’s own Memory work: extracting entries from a finished Session and consolidating a Memory that grew large. A cheap model is enough."
      control={
        <ModelPicker
          field
          label="Memory model"
          value={snapshot.memoryModel}
          placeholder={snapshot.memoryModel}
          onChange={(memoryModel) => void bridge.update({ memoryModel }).then(onSaved)}
          models={pickableModels(models, { routers: false })}
        />
      }
    />
  )
}
