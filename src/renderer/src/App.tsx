import { useMemo, useState } from 'react'
import { useConnectionState } from './daemon/connection-context'
import { groupByWorkspace, useSessionList } from './daemon/sessions'
import { ConnectionStatus, PairingFailed, ReconnectingBanner } from './components/connection-status'
import { SessionSidebar } from './components/sidebar/session-sidebar'
import { SessionView } from './components/chat/session-view'
import { NewSessionPage } from './components/new-session-page'
import { SettingsPage } from './components/settings-page'
import { Settings } from 'lucide-react'
import { Button } from './components/ui/button'
import { recentWorkspaces } from './daemon/use-new-session'
import { useHashRoute } from './lib/use-hash-route'

export function App() {
  const state = useConnectionState()
  const shellBridge = window.droiShell?.settings ?? null
  if (state.status === 'unpaired' && !shellBridge) return <PairingFailed reason={state.reason} />

  return (
    <div className="flex h-dvh flex-col bg-background text-foreground">
      <header className="flex h-10 shrink-0 items-center justify-between border-b px-3">
        <h1 className="text-sm font-semibold tracking-tight">Droi</h1>
        <div className="flex items-center gap-2">
          <ConnectionStatus />
          {shellBridge ? (
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label="Settings"
              onClick={() => {
                window.location.hash = '#/settings'
              }}
            >
              <Settings aria-hidden />
            </Button>
          ) : null}
        </div>
      </header>
      <ReconnectingBanner />
      <Workspace />
    </div>
  )
}

function Workspace() {
  const [route, navigate] = useHashRoute()
  const [showArchived, setShowArchived] = useState(false)
  const sessions = useSessionList({ includeArchived: showArchived })
  const groups = useMemo(() => groupByWorkspace(sessions.data ?? []), [sessions.data])
  const recent = useMemo(() => recentWorkspaces(sessions.data ?? []), [sessions.data])
  const selectedId = route.name === 'session' ? route.sessionId : null
  const selected = sessions.data?.find((s) => s.sessionId === selectedId) ?? null

  return (
    <div className="flex min-h-0 flex-1">
      <aside className="w-64 shrink-0 border-r">
        <SessionSidebar
          groups={groups}
          selectedSessionId={selectedId}
          onSelect={(sessionId) => navigate({ name: 'session', sessionId })}
          isLoading={sessions.isPending}
          error={sessions.error ? sessions.error.message : null}
          showArchived={showArchived}
          onToggleArchived={setShowArchived}
          onNewSession={() => navigate({ name: 'new' })}
        />
      </aside>
      <main className="min-w-0 flex-1">
        {route.name === 'settings' && window.droiShell ? (
          <SettingsPage bridge={window.droiShell.settings} />
        ) : route.name === 'new' ? (
          <NewSessionPage
            recent={recent}
            onCreated={(sessionId) => navigate({ name: 'session', sessionId })}
          />
        ) : selectedId ? (
          <SessionView
            key={selectedId}
            sessionId={selectedId}
            title={selected?.title ?? 'Session'}
            archived={Boolean(selected?.archivedAt)}
            onArchived={() => navigate({ name: 'home' })}
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Select a session to view its history.
          </div>
        )}
      </main>
    </div>
  )
}
