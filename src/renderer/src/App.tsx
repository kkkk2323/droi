import { useMemo } from 'react'
import { useConnectionState } from './daemon/connection-context'
import { groupByWorkspace, useSessionList } from './daemon/sessions'
import { ConnectionStatus, PairingFailed, ReconnectingBanner } from './components/connection-status'
import { SessionSidebar } from './components/sidebar/session-sidebar'
import { SessionView } from './components/chat/session-view'
import { useHashRoute } from './lib/use-hash-route'

export function App() {
  const state = useConnectionState()
  if (state.status === 'unpaired') return <PairingFailed reason={state.reason} />

  return (
    <div className="flex h-dvh flex-col bg-background text-foreground">
      <header className="flex h-10 shrink-0 items-center justify-between border-b px-3">
        <h1 className="text-sm font-semibold tracking-tight">Droi</h1>
        <ConnectionStatus />
      </header>
      <ReconnectingBanner />
      <Workspace />
    </div>
  )
}

function Workspace() {
  const [route, navigate] = useHashRoute()
  const sessions = useSessionList()
  const groups = useMemo(() => groupByWorkspace(sessions.data ?? []), [sessions.data])
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
        />
      </aside>
      <main className="min-w-0 flex-1">
        {selectedId ? (
          <SessionView
            key={selectedId}
            sessionId={selectedId}
            title={selected?.title ?? 'Session'}
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
