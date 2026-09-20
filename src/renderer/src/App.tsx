import { useMemo, useState } from 'react'
import { Dialog } from '@base-ui/react/dialog'
import { PanelLeft } from 'lucide-react'
import { useConnectionState } from './daemon/connection-context'
import { groupByWorkspace, useSessionList } from './daemon/sessions'
import { recentWorkspaces } from './daemon/use-new-session'
import { PairingFailed, ReconnectingBanner } from './components/connection-status'
import { PageHeader } from './components/page-header'
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

  if (route.name === 'settings' && window.droiShell) {
    return <SettingsPage bridge={window.droiShell.settings} onBack={() => go({ name: 'home' })} />
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
      onSettings={hasShellBridge ? () => go({ name: 'settings' }) : null}
      // Traffic lights sit over the sidebar's top strip on macOS.
      insetTop={hasShellBridge && !narrow}
    />
  )

  const menuButton = narrow ? (
    <Button
      size="icon-sm"
      variant="ghost"
      aria-label="Open sessions"
      aria-expanded={drawerOpen}
      onClick={() => setDrawerOpen(true)}
    >
      <PanelLeft aria-hidden />
    </Button>
  ) : null

  return (
    <div className="flex h-dvh overflow-hidden bg-sidebar text-foreground">
      {narrow ? (
        <Dialog.Root open={drawerOpen} onOpenChange={setDrawerOpen}>
          <Dialog.Portal>
            <Dialog.Backdrop className="fixed inset-0 z-40 bg-black/30 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
            <Dialog.Popup
              aria-label="Sessions"
              className="fixed inset-y-0 left-0 z-50 flex w-[85vw] max-w-72 flex-col bg-sidebar pb-[env(safe-area-inset-bottom)] pt-[env(safe-area-inset-top)] shadow-xl outline-none transition-transform duration-200 ease-out data-[ending-style]:-translate-x-full data-[starting-style]:-translate-x-full"
            >
              <Dialog.Title className="sr-only">Sessions</Dialog.Title>
              <div className="min-h-0 flex-1">{sidebar}</div>
            </Dialog.Popup>
          </Dialog.Portal>
        </Dialog.Root>
      ) : (
        <aside className="w-60 shrink-0">{sidebar}</aside>
      )}
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-background md:border-l">
        <ReconnectingBanner />
        {route.name === 'new' ? (
          <NewSessionPage
            recent={recent}
            onCreated={(sessionId) => go({ name: 'session', sessionId })}
            header={<PageHeader leading={menuButton} title="New session" />}
          />
        ) : selectedId ? (
          <SessionView
            key={selectedId}
            sessionId={selectedId}
            title={selected?.title ?? 'Session'}
            workspace={selected?.cwd ?? null}
            archived={Boolean(selected?.archivedAt)}
            onArchived={() => go({ name: 'home' })}
            leading={menuButton}
          />
        ) : (
          <>
            <PageHeader leading={menuButton} title="" />
            <div className="flex flex-1 items-center justify-center px-6 text-center text-sm text-muted-foreground">
              {narrow
                ? 'Open the sessions list to pick a session.'
                : 'Select a session, or start a new one.'}
            </div>
          </>
        )}
      </main>
    </div>
  )
}
