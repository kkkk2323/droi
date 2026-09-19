import { useConnectionState } from './daemon/connection-context'
import { ConnectionStatus, PairingFailed, ReconnectingBanner } from './components/connection-status'

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
      <main className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        {state.status === 'connected' ? 'Connected to the Daemon.' : null}
      </main>
    </div>
  )
}
