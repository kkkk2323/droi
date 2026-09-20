import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { ShellSettingsBridge } from '@shared/shell-settings'

const SETTINGS_KEY = ['shell-settings'] as const

/**
 * Local Client only: without a login or API key every authenticate fails, and
 * the Daemon's "sign in again" wording points the wrong way. Say what to do.
 */
export function SetupBanner({
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
  if (!settings.data || settings.data.hasCredential) return null
  return (
    <div role="alert" className="flex items-center gap-3 border-b bg-card px-4 py-2 text-sm">
      <KeyRound aria-hidden className="size-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1">
        Droi is not signed in to Factory yet, so it cannot talk to the Daemon.
      </span>
      <Button size="sm" onClick={onOpenSettings}>
        Sign in
      </Button>
    </div>
  )
}
