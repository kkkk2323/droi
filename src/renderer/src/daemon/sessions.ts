// Session list and Workspace grouping. The list is a collection fetched from
// the Daemon on demand, so it lives in TanStack Query; the open Session's
// transcript lives in the SDK state manager (see use-session.ts).
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useConnectionState, useDaemonConnection } from './connection-context'

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
  tags: SessionTag[]
  /** The Session this one continues after a compaction (see use-compact.ts). */
  parentId: string | null
}

export interface SessionTag {
  name: string
  metadata?: Record<string, string>
}

/**
 * Tag a compaction's child Session carries so every Client can chain it to
 * its parent; the Daemon writes the link into the session file but does not
 * list it.
 */
export const CONTINUES_TAG = 'droi.continues'

export function continuationParent(tags: readonly SessionTag[] | undefined): string | null {
  return tags?.find((t) => t.name === CONTINUES_TAG)?.metadata?.['parent'] ?? null
}

/** Tags for a child Session: the parent's, minus any older link, plus the new one. */
export function continuationTags(parentId: string, inherited: readonly SessionTag[]): SessionTag[] {
  return [
    ...inherited.filter((t) => t.name !== CONTINUES_TAG),
    { name: CONTINUES_TAG, metadata: { parent: parentId } },
  ]
}

/** Drops Sessions that another listed Session continues; the chain shows as its latest link. */
export function foldContinued(sessions: readonly SessionSummary[]): SessionSummary[] {
  const parents = new Set(sessions.map((s) => s.parentId).filter((id): id is string => !!id))
  return sessions.filter((s) => !parents.has(s.sessionId))
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
  // Read through the hook, not connection.getState(): the compiler memoizes
  // the options on `connection`, which never changes identity.
  const connected = useConnectionState().status === 'connected'
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
        tags: s.tags ?? [],
        parentId: continuationParent(s.tags),
      }))
    },
    enabled: connected,
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

/** Sessions touched within this window always show; older ones sit behind "Show more". */
export const RECENT_WINDOW_MS = 3 * 24 * 60 * 60 * 1000
export const OLDER_BATCH = 30

/**
 * The rows a Workspace section shows: every recent Session plus the first
 * `revealed` older ones (the list is newest first), and how many stay hidden.
 */
export function visibleSessions(
  sessions: readonly SessionSummary[],
  revealed: number,
  now = Date.now(),
): { visible: SessionSummary[]; hidden: number } {
  const cutoff = now - RECENT_WINDOW_MS
  const visible: SessionSummary[] = []
  let older = 0
  for (const session of sessions) {
    const recent = session.updatedAt * 1000 >= cutoff
    if (recent || older < revealed) visible.push(session)
    if (!recent) older += 1
  }
  return { visible, hidden: Math.max(0, older - revealed) }
}

export function workspaceLabel(path: string): string {
  if (!path) return 'Unknown workspace'
  const trimmed = path.replace(/[\\/]+$/, '')
  const last = trimmed.split(/[\\/]/).pop()
  return last || trimmed
}
