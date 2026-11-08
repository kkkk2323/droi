// One connection, to the selected Paired Computer's Kernel, through the shared daemon
// layer. A different computer, address or Pairing Token builds a new
// connection and a new QueryClient, so nothing of the last one carries over.
import { ConnectionProvider } from '@droi/daemon-layer/connection-context'
import { createKernelConnection } from '@droi/daemon-layer/connection'
import { QueryClientProvider, useQuery } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import { AppState } from 'react-native'
import { tokenKey, type PairedComputer } from '../computers/store'
import { keychain } from '../platform/keychain'
import { createQueryClient } from '../query-client'

export const PAIRING_TOKEN_QUERY = 'pairing-token'

export function ComputerConnection({
  computer,
  children,
}: {
  computer: PairedComputer
  children: ReactNode
}) {
  const token = useQuery({
    queryKey: [PAIRING_TOKEN_QUERY, computer.id],
    queryFn: () => keychain.get(tokenKey(computer.id)),
  })
  if (token.isPending) return null
  return (
    <Connected
      key={`${computer.id} ${computer.address} ${computer.relay ?? ''} ${token.data ?? ''}`}
      address={computer.address}
      relay={computer.relay ?? null}
      token={token.data ?? null}
    >
      {children}
    </Connected>
  )
}

function Connected({
  address,
  relay,
  token,
  children,
}: {
  address: string
  relay: string | null
  token: string | null
  children: ReactNode
}) {
  // Connected is keyed on the computer, address and token, so one mount is
  // one connection. (No StrictMode here: a second effect run would reuse a
  // disposed connection.)
  const [live] = useState(() => ({
    connection: createKernelConnection({
      kind: 'remote',
      gatewayUrl: address,
      relayUrl: relay,
      pairingToken: token,
    }),
    queries: createQueryClient(),
  }))
  useEffect(() => {
    live.connection.start()
    // iOS suspends a backgrounded app and its socket with it; let go on the
    // way out and reconnect on the way back rather than trust a dead socket.
    const appState = AppState.addEventListener('change', (next) => {
      if (next === 'background') live.connection.suspend()
      if (next === 'active') live.connection.resume()
    })
    return () => {
      appState.remove()
      live.connection.dispose()
      live.queries.clear()
    }
  }, [live])

  return (
    <QueryClientProvider client={live.queries}>
      <ConnectionProvider connection={live.connection}>{children}</ConnectionProvider>
    </QueryClientProvider>
  )
}
