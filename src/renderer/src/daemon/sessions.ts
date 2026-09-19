// Session list and Workspace grouping. The list is a collection fetched from
// the Daemon on demand, so it lives in TanStack Query; the open Session's
// transcript lives in the SDK state manager (see use-session.ts).
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useDaemonConnection } from './connection-context'

export interface SessionSummary {
  sessionId: string
  title: string
  cwd: string | null
  /** Grouping key the Daemon provides for checkouts; falls back to cwd. */
  repoRoot: string | null
  /** Unix epoch seconds. */
  updatedAt: number
  messagesCount: number | null
  archivedAt: string | null
}

export interface WorkspaceGroup {
  key: string
  label: string
  path: string
  sessions: SessionSummary[]
}

export const SESSIONS_QUERY_KEY = ['sessions'] as const

export function useSessionList(options: { includeArchived?: boolean } = {}) {
  const includeArchived = options.includeArchived ?? false
  const connection = useDaemonConnection()
  const queryClient = useQueryClient()

  useEffect(() => {
    const invalidate = () => void queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
    connection.controller.on('connected', invalidate)
    connection.controller.on('sessionTitleUpdated', invalidate)
    connection.controller.on('sessionArchiveStateChanged', invalidate)
    return () => {
      connection.controller.off('connected', invalidate)
      connection.controller.off('sessionTitleUpdated', invalidate)
      connection.controller.off('sessionArchiveStateChanged', invalidate)
    }
  }, [connection, queryClient])

  return useQuery({
    queryKey: [...SESSIONS_QUERY_KEY, { includeArchived }],
    queryFn: async (): Promise<SessionSummary[]> => {
      const result = await connection.controller.listAvailableSessions({
        limit: 100,
        includeArchived,
      })
      return result.sessions.map((s) => ({
        sessionId: s.sessionId,
        title: s.title?.trim() || 'Untitled session',
        cwd: s.cwd ?? null,
        repoRoot: s.repoRoot ?? null,
        updatedAt: s.updatedAt,
        messagesCount: s.messagesCount ?? null,
        archivedAt: s.archivedAt ?? null,
      }))
    },
    enabled: connection.getState().status === 'connected',
    staleTime: 10_000,
  })
}

/** Newest Workspace first; within a Workspace, newest Session first. */
export function groupByWorkspace(sessions: readonly SessionSummary[]): WorkspaceGroup[] {
  const groups = new Map<string, WorkspaceGroup>()
  for (const session of sessions) {
    const path = session.repoRoot ?? session.cwd ?? ''
    const key = path || '(unknown)'
    let group = groups.get(key)
    if (!group) {
      group = { key, label: workspaceLabel(path), path, sessions: [] }
      groups.set(key, group)
    }
    group.sessions.push(session)
  }
  const result = [...groups.values()]
  for (const group of result) group.sessions.sort((a, b) => b.updatedAt - a.updatedAt)
  result.sort((a, b) => (b.sessions[0]?.updatedAt ?? 0) - (a.sessions[0]?.updatedAt ?? 0))
  return result
}

export function workspaceLabel(path: string): string {
  if (!path) return 'Unknown workspace'
  const trimmed = path.replace(/[\\/]+$/, '')
  const last = trimmed.split(/[\\/]/).pop()
  return last || trimmed
}
