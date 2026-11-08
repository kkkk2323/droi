// The computer's MCP Servers, through its Kernel: `mcp.list` for the user's global servers and
// the Session's repository's own, `mcp.set` to change one, and `mcp.authorize` to sign in to an
// HTTP server. `mcp.changed` tells every Client after any change, which reads the list again.
// A sign-in page waits on the computer's own callback, so it is kept here, by server, until the
// server connects.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, useSyncExternalStore } from 'react'
import type { KernelConnection } from './connection'
import { useConnectionState, useKernelConnection } from './connection-context'
import { serverKey, type AddMcpServerInput, type McpServer } from './mcp'

export const MCP_SERVERS_QUERY_KEY = ['mcp-servers'] as const

export interface McpServersView {
  servers: readonly McpServer[]
  isLoading: boolean
  error: string | null
}

const NO_SERVERS: McpServer[] = []

/** Sign-in pages started from this Client, by serverKey. */
interface SignIns {
  pages: ReadonlyMap<string, string>
  set(key: string, url: string | null): void
  subscribe(listener: () => void): () => void
}

const signIns = new WeakMap<KernelConnection, SignIns>()

function signInsOf(connection: KernelConnection): SignIns {
  let store = signIns.get(connection)
  if (!store) {
    const listeners = new Set<() => void>()
    const made: SignIns = {
      pages: new Map(),
      set(key, url) {
        const next = new Map(made.pages)
        if (url) next.set(key, url)
        else if (!next.delete(key)) return
        made.pages = next
        for (const listener of listeners) listener()
      },
      subscribe(listener) {
        listeners.add(listener)
        return () => void listeners.delete(listener)
      },
    }
    store = made
    signIns.set(connection, store)
  }
  return store
}

/** The sign-in page this Client started for each server, until it connects. */
export function useSignInPages(): ReadonlyMap<string, string> {
  const store = signInsOf(useKernelConnection())
  return useSyncExternalStore(
    store.subscribe,
    () => store.pages,
    () => store.pages,
  )
}

/** The global servers and, with `repoRoot`, that repository's. */
export function useMcpServers(repoRoot: string | null): McpServersView {
  const connection = useKernelConnection()
  const connected = useConnectionState().status === 'connected'
  const queryClient = useQueryClient()
  const key = [...MCP_SERVERS_QUERY_KEY, repoRoot]
  const query = useQuery({
    queryKey: key,
    enabled: connected,
    staleTime: 30_000,
    queryFn: async () => {
      const listed = await connection.call<{ servers: McpServer[] }>(
        'mcp.list',
        repoRoot ? { repoRoot } : {},
      )
      const store = signInsOf(connection)
      for (const server of listed.servers) {
        if (server.status === 'connected') store.set(serverKey(server), null)
      }
      return listed.servers
    },
  })

  useEffect(
    () =>
      connection.onNotification(({ method }) => {
        if (method === 'mcp.changed')
          void queryClient.invalidateQueries({ queryKey: MCP_SERVERS_QUERY_KEY })
      }),
    [connection, queryClient],
  )

  return {
    servers: query.data ?? NO_SERVERS,
    isLoading: query.isPending,
    error: query.error ? messageOf(query.error) : null,
  }
}

export interface McpActions {
  setEnabled(server: McpServer, enabled: boolean): Promise<void>
  remove(server: McpServer): Promise<void>
  /** Resolves false when the computer refused, with the reason in `error`. */
  add(params: AddMcpServerInput): Promise<boolean>
  /** Allows a repository's server to run what it was shown to run. */
  trust(server: McpServer): Promise<void>
  signIn(server: McpServer): Promise<void>
  cancelSignIn(server: McpServer): Promise<void>
  /** The server an action is under way for, if any. */
  busy: string | null
  error: string | null
}

export function useMcpActions(): McpActions {
  const connection = useKernelConnection()
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState<string | null>(null)
  const mutation = useMutation({
    mutationFn: async ({ name, run }: { name: string; run: () => Promise<unknown> }) => {
      setBusy(name)
      try {
        await run()
      } finally {
        setBusy(null)
      }
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: MCP_SERVERS_QUERY_KEY }),
  })
  const act = (name: string, run: () => Promise<unknown>) =>
    mutation.mutateAsync({ name, run }).then(
      () => true,
      () => false,
    )
  const where = (server: McpServer) => (server.repoRoot ? { repoRoot: server.repoRoot } : {})

  return {
    setEnabled: async (server, enabled) => {
      await act(server.name, () => connection.call('mcp.set', { name: server.name, enabled }))
    },
    remove: async (server) => {
      await act(server.name, () => connection.call('mcp.set', { name: server.name, remove: true }))
    },
    add: ({ name, ...config }) =>
      act(name, () => connection.call('mcp.set', { name, server: config })),
    trust: async (server) => {
      await act(server.name, () =>
        connection.call('mcp.set', {
          name: server.name,
          ...where(server),
          trust: true,
          fingerprint: server.fingerprint,
        }),
      )
    },
    signIn: async (server) => {
      await act(server.name, async () => {
        const answer = await connection.call<{ authorizationUrl?: string }>('mcp.authorize', {
          name: server.name,
          ...where(server),
        })
        if (answer.authorizationUrl) {
          signInsOf(connection).set(serverKey(server), answer.authorizationUrl)
        }
      })
    },
    cancelSignIn: async (server) => {
      signInsOf(connection).set(serverKey(server), null)
      await act(server.name, () =>
        connection.call('mcp.authorize.cancel', { name: server.name, ...where(server) }),
      )
    },
    busy,
    error: mutation.error ? messageOf(mutation.error) : null,
  }
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
