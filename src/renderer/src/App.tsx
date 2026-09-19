import { useMemo, useState } from 'react'
import { Dialog } from '@base-ui/react/dialog'
import { PanelLeft, Settings } from 'lucide-react'
import { useConnectionState } from './daemon/connection-context'
import { groupByWorkspace, useSessionList } from './daemon/sessions'
import { recentWorkspaces } from './daemon/use-new-session'
import { ConnectionStatus, PairingFailed, ReconnectingBanner } from './components/connection-status'
import { SessionSidebar } from './components/sidebar/session-sidebar'
import { SessionView } from './components/chat/session-view'
import { NewSessionPage } from './components/new-session-page'
import { SettingsPage } from './components/settings-page'
import { Button } from './components/ui/button'
import { useHashRoute, type Route } from './lib/use-hash-route'
import { useMediaQuery } from './lib/use-media-query'

// Below this width the sidebar becomes a drawer and the conversation takes
// the whole screen. Matches Tailwind's `md`.
const NARROW = '(max-width: 767px)'

export function App() {
  const state = useConnectionState()
  const shellBridge = window.droiShell?.settings ?? null
  if (state.status === 'unpaired' && !shellBridge) return <PairingFailed reason={state.reason} />
  return <Shell hasShellBridge={shellBridge !== null} />
}

function Shell({ hasShellBridge }: { hasShellBridge: boolean }) {
  const [route, navigate] = useHashRoute()
  const narrow = useMediaQuery(NARROW)
  const [drawerRequested, setDrawerOpen] = useState(false)
  // A drawer only exists on narrow screens; widening the window closes it.
  const drawerOpen = narrow && drawerRequested
  const [showArchived, setShowArchived] = useState(false)
  const sessions = useSessionList({ includeArchived: showArchived })
  const groups = useMemo(() => groupByWorkspace(sessions.data ?? []), [sessions.data])
  const recent = useMemo(() => recentWorkspaces(sessions.data ?? []), [sessions.data])
  const selectedId = route.name === 'session' ? route.sessionId : null
  const selected = sessions.data?.find((s) => s.sessionId === selectedId) ?? null

  // Picking anything in the drawer is the end of the drawer's job.
  const go = (next: Route) => {
    navigate(next)
    setDrawerOpen(false)
  }

  const sidebar = (
    <SessionSidebar
      groups={groups}
      selectedSessionId={selectedId}
      onSelect={(sessionId) => go({ name: 'session', sessionId })}
      isLoading={sessions.isPending}
      error={sessions.error ? sessions.error.message : null}
      showArchived={showArchived}
      onToggleArchived={setShowArchived}
      onNewSession={() => go({ name: 'new' })}
    />
  )

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-background text-foreground">
      <header className="flex h-10 shrink-0 items-center justify-between gap-2 border-b px-2 pt-[env(safe-area-inset-top)] md:px-3">
        <div className="flex min-w-0 items-center gap-1">
          {narrow ? (
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label="Open sessions"
              aria-expanded={drawerOpen}
              onClick={() => setDrawerOpen(true)}
            >
              <PanelLeft aria-hidden />
            </Button>
          ) : null}
          <h1 className="truncate text-sm font-semibold tracking-tight">Droi</h1>
        </div>
        <div className="flex items-center gap-2">
          <ConnectionStatus />
          {hasShellBridge ? (
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label="Settings"
              onClick={() => go({ name: 'settings' })}
            >
              <Settings aria-hidden />
            </Button>
          ) : null}
        </div>
      </header>
      <ReconnectingBanner />
      <div className="flex min-h-0 flex-1">
        {narrow ? (
          <Dialog.Root open={drawerOpen} onOpenChange={setDrawerOpen}>
            <Dialog.Portal>
              <Dialog.Backdrop className="fixed inset-0 z-40 bg-black/50 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
              <Dialog.Popup
                aria-label="Sessions"
                className="fixed inset-y-0 left-0 z-50 flex w-[85vw] max-w-72 flex-col bg-sidebar pb-[env(safe-area-inset-bottom)] pt-[env(safe-area-inset-top)] shadow-xl outline-none transition-transform duration-200 ease-out data-[ending-style]:-translate-x-full data-[starting-style]:-translate-x-full"
              >
                <div className="flex h-10 shrink-0 items-center justify-between border-b px-3">
                  <Dialog.Title className="text-sm font-semibold">Sessions</Dialog.Title>
                  <Dialog.Close
                    aria-label="Close sessions"
                    className="rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted"
                  >
                    Close
                  </Dialog.Close>
                </div>
                <div className="min-h-0 flex-1">{sidebar}</div>
              </Dialog.Popup>
            </Dialog.Portal>
          </Dialog.Root>
        ) : (
          <aside className="w-64 shrink-0 border-r">{sidebar}</aside>
        )}
        <main className="min-w-0 flex-1">
          {route.name === 'settings' && window.droiShell ? (
            <SettingsPage bridge={window.droiShell.settings} />
          ) : route.name === 'new' ? (
            <NewSessionPage
              recent={recent}
              onCreated={(sessionId) => go({ name: 'session', sessionId })}
            />
          ) : selectedId ? (
            <SessionView
              key={selectedId}
              sessionId={selectedId}
              title={selected?.title ?? 'Session'}
              archived={Boolean(selected?.archivedAt)}
              onArchived={() => go({ name: 'home' })}
            />
          ) : (
            <div className="flex h-full items-center justify-center px-6 text-center text-sm text-muted-foreground">
              {narrow
                ? 'Open the sessions list to pick a session.'
                : 'Select a session to view its history.'}
            </div>
          )}
        </main>
      </div>
    </div>
  )
}
