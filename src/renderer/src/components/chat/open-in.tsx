import { Menu } from '@base-ui/react/menu'
import { useQuery } from '@tanstack/react-query'
import { Check, ChevronDown, FolderOpen } from 'lucide-react'
import type { OpenInApp, OpenInBridge } from '@shared/open-in'
import { usePreference } from '@droi/daemon-layer/local-preference'
import { openInApp } from '@/lib/local-preference'

/** The remembered app while it is still installed, otherwise Finder, otherwise the first one. */
export function preferredApp(
  apps: readonly OpenInApp[],
  remembered: string | null,
): OpenInApp | null {
  return (
    apps.find((app) => app.id === remembered) ??
    apps.find((app) => app.id === 'finder') ??
    apps[0] ??
    null
  )
}

/**
 * Waku's split "open in" control: the icon opens the Workspace in the
 * preferred app, the chevron lists every installed one and remembers the
 * pick. Only the Local Client has a bridge; nothing shows without one.
 */
export function OpenInButton({
  path,
  bridge,
}: {
  path: string | null
  bridge: OpenInBridge | null
}) {
  const apps = useQuery({
    queryKey: ['open-in-apps'],
    enabled: bridge !== null,
    staleTime: Infinity,
    queryFn: () => bridge!.list(),
  })
  const [remembered, remember] = usePreference(openInApp)
  const preferred = preferredApp(apps.data ?? [], remembered)
  if (!bridge || !path || !preferred) return null

  const open = (app: OpenInApp) => {
    remember(app.id)
    void bridge.open(path, app.id).catch(console.error)
  }

  return (
    <div className="app-no-drag flex h-7 items-center rounded-md border">
      <button
        type="button"
        aria-label={`Open in ${preferred.label}`}
        title={`Open in ${preferred.label}`}
        onClick={() => open(preferred)}
        className="flex h-full items-center rounded-l-md px-1.5 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <AppIcon app={preferred} />
      </button>
      <div aria-hidden className="h-full w-px bg-border" />
      <Menu.Root>
        <Menu.Trigger
          aria-label="Open in another app"
          title="Open in another app"
          className="flex h-full w-[18px] items-center justify-center rounded-r-md text-muted-foreground transition-colors outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:bg-accent"
        >
          <ChevronDown aria-hidden className="size-3" />
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner side="bottom" align="end" sideOffset={6} className="z-50 outline-none">
            <Menu.Popup
              aria-label="Open in"
              className="max-h-[min(22rem,var(--available-height))] min-w-44 overflow-y-auto rounded-lg border bg-popover p-1 text-sm text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none"
            >
              <Menu.RadioGroup value={preferred.id}>
                {apps.data?.map((app) => (
                  <Menu.RadioItem
                    key={app.id}
                    value={app.id}
                    closeOnClick
                    onClick={() => open(app)}
                    className="flex items-center gap-2 rounded-md py-1.5 pr-2 pl-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
                  >
                    <AppIcon app={app} />
                    <span className="min-w-0 flex-1 truncate">{app.label}</span>
                    <Menu.RadioItemIndicator className="flex size-4 items-center justify-center">
                      <Check aria-hidden className="size-3.5" />
                    </Menu.RadioItemIndicator>
                  </Menu.RadioItem>
                ))}
              </Menu.RadioGroup>
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
    </div>
  )
}

function AppIcon({ app }: { app: OpenInApp }) {
  return app.icon ? (
    <img alt="" src={app.icon} className="size-4 shrink-0" draggable={false} />
  ) : (
    <FolderOpen aria-hidden className="size-4 shrink-0 text-muted-foreground" />
  )
}
