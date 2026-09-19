import { useConnectionState } from '../daemon/connection-context'
import type { ConnectionState } from '../daemon/connection'

const LABELS: Record<ConnectionState['status'], string> = {
  connecting: 'Connecting',
  connected: 'Connected',
  reconnecting: 'Reconnecting',
  unpaired: 'Not paired',
  unreachable: 'Daemon unreachable',
}

const DOT_CLASS: Record<ConnectionState['status'], string> = {
  connecting: 'bg-muted-foreground animate-pulse',
  connected: 'bg-emerald-500',
  reconnecting: 'bg-amber-500 animate-pulse',
  unpaired: 'bg-destructive-foreground',
  unreachable: 'bg-destructive-foreground',
}

/** Compact indicator for the header; the full-width banner lives elsewhere. */
export function ConnectionStatus() {
  const state = useConnectionState()
  return (
    <div
      role="status"
      aria-label="Connection"
      className="flex items-center gap-1.5 text-xs text-muted-foreground"
    >
      <span aria-hidden className={`size-1.5 rounded-full ${DOT_CLASS[state.status]}`} />
      <span>{LABELS[state.status]}</span>
    </div>
  )
}

/** Shown while the Client has lost the Daemon and is trying to get it back. */
export function ReconnectingBanner() {
  const state = useConnectionState()
  if (state.status !== 'reconnecting' && state.status !== 'unreachable') return null
  return (
    <div
      role="alert"
      className="flex items-center justify-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-200"
    >
      <span aria-hidden className="size-1.5 animate-pulse rounded-full bg-amber-500" />
      {state.status === 'reconnecting'
        ? 'Connection to the Daemon was lost. Reconnecting…'
        : `Cannot reach the Daemon (${state.detail}). Retrying…`}
    </div>
  )
}

/** Full-screen stop: without a valid Pairing Token there is nothing to show. */
export function PairingFailed({ reason }: { reason: 'missing-token' | 'rejected-token' }) {
  return (
    <main className="flex h-dvh flex-col items-center justify-center gap-3 px-6 text-center">
      <h1 className="text-lg font-semibold tracking-tight">Pairing failed</h1>
      <p className="max-w-sm text-sm text-muted-foreground">
        {reason === 'missing-token'
          ? 'This browser has not been paired with Droi. Open the pairing link from the desktop app on this device.'
          : 'The pairing link is no longer valid. Open a fresh pairing link from the desktop app.'}
      </p>
    </main>
  )
}
