import { createContext, useContext, useSyncExternalStore, type ReactNode } from 'react'
import type { ConnectionState, KernelConnection } from './connection'

const ConnectionContext = createContext<KernelConnection | null>(null)

export function ConnectionProvider({
  connection,
  children,
}: {
  connection: KernelConnection
  children: ReactNode
}) {
  return <ConnectionContext.Provider value={connection}>{children}</ConnectionContext.Provider>
}

export function useKernelConnection(): KernelConnection {
  const connection = useContext(ConnectionContext)
  if (!connection) throw new Error('useKernelConnection needs a ConnectionProvider')
  return connection
}

export function useConnectionState(): ConnectionState {
  const connection = useKernelConnection()
  return useSyncExternalStore(connection.subscribe, connection.getState, connection.getState)
}
