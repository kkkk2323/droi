import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDownToLine, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type {
  ShellSettingsBridge,
  ShellSettingsSnapshot,
  UpdateState,
} from '@shared/shell-settings'

const SETTINGS_KEY = ['shell-settings'] as const

/**
 * The About row's control: one button that walks the update through
 * check → download → restart, with the state written next to it.
 */
export function UpdateControl({
  update,
  bridge,
  onSaved,
}: {
  update: UpdateState
  bridge: ShellSettingsBridge
  onSaved: (snapshot: ShellSettingsSnapshot) => void
}) {
  const [busy, setBusy] = useState(false)
  const run = async (action: () => Promise<ShellSettingsSnapshot>) => {
    setBusy(true)
    try {
      onSaved(await action())
    } finally {
      setBusy(false)
    }
  }
  const working = busy || update.status === 'checking' || update.status === 'downloading'
  return (
    <div className="flex items-center gap-3">
      <span role="status" aria-label="Update status" className="text-xs text-muted-foreground">
        {describe(update)}
      </span>
      {update.status === 'available' ? (
        <Button size="sm" disabled={working} onClick={() => void run(() => bridge.installUpdate())}>
          <ArrowDownToLine aria-hidden />
          Update to {update.version}
        </Button>
      ) : update.status === 'ready' ? (
        <Button size="sm" onClick={() => void bridge.relaunch()}>
          Restart to update
        </Button>
      ) : (
        <Button
          size="sm"
          variant="outline"
          disabled={working}
          onClick={() => void run(() => bridge.checkForUpdate())}
        >
          {working ? <Loader2 aria-hidden className="animate-spin" /> : null}
          {update.status === 'error' ? 'Try again' : 'Check for updates'}
        </Button>
      )}
    </div>
  )
}

function describe(update: UpdateState): string {
  switch (update.status) {
    case 'idle':
      return ''
    case 'checking':
      return 'Checking…'
    case 'up-to-date':
      return 'Up to date'
    case 'available':
      return `${update.version} is available`
    case 'downloading':
      return `Downloading ${update.percent}%`
    case 'ready':
      return `${update.version} is installed`
    case 'error':
      return update.message
  }
}

/**
 * Local Client only: a line under the header once a Release is waiting or
 * has been installed. Says nothing while checking or after a failure; the
 * About row carries those.
 */
export function UpdateBanner({
  bridge,
  onOpenSettings,
}: {
  bridge: ShellSettingsBridge
  onOpenSettings: () => void
}) {
  const queryClient = useQueryClient()
  const settings = useQuery({ queryKey: SETTINGS_KEY, queryFn: () => bridge.get() })
  useEffect(
    () => bridge.onChange(() => void queryClient.invalidateQueries({ queryKey: SETTINGS_KEY })),
    [bridge, queryClient],
  )
  const update = settings.data?.update
  if (
    !update ||
    (update.status !== 'available' && update.status !== 'downloading' && update.status !== 'ready')
  ) {
    return null
  }
  return (
    <div role="status" className="flex items-center gap-3 border-b bg-card px-4 py-2 text-sm">
      <ArrowDownToLine aria-hidden className="size-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1">
        {update.status === 'ready'
          ? `Droi ${update.version} is installed and takes effect on the next launch.`
          : update.status === 'downloading'
            ? `Downloading Droi ${update.version}… ${update.percent}%`
            : `Droi ${update.version} is available.`}
      </span>
      {update.status === 'ready' ? (
        <Button size="sm" onClick={() => void bridge.relaunch()}>
          Restart now
        </Button>
      ) : update.status === 'available' ? (
        <>
          <Button size="sm" variant="ghost" onClick={onOpenSettings}>
            Details
          </Button>
          <Button
            size="sm"
            onClick={() =>
              void bridge
                .installUpdate()
                .then((next) => queryClient.setQueryData(SETTINGS_KEY, next))
            }
          >
            Update
          </Button>
        </>
      ) : null}
    </div>
  )
}
