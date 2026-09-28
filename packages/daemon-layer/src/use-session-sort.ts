// The Session list's order, as this Client keeps it: how Workspaces and the
// Sessions in them are sorted, the Workspaces placed by hand, and when each
// Session was first seen, which stands in for the creation time the Daemon
// does not list.
import { useEffect } from 'react'
import {
  manualWorkspaceOrder,
  sessionSort,
  sessionsFirstSeen,
  usePreference,
  workspaceSort,
} from './local-preference'
import {
  SESSION_SORT_LABELS,
  WORKSPACE_SORT_LABELS,
  noteFirstSeen,
  type SessionSort,
  type SessionSummary,
  type SortOrder,
  type WorkspaceSort,
} from './sessions'

export interface SessionSortControls {
  order: SortOrder
  setWorkspaces(sort: WorkspaceSort): void
  setSessions(sort: SessionSort): void
  setManual(keys: string[]): void
}

export function useSessionSort(listed: readonly SessionSummary[]): SessionSortControls {
  const [workspaces, setWorkspaces] = usePreference(workspaceSort)
  const [sessions, setSessions] = usePreference(sessionSort)
  const [manual, setManual] = usePreference(manualWorkspaceOrder)
  const [firstSeen, setFirstSeen] = usePreference(sessionsFirstSeen)

  useEffect(() => {
    const next = noteFirstSeen(sessionsFirstSeen.get(), listed)
    if (next !== sessionsFirstSeen.get()) setFirstSeen({ ...next })
  }, [listed, setFirstSeen])

  return {
    order: {
      workspaces:
        workspaces && workspaces in WORKSPACE_SORT_LABELS
          ? (workspaces as WorkspaceSort)
          : 'sessions',
      sessions: sessions && sessions in SESSION_SORT_LABELS ? (sessions as SessionSort) : 'recent',
      manual,
      firstSeen,
    },
    setWorkspaces,
    setSessions,
    setManual,
  }
}
