import { useMemo, useState } from 'react'
import { Dialog } from '@base-ui/react/dialog'
import { PanelLeft } from 'lucide-react'
import { useConnectionState } from './daemon/connection-context'
import { groupByWorkspace, useSessionList } from './daemon/sessions'
import { recentWorkspaces } from './daemon/use-new-session'
import { ConnectionStatus, PairingFailed, ReconnectingBanner } from './components/connection-status'
import { PageHeader } from './components/page-header'
import { SessionSidebar } from './components/sidebar/session-sidebar'
import { SidebarToggle } from './components/sidebar-toggle'
import { SessionView } from './components/chat/session-view'
import { NewSessionPage } from './components/new-session-page'
import { SettingsPage } from './components/settings-page'
import { SetupBanner } from './components/setup-banner'
import { Button } from './components/ui/button'
import { showArchivedSessions, sidebarVisible } from './lib/local-preference'
import { useHashRoute, type Route } from './lib/use-hash-route'
import { useMediaQuery } from './lib/use-media-query'
import { cn } from './lib/utils'

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
  const [sidebarShown, setSidebarShown] = sidebarVisible.use()
  const [showArchived] = showArchivedSessions.use()
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

  // The connection is announced, not shown: the banner below covers trouble.
  const status = (
    <div className="sr-only">
      <ConnectionStatus />
    </div>
  )

  if (route.name === 'settings') {
    return (
      <>
        {status}
        <SettingsPage
          bridge={window.droiShell?.settings ?? null}
          onBack={() => go({ name: 'home' })}
        />
      </>
    )
  }

  const sidebar = (
    <SessionSidebar
      groups={groups}
      selectedSessionId={selectedId}
      onSelect={(sessionId) => go({ name: 'session', sessionId })}
      isLoading={sessions.isPending}
      error={sessions.error ? sessions.error.message : null}
      onNewSession={() => go({ name: 'new' })}
      onSettings={() => go({ name: 'settings' })}
      onHide={narrow ? null : () => setSidebarShown(false)}
      // Traffic lights sit over the sidebar's top strip on macOS.
      insetTop={hasShellBridge && !narrow}
    />
  )

  // Narrow: a button that opens the drawer. Wide with the sidebar hidden: the
  // toggle moves into the header, clearing the traffic lights like the sidebar did.
  const leading = narrow ? (
    <Button
      size="icon-sm"
      variant="ghost"
      aria-label="Open sessions"
      aria-expanded={drawerOpen}
      onClick={() => setDrawerOpen(true)}
    >
      <PanelLeft aria-hidden />
    </Button>
  ) : !sidebarShown ? (
    <div className={cn('flex items-center', hasShellBridge && 'pl-[68px]')}>
      <SidebarToggle expanded={false} onClick={() => setSidebarShown(true)} />
    </div>
  ) : null

  return (
    <div className="flex h-dvh overflow-hidden bg-sidebar text-foreground">
      {status}
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
        <aside id="sessions-sidebar" hidden={!sidebarShown} className="w-60 shrink-0">
          {sidebar}
        </aside>
      )}
      <main
        className={cn(
          'flex min-w-0 flex-1 flex-col overflow-hidden bg-background',
          sidebarShown && 'md:border-l',
        )}
      >
        {window.droiShell ? (
          <SetupBanner
            bridge={window.droiShell.settings}
            onOpenSettings={() => go({ name: 'settings' })}
          />
        ) : null}
        <ReconnectingBanner />
        {route.name === 'new' ? (
          <NewSessionPage
            recent={recent}
            onCreated={(sessionId) => go({ name: 'session', sessionId })}
            header={<PageHeader leading={leading} title="New session" />}
          />
        ) : selectedId ? (
          <SessionView
            key={selectedId}
            sessionId={selectedId}
            title={selected?.title ?? 'Session'}
            workspace={selected?.cwd ?? null}
            archived={Boolean(selected?.archivedAt)}
            onArchived={() => go({ name: 'home' })}
            leading={leading}
          />
        ) : (
          <>
            <PageHeader leading={leading} title="" />
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
