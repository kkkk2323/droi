import { useState } from 'react'
import { pickableModels } from '@droi/daemon-layer/session-defaults'
import { useSessionDefaults } from '@droi/daemon-layer/use-session-defaults'
import { ModelPicker } from '@/components/chat/model-picker'
import { Button } from '@/components/ui/button'
import { SettingRow, Switch } from '@/components/ui/setting-row'
import type { ShellSettingsBridge, ShellSettingsSnapshot } from '@shared/shell-settings'

type Props = {
  snapshot: ShellSettingsSnapshot
  bridge: ShellSettingsBridge
  onSaved: (s: ShellSettingsSnapshot) => void
}

/**
 * Memory (ADR 0010): the Desktop Shell attaches it to its own Daemon only, so
 * the switch lives here and turning it either way restarts the Daemon.
 */
export function MemoryTab({ snapshot, bridge, onSaved }: Props) {
  return (
    <>
      <MemorySwitchRow snapshot={snapshot} bridge={bridge} onSaved={onSaved} />
      <MemoryModelRow snapshot={snapshot} bridge={bridge} onSaved={onSaved} />
    </>
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
        <Switch
          aria-label="Memory"
          checked={pending ?? snapshot.memoryEnabled}
          disabled={saving}
          onCheckedChange={(next) => setPending(next === snapshot.memoryEnabled ? null : next)}
        />
      }
    >
      {pending !== null ? (
        <div
          role="alertdialog"
          aria-label={pending ? 'Turn Memory on' : 'Turn Memory off'}
          className="flex flex-wrap items-center gap-2"
        >
          <p className="min-w-48 flex-1 text-[13px] text-muted-foreground">
            The Daemon restarts to {pending ? 'attach' : 'detach'} Memory. Sessions that are working
            stop.
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
