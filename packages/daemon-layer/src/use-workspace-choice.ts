// Where a new Session will work, as the New session page offers it: a recent
// Workspace, a typed path, or None, which is a fresh Scratch Workspace the
// Gateway makes on the computer (ADR 0008). A folder made for None that no
// Session took goes when the page does (the Gateway removes it while empty).
import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useConnectionState, useKernelConnection } from './connection-context'
import { SCRATCH_TAG, type SessionTag } from './sessions'
import type { RecentWorkspace } from './use-new-session'
import { uuid } from './uuid'

export type WorkspacePick =
  /** A folder by path: a recent Workspace, or one chosen in the Desktop Shell's folder dialog. */
  { kind: 'recent'; path: string } | { kind: 'scratch' } | { kind: 'other' }

/**
 * Until the user picks: the most recent Workspace, or None when there is none
 * yet. Null while the session list is still on its way, so no folder is made
 * for a user who has recents.
 */
export function defaultPick(recent: readonly RecentWorkspace[] | null): WorkspacePick | null {
  if (!recent) return null
  const latest = recent[0]
  return latest ? { kind: 'recent', path: latest.path } : { kind: 'scratch' }
}

export const SCRATCH_TAGS: SessionTag[] = [{ name: SCRATCH_TAG }]
const NO_TAGS: SessionTag[] = []

export interface WorkspaceChoice {
  pick: WorkspacePick | null
  /** Where the Session starts; null while a path is typed or the folder is being made. */
  workspace: string | null
  /** Tags the Session carries: the Scratch tag for None. */
  tags: SessionTag[]
  /** Why no folder could be made for None. */
  error: string | null
  choose(pick: WorkspacePick): void
  /** A Session was started in the Scratch folder: it stays when the page goes. */
  keep(): void
}

export function useWorkspaceChoice(
  recent: readonly RecentWorkspace[] | null,
  initial: WorkspacePick | null,
): WorkspaceChoice {
  const connection = useKernelConnection()
  const connected = useConnectionState().status === 'connected'
  const [chosen, setChosen] = useState(initial)
  const [page] = useState(uuid)
  const pick = chosen ?? defaultPick(recent)
  const scratch = pick?.kind === 'scratch'
  const folder = useQuery({
    queryKey: ['scratch-workspace', page],
    enabled: connected && scratch,
    // Each answer is a folder on disk; asking again would make another.
    staleTime: Infinity,
    retry: false,
    queryFn: () => connection.scratch.create(),
  })
  const made = useRef<string | null>(null)
  const kept = useRef(false)
  useEffect(() => {
    made.current = folder.data ?? null
  }, [folder.data])
  useEffect(
    () => () => {
      if (!kept.current && made.current) void connection.scratch.trash(made.current).catch(() => {})
    },
    [connection],
  )
  return {
    pick,
    workspace: pick?.kind === 'recent' ? pick.path : scratch ? (folder.data ?? null) : null,
    tags: scratch ? SCRATCH_TAGS : NO_TAGS,
    error: scratch && folder.error ? folder.error.message : null,
    choose: setChosen,
    keep: () => {
      kept.current = true
    },
  }
}
