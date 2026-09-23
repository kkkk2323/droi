import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDownToLine, Loader2, RefreshCw, X } from 'lucide-react'
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
 * Local Client only: a small card in the bottom-left corner once a Release is
 * waiting, downloading or installed. Says nothing while checking or after a
 * failure; the About row carries those. Closing it hides that one step; the
 * card comes back when the update moves on (say, from downloading to ready).
 */
export function UpdateToast({
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
  const [dismissed, setDismissed] = useState<string | null>(null)
  const update = settings.data?.update
  if (
    !update ||
    (update.status !== 'available' && update.status !== 'downloading' && update.status !== 'ready')
  ) {
    return null
  }
  const step = `${update.status}:${update.version}`
  if (dismissed === step) return null
  return (
    <div
      role="status"
      aria-label="Update"
      className="animate-toast-in fixed bottom-4 left-4 z-50 w-72 rounded-lg border bg-popover p-3 text-sm shadow-lg"
    >
      <div className="flex items-start gap-2.5">
        {update.status === 'ready' ? (
          <RefreshCw aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        ) : (
          <ArrowDownToLine aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        )}
        <div className="min-w-0 flex-1">
          <p className="font-medium">
            {update.status === 'ready'
              ? `Droi ${update.version} is ready`
              : update.status === 'downloading'
                ? `Downloading Droi ${update.version}…`
                : `Droi ${update.version} is available`}
          </p>
          {update.status === 'downloading' ? (
            <div
              role="progressbar"
              aria-label="Download progress"
              aria-valuenow={update.percent}
              aria-valuemin={0}
              aria-valuemax={100}
              className="mt-2 h-1 w-full overflow-hidden rounded-full bg-muted"
            >
              <div
                className="h-full rounded-full bg-primary transition-[width]"
                style={{ width: `${update.percent}%` }}
              />
            </div>
          ) : (
            <div className="mt-2 flex items-center gap-1">
              {update.status === 'ready' ? (
                <Button size="sm" onClick={() => void bridge.relaunch()}>
                  Restart now
                </Button>
              ) : (
                <>
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
                  <Button size="sm" variant="ghost" onClick={onOpenSettings}>
                    Details
                  </Button>
                </>
              )}
            </div>
          )}
        </div>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Dismiss"
          className="-mr-1 -mt-1 shrink-0"
          onClick={() => setDismissed(step)}
        >
          <X aria-hidden />
        </Button>
      </div>
    </div>
  )
}
