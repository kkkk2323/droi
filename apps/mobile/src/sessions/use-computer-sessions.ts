// The connected computer's Session list, or what was last seen of it while
// the connection is not up yet (a switch, a cold start, the computer away).
import { showArchivedSessions, usePreference } from '@droi/daemon-layer/local-preference'
import { useSessionList, type SessionSummary } from '@droi/daemon-layer/sessions'
import { useEffect } from 'react'
import { sessionSummaries, type SessionSummaryCache } from '../computers/store'

export interface ComputerSessions {
  sessions: SessionSummary[]
  /** False while showing the last known list. */
  live: boolean
  isPending: boolean
  error: Error | null
}

export function useComputerSessions(computerId: string): ComputerSessions {
  const [showArchived] = usePreference(showArchivedSessions)
  const list = useSessionList({ includeArchived: showArchived })
  const [cached] = usePreference(sessionSummaries(computerId))
  useEffect(() => {
    if (list.data) sessionSummaries(computerId).set(list.data.map(toCache))
  }, [list.data, computerId])
  return {
    sessions: list.data ?? cached.map(fromCache),
    live: list.data !== undefined,
    isPending: list.isPending && cached.length === 0,
    error: list.error,
  }
}

function toCache(s: SessionSummary): SessionSummaryCache {
  return {
    sessionId: s.sessionId,
    title: s.title,
    cwd: s.cwd,
    repoRoot: s.repoRoot,
    updatedAt: s.updatedAt,
    archivedAt: s.archivedAt,
    parentId: s.parentId,
    callingSessionId: s.callingSessionId,
    callingToolUseId: s.callingToolUseId,
  }
}

function fromCache(s: SessionSummaryCache): SessionSummary {
  return {
    ...s,
    messagesCount: null,
    tags: [],
    callingSessionId: s.callingSessionId ?? null,
    callingToolUseId: s.callingToolUseId ?? null,
  }
}
