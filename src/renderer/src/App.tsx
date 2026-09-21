import { useEffect, useRef, useState } from 'react'
import { Dialog } from '@base-ui/react/dialog'
import { PanelLeft } from 'lucide-react'
import { useConnectionState, useDaemonConnection } from './daemon/connection-context'
import { isStartingUp } from './daemon/connection'
import { foldContinued, groupByWorkspace, useSessionList, type SessionTag } from './daemon/sessions'

const NO_TAGS: SessionTag[] = []
import { recentWorkspaces } from './daemon/use-new-session'
import { useWorkingSessionIds } from './daemon/use-working-sessions'
import {
  ConnectionStatus,
  PairingFailed,
  ReconnectingBanner,
  StartingUp,
} from './components/connection-status'
import { PageHeader } from './components/page-header'
import { SessionSidebar } from './components/sidebar/session-sidebar'
import { SidebarToggle } from './components/sidebar-toggle'
import { SessionView } from './components/chat/session-view'
import { NewSessionPage } from './components/new-session-page'
import { SettingsPage } from './components/settings-page'
import { SetupBanner } from './components/setup-banner'
import { UpdateToast } from './components/update-control'
import { Button } from './components/ui/button'
import {
  lastSessionId,
  pinnedSessions,
  pinnedWorkspaces,
  showArchivedSessions,
  sidebarVisible,
  usePreference,
} from './lib/local-preference'
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
  const { controller } = useDaemonConnection()
  const startingUp = isStartingUp(useConnectionState())
  const narrow = useMediaQuery(NARROW)
  const [drawerRequested, setDrawerOpen] = useState(false)
  // A drawer only exists on narrow screens; widening the window closes it.
  const drawerOpen = narrow && drawerRequested
  const [sidebarShown, setSidebarShown] = usePreference(sidebarVisible)
  const [showArchived] = usePreference(showArchivedSessions)
  const sessions = useSessionList({ includeArchived: showArchived })
  const workingSessionIds = useWorkingSessionIds()
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  const [pinnedIds] = usePreference(pinnedSessions)
  const groups = groupByWorkspace(foldContinued(sessions.data ?? []), {
    workspaces: new Set(pinnedGroups),
    sessions: new Set(pinnedIds),
  })
  const recent = recentWorkspaces(sessions.data ?? [])
  const selectedId = route.name === 'session' ? route.sessionId : null
  const selected = sessions.data?.find((s) => s.sessionId === selectedId) ?? null
  const parent = sessions.data?.find((s) => s.sessionId === selected?.parentId) ?? null

  // Launching on the bare home route reopens the Session that was open last,
  // once the list confirms it still exists. Only the first list counts: going
  // home on purpose afterwards must stick.
  const [lastId] = usePreference(lastSessionId)
  const launchRoute = useRef(route)
  const restored = useRef(false)
  useEffect(() => {
    if (restored.current || !sessions.data) return
    restored.current = true
    if (launchRoute.current.name !== 'home' || !lastId) return
    if (sessions.data.some((s) => s.sessionId === lastId)) {
      navigate({ name: 'session', sessionId: lastId })
    }
  }, [sessions.data, lastId, navigate])
  useEffect(() => {
    if (selectedId) lastSessionId.set(selectedId)
  }, [selectedId])

  // ⌘B / Ctrl+B toggles the sidebar on wide screens, as the previous Droi did.
  useEffect(() => {
    if (narrow) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'b' && (event.metaKey || event.ctrlKey) && !event.altKey) {
        event.preventDefault()
        setSidebarShown(!sidebarVisible.get())
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [narrow, setSidebarShown])

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
      workingSessionIds={workingSessionIds}
      onSelect={(sessionId) => go({ name: 'session', sessionId })}
      onArchiveToggle={(session) => {
        if (session.archivedAt) {
          void controller.unarchiveSession(session.sessionId).catch(console.error)
          return
        }
        void controller
          .archiveSession(session.sessionId)
          .then(() => {
            if (session.sessionId === selectedId) {
              lastSessionId.set(null)
              go({ name: 'home' })
            }
          })
          .catch(console.error)
      }}
      isLoading={sessions.isPending}
      error={sessions.error ? sessions.error.message : null}
      onNewSession={() => go({ name: 'new' })}
      onNewSessionIn={(workspace) => go({ name: 'new', workspace })}
      onSettings={() => go({ name: 'settings' })}
      // Traffic lights sit over the sidebar's top strip on macOS.
      insetTop={hasShellBridge && !narrow}
    />
  )

  // Narrow: a button in the header that opens the drawer. Wide: the toggle is
  // pinned top-left and never moves; the sidebar slides under it, and the main
  // header grows a matching gap so its title clears the button.
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
  ) : (
    <div
      aria-hidden
      className={cn(
        'shrink-0 transition-[width] duration-200 ease-out motion-reduce:transition-none',
        sidebarShown ? 'w-0' : hasShellBridge ? 'w-[100px]' : 'w-8',
      )}
    />
  )

  return (
    <div className="relative flex h-full overflow-hidden bg-sidebar text-foreground">
      {status}
      {narrow ? (
        <Dialog.Root open={drawerOpen} onOpenChange={setDrawerOpen}>
          <Dialog.Portal>
            <Dialog.Backdrop className="fixed inset-0 z-40 bg-black/30 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
            <Dialog.Popup
              aria-label="Sessions"
              className="fixed inset-y-0 left-0 z-50 flex w-[85vw] max-w-72 flex-col bg-sidebar pb-[env(safe-area-inset-bottom)] shadow-xl outline-none transition-transform duration-200 ease-out data-[ending-style]:-translate-x-full data-[starting-style]:-translate-x-full"
            >
              <Dialog.Title className="sr-only">Sessions</Dialog.Title>
              <div className="min-h-0 flex-1">{sidebar}</div>
            </Dialog.Popup>
          </Dialog.Portal>
        </Dialog.Root>
      ) : (
        <aside
          id="sessions-sidebar"
          // Width clips the panel; the visibility flip waits for the width so the
          // slide is seen, and lands at once when opening.
          className={cn(
            'shrink-0 overflow-hidden transition-[width,visibility] duration-200 ease-out motion-reduce:transition-none',
            sidebarShown ? 'w-60' : 'invisible w-0',
          )}
        >
          <div className="h-full w-60">{sidebar}</div>
        </aside>
      )}
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-background md:border-l">
        {window.droiShell ? (
          <>
            <SetupBanner
              bridge={window.droiShell.settings}
              onOpenSettings={() => go({ name: 'settings' })}
            />
            <UpdateToast
              bridge={window.droiShell.settings}
              onOpenSettings={() => go({ name: 'settings' })}
            />
          </>
        ) : null}
        <ReconnectingBanner />
        {route.name === 'new' ? (
          <NewSessionPage
            key={route.workspace ?? ''}
            recent={recent}
            initialWorkspace={route.workspace ?? null}
            onCreated={(sessionId) => go({ name: 'session', sessionId })}
            header={<PageHeader leading={leading} title="New session" />}
          />
        ) : selectedId ? (
          <SessionView
            key={selectedId}
            sessionId={selectedId}
            title={selected?.title ?? 'Session'}
            workspace={selected?.cwd ?? null}
            tags={selected?.tags ?? NO_TAGS}
            parent={parent}
            onContinued={(sessionId) => go({ name: 'session', sessionId })}
            leading={leading}
          />
        ) : (
          <>
            <PageHeader leading={leading} title="" />
            <div className="flex flex-1 items-center justify-center px-6 text-center text-sm text-muted-foreground">
              {startingUp ? (
                <StartingUp />
              ) : narrow ? (
                'Open the sessions list to pick a session.'
              ) : (
                'Select a session, or start a new one.'
              )}
            </div>
          </>
        )}
      </main>
      {narrow ? null : (
        <div
          className={cn(
            // Floats over the window-drag strips. Electron folds drag and no-drag
            // rects in DOM order, so this must come after every strip it covers or
            // the strips win and the button never gets the click.
            'app-no-drag absolute top-0 z-20 flex h-[calc(env(safe-area-inset-top)+2.75rem)] items-center pt-[env(safe-area-inset-top)]',
            hasShellBridge ? 'left-[76px]' : 'left-2',
          )}
        >
          <SidebarToggle expanded={sidebarShown} onClick={() => setSidebarShown(!sidebarShown)} />
        </div>
      )}
    </div>
  )
}
