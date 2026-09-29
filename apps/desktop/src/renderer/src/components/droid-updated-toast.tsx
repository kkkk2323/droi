import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import type { ShellSettingsBridge } from '@shared/shell-settings'

const SETTINGS_KEY = ['shell-settings'] as const

/**
 * Local Client only: a corner card once `droid` updated itself while the
 * Daemon kept running the earlier build. Closing it hides it until the next
 * update; restarting the Daemon from here or from Settings clears it.
 */
export function DroidUpdatedToast({ bridge }: { bridge: ShellSettingsBridge }) {
  const queryClient = useQueryClient()
  const settings = useQuery({ queryKey: SETTINGS_KEY, queryFn: () => bridge.get() })
  useEffect(
    () => bridge.onChange(() => void queryClient.invalidateQueries({ queryKey: SETTINGS_KEY })),
    [bridge, queryClient],
  )
  const [dismissed, setDismissed] = useState(false)
  const [restarting, setRestarting] = useState(false)
  if (!settings.data?.droidUpdated) {
    if (dismissed) setDismissed(false)
    return null
  }
  if (dismissed) return null
  const restart = async () => {
    setRestarting(true)
    try {
      queryClient.setQueryData(SETTINGS_KEY, await bridge.restartDaemon())
    } finally {
      setRestarting(false)
    }
  }
  return (
    <div
      role="status"
      aria-label="droid update"
      className="animate-toast-in w-72 rounded-lg border bg-popover p-3 text-sm shadow-lg"
    >
      <div className="flex items-start gap-2.5">
        <RefreshCw aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <p className="font-medium">droid was updated</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Restart the Daemon to use the new version. Working Sessions are interrupted.
          </p>
          <div className="mt-2 flex items-center gap-1">
            <Button size="sm" disabled={restarting} onClick={() => void restart()}>
              {restarting ? <Spinner aria-hidden /> : null}
              Restart Daemon
            </Button>
          </div>
        </div>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Dismiss"
          className="-mr-1 -mt-1 shrink-0"
          onClick={() => setDismissed(true)}
        >
          <X aria-hidden />
        </Button>
      </div>
    </div>
  )
}
