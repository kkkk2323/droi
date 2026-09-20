import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { parsePairingInput, savePairing } from '@/lib/client-config'
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
      className="flex h-8 items-center gap-1.5 px-2 text-xs text-muted-foreground"
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
      className="flex items-center justify-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-700 dark:text-amber-200"
    >
      <span aria-hidden className="size-1.5 animate-pulse rounded-full bg-amber-500" />
      {state.status === 'reconnecting'
        ? 'Connection to the Daemon was lost. Reconnecting…'
        : `Cannot reach the Daemon (${state.detail}). Retrying…`}
    </div>
  )
}

/**
 * Full-screen stop: without a valid Pairing Token there is nothing to show.
 * The link can be pasted here too: a web app added to the iOS home screen
 * opens without the link's fragment and with storage of its own.
 */
export function PairingFailed({ reason }: { reason: 'missing-token' | 'rejected-token' }) {
  const [link, setLink] = useState('')
  const [invalid, setInvalid] = useState(false)
  const pair = () => {
    const pairing = parsePairingInput(link)
    if (!pairing) {
      setInvalid(true)
      return
    }
    savePairing(window.localStorage, pairing)
    window.location.replace('/')
  }
  return (
    <main className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
      <h1 className="text-lg font-semibold tracking-tight">Pairing failed</h1>
      <p className="max-w-sm text-sm text-muted-foreground">
        {reason === 'missing-token'
          ? 'This browser has not been paired with Droi. Open the pairing link from the desktop app on this device, or paste it below.'
          : 'The pairing link is no longer valid. Open a fresh pairing link from the desktop app, or paste it below.'}
      </p>
      <form
        className="mt-2 flex w-full max-w-sm gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          pair()
        }}
      >
        <input
          aria-label="Pairing link"
          aria-invalid={invalid || undefined}
          value={link}
          onChange={(event) => {
            setLink(event.target.value)
            setInvalid(false)
          }}
          placeholder="http://…/#pair=…"
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          className="h-9 min-w-0 flex-1 rounded-lg border bg-background px-3 font-mono text-sm outline-none transition-colors focus-visible:border-ring/60 focus-visible:ring-2 focus-visible:ring-ring/30 aria-invalid:border-destructive"
        />
        <Button type="submit" disabled={!link.trim()}>
          Pair
        </Button>
      </form>
      {invalid ? (
        <p role="alert" className="text-xs text-destructive-foreground">
          That is not a pairing link or token.
        </p>
      ) : null}
    </main>
  )
}
