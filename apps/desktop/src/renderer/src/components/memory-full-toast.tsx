import { useQuery } from '@tanstack/react-query'
import { Brain, X } from 'lucide-react'
import { usePreference } from '@droi/daemon-layer/local-preference'
import { Button } from '@/components/ui/button'
import { useMemoryOverview } from '@/components/settings-memory'
import { largeMemoriesNoticed } from '@/lib/local-preference'
import type { MemoryBridge } from '@shared/memory'
import type { ShellSettingsBridge } from '@shared/shell-settings'

/**
 * Local Client only: a corner card, once per crossing, when a Project Memory
 * grows past its soft limit. Past the hard limit Droid can no longer write to
 * it, so the card points at Settings → Memory before that happens.
 */
export function MemoryFullToast({
  memory,
  settings,
  onOpenSettings,
}: {
  memory: MemoryBridge
  settings: ShellSettingsBridge
  onOpenSettings: () => void
}) {
  // The Memory folder is left alone until Memory is turned on.
  const snapshot = useQuery({ queryKey: ['shell-settings'], queryFn: () => settings.get() })
  const overview = useMemoryOverview(memory, settings, snapshot.data?.memoryEnabled === true)
  const [noticed, setNoticed] = usePreference(largeMemoriesNoticed)
  const rows = overview.data?.rows
  if (!rows || !snapshot.data?.memoryEnabled) return null
  // Consolidating is how a Memory shrinks; a new consolidation makes a new key,
  // so a Memory that grows large again is announced again.
  const large = rows.flatMap((row) =>
    row.workspace && row.overSoftLimit ? [`${row.workspace}\n${row.lastConsolidated ?? ''}`] : [],
  )
  const key = large.find((k) => !noticed.includes(k))
  if (!key) return null
  const workspace = key.split('\n')[0]!
  const notice = () => setNoticed([...noticed.filter((k) => large.includes(k)), key])
  const name = workspace.split('/').filter(Boolean).at(-1) ?? workspace
  return (
    <div
      role="status"
      aria-label="Memory is large"
      className="animate-toast-in w-72 rounded-lg border bg-popover p-3 text-sm shadow-lg"
    >
      <div className="flex items-start gap-2.5">
        <Brain aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <p className="font-medium">{name}’s Memory is large</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Consolidate it before it fills up; a full Memory takes no new entries.
          </p>
          <div className="mt-2 flex items-center gap-1">
            <Button
              size="sm"
              onClick={() => {
                notice()
                onOpenSettings()
              }}
            >
              Open Memory settings
            </Button>
          </div>
        </div>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Dismiss"
          className="-mr-1 -mt-1 shrink-0"
          onClick={notice}
        >
          <X aria-hidden />
        </Button>
      </div>
    </div>
  )
}
