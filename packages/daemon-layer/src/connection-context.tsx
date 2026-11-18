import { createContext, useContext, useSyncExternalStore, type ReactNode } from 'react'
import type { ConnectionState, DaemonConnection } from './connection'

const ConnectionContext = createContext<DaemonConnection | null>(null)

export function ConnectionProvider({
  connection,
  children,
}: {
  connection: DaemonConnection
  children: ReactNode
}) {
  return <ConnectionContext.Provider value={connection}>{children}</ConnectionContext.Provider>
}

export function useDaemonConnection(): DaemonConnection {
  const connection = useContext(ConnectionContext)
  if (!connection) throw new Error('useDaemonConnection needs a ConnectionProvider')
  return connection
}

export function useConnectionState(): ConnectionState {
  const connection = useDaemonConnection()
  return useSyncExternalStore(connection.subscribe, connection.getState, connection.getState)
}
