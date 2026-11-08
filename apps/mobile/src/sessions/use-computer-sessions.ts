// The connected computer's Session list, or what was last seen of it while
// the connection is not up yet (a switch, a cold start, the computer away).
import { showArchivedSessions, usePreference } from '@droi/daemon-layer/local-preference'
import { useSessionList, type SessionSummary } from '@droi/daemon-layer/sessions'
import { useEffect, useMemo } from 'react'
import { sessionSummaries, type SessionSummaryCache } from '../computers/store'

export interface ComputerSessions {
  sessions: SessionSummary[]
  /** False while showing the last known list. */
  live: boolean
  isPending: boolean
  error: Error | null
  /** Sessions older than the ones listed wait on the computer; null when the list is complete. */
  older: { loading: boolean; load: () => void } | null
}

export function useComputerSessions(computerId: string): ComputerSessions {
  const [showArchived] = usePreference(showArchivedSessions)
  const list = useSessionList({ includeArchived: showArchived })
  const [cached] = usePreference(sessionSummaries(computerId))
  // One array per cached list, so what is memoized on the list holds while offline.
  const offline = useMemo(() => cached.map(fromCache), [cached])
  useEffect(() => {
    if (list.data) sessionSummaries(computerId).set(list.data.map(toCache))
  }, [list.data, computerId])
  return {
    sessions: list.data ?? offline,
    live: list.data !== undefined,
    isPending: list.isPending && cached.length === 0,
    error: list.error,
    older: list.hasMore ? { loading: list.isLoadingMore, load: list.loadMore } : null,
  }
}

function toCache(s: SessionSummary): SessionSummaryCache {
  return {
    sessionId: s.sessionId,
    title: s.title,
    cwd: s.cwd,
    repoRoot: s.repoRoot,
    worktree: s.worktree,
    updatedAt: s.updatedAt,
    activityAt: s.activityAt,
    viewedAt: s.viewedAt,
    kind: s.kind,
    archivedAt: s.archivedAt,
    parentId: s.parentId,
    tags: s.tags,
  }
}

function fromCache(s: SessionSummaryCache): SessionSummary {
  return { ...s, worktree: s.worktree ?? null, messagesCount: null }
}
