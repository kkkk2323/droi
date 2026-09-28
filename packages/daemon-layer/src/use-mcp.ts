// A Session's MCP Servers, tools and sign-in, read from and changed through the
// Daemon. The Daemon pushes `mcp_status_changed` as connections come and go,
// which replaces the cached list. A sign-in a server needs is a page the
// Daemon names and a callback of its own on the computer that finishes it;
// a Client only opens the page.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useConnectionState, useDaemonConnection } from './connection-context'
import type { AddMcpServerInput, McpRegistryEntry, McpServer, McpSummary, McpTool } from './mcp'

export const MCP_SERVERS_QUERY_KEY = ['mcp-servers'] as const
export const MCP_TOOLS_QUERY_KEY = ['mcp-tools'] as const
const MCP_REGISTRY_QUERY_KEY = ['mcp-registry'] as const

export interface McpServersView {
  servers: readonly McpServer[]
  summary: McpSummary | null
  isLoading: boolean
  error: string | null
}

export function useMcpServers(sessionId: string): McpServersView {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: [...MCP_SERVERS_QUERY_KEY, sessionId],
    enabled: connected,
    staleTime: 30_000,
    queryFn: () => controller.listMcpServers(sessionId),
  })

  // A sign-in the Daemon asks for shows in the list as the server's
  // pendingAuthUrl; the two auth notifications only say the list changed.
  useEffect(() => {
    const key = [...MCP_SERVERS_QUERY_KEY, sessionId]
    const onStatus = (params: { sessionId: string } & Partial<Listed>) => {
      if (params.sessionId !== sessionId || !params.servers || !params.summary) return
      queryClient.setQueryData<Listed>(key, { servers: params.servers, summary: params.summary })
    }
    const onAuth = (params: { sessionId: string }) => {
      if (params.sessionId === sessionId) void queryClient.invalidateQueries({ queryKey: key })
    }
    controller.on('mcpStatusChanged', onStatus)
    controller.on('mcpAuthRequired', onAuth)
    controller.on('mcpAuthCompleted', onAuth)
    return () => {
      controller.off('mcpStatusChanged', onStatus)
      controller.off('mcpAuthRequired', onAuth)
      controller.off('mcpAuthCompleted', onAuth)
    }
  }, [controller, queryClient, sessionId])

  return {
    servers: query.data?.servers ?? NO_SERVERS,
    summary: query.data?.summary ?? null,
    isLoading: query.isPending,
    error: query.error ? messageOf(query.error) : null,
  }
}

type Listed = { servers: McpServer[]; summary: McpSummary }
const NO_SERVERS: McpServer[] = []
const NO_TOOLS: McpTool[] = []
const NO_ENTRIES: McpRegistryEntry[] = []

/** Every server's tools; `enabled` false leaves the Daemon alone until they are wanted. */
export function useMcpTools(
  sessionId: string,
  enabled = true,
): { tools: readonly McpTool[]; isLoading: boolean } {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const query = useQuery({
    queryKey: [...MCP_TOOLS_QUERY_KEY, sessionId],
    enabled: connected && enabled,
    staleTime: 30_000,
    queryFn: () => controller.listMcpTools(sessionId),
  })
  return { tools: query.data?.tools ?? NO_TOOLS, isLoading: enabled && query.isPending }
}

/** Factory's catalogue of servers to add with a tap. */
export function useMcpRegistry(
  sessionId: string,
  enabled = true,
): { entries: readonly McpRegistryEntry[]; isLoading: boolean; error: string | null } {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const query = useQuery({
    queryKey: [...MCP_REGISTRY_QUERY_KEY, sessionId],
    enabled: connected && enabled,
    staleTime: 5 * 60_000,
    queryFn: () => controller.listMcpRegistry(sessionId),
  })
  return {
    entries: query.data?.servers ?? NO_ENTRIES,
    isLoading: enabled && query.isPending,
    error: query.error ? messageOf(query.error) : null,
  }
}

export interface McpActions {
  setEnabled(serverName: string, enabled: boolean): Promise<void>
  remove(serverName: string): Promise<void>
  /** Resolves false when the Daemon refused, with the reason in `error`. */
  add(params: AddMcpServerInput): Promise<boolean>
  setToolEnabled(serverName: string, toolName: string, enabled: boolean): Promise<void>
  signIn(serverName: string): Promise<void>
  cancelSignIn(serverName: string): Promise<void>
  signOut(serverName: string): Promise<void>
  /** The server an action is under way for, if any. */
  busy: string | null
  error: string | null
}

export function useMcpActions(sessionId: string): McpActions {
  const { controller } = useDaemonConnection()
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState<string | null>(null)
  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: [...MCP_SERVERS_QUERY_KEY, sessionId] }),
      queryClient.invalidateQueries({ queryKey: [...MCP_TOOLS_QUERY_KEY, sessionId] }),
    ])
  const mutation = useMutation({
    mutationFn: async ({
      serverName,
      run,
    }: {
      serverName: string
      run: () => Promise<unknown>
    }) => {
      setBusy(serverName)
      try {
        await run()
      } finally {
        setBusy(null)
      }
    },
    onSettled: refresh,
  })
  const act = (serverName: string, run: () => Promise<unknown>) =>
    mutation.mutateAsync({ serverName, run }).then(
      () => true,
      () => false,
    )
  const level = 'user' as never

  return {
    setEnabled: async (serverName, enabled) => {
      await act(serverName, () =>
        controller.toggleMcpServer({ sessionId, serverName, enabled, settingsLevel: level }),
      )
    },
    remove: async (serverName) => {
      await act(serverName, () =>
        controller.removeMcpServer({ sessionId, serverName, settingsLevel: level }),
      )
    },
    add: (params) => act(params.name, () => controller.addMcpServer({ sessionId, ...params })),
    setToolEnabled: async (serverName, toolName, enabled) => {
      await act(serverName, () =>
        controller.toggleMcpTool(sessionId, serverName, toolName, enabled),
      )
    },
    signIn: async (serverName) => {
      await act(serverName, () => controller.authenticateMcpServer({ sessionId, serverName }))
    },
    cancelSignIn: async (serverName) => {
      await act(serverName, () => controller.cancelMcpAuth({ sessionId, serverName }))
    },
    signOut: async (serverName) => {
      await act(serverName, () => controller.clearMcpAuth({ sessionId, serverName }))
    },
    busy,
    error: mutation.error ? messageOf(mutation.error) : null,
  }
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
