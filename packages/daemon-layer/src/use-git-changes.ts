// What the Session has changed in its Workspace: the working tree against HEAD, untracked files
// included, as the computer's git sees it through the Gateway (`/git-changes`). The Kernel
// edits files while a turn runs, so the changes are read again after each tool result, and
// once more when the turn rests.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useSyncExternalStore } from 'react'
import { useConnectionState, useKernelConnection } from './connection-context'
import { conversationStore } from './kernel-conversations'
import { conversationIdOf, type AgentDoc } from './kernel-wire'

export interface ChangedFile {
  path: string
  /** git's word: added, modified, deleted, renamed, ... */
  status: string
  additions: number
  deletions: number
}

export interface GitChanges {
  branch: string
  files: ChangedFile[]
  additions: number
  deletions: number
}

export function useGitChanges(
  sessionId: string,
  deps: { loaded: boolean; running: boolean },
): GitChanges | null {
  const connection = useKernelConnection()
  const connected = useConnectionState().status === 'connected'
  const store = conversationStore(connection)
  const queryClient = useQueryClient()
  const id = conversationIdOf(sessionId)
  const subscribe = useCallback(
    (listener: () => void) => (id === null ? () => {} : store.subscribe(id, listener)),
    [store, id],
  )
  const getCwd = useCallback(() => {
    if (id === null) return null
    const cwd = (store.snapshot(id).docs['pi.agent'] as AgentDoc | undefined)?.cwd
    return typeof cwd === 'string' && cwd ? cwd : null
  }, [store, id])
  const getResults = useCallback(
    () =>
      id === null
        ? 0
        : store.snapshot(id).entries.filter((entry) => entry.kind === 'pi.tool-result').length,
    [store, id],
  )
  const cwd = useSyncExternalStore(subscribe, getCwd, getCwd)
  const results = useSyncExternalStore(subscribe, getResults, getResults)

  useEffect(() => {
    if (!deps.running) void queryClient.invalidateQueries({ queryKey: ['git-changes', sessionId] })
  }, [deps.running, sessionId, queryClient])
  useEffect(() => {
    // Before the turn's first tool result nothing has changed yet.
    if (!deps.running || results === 0) return
    // A read already under way is left to finish rather than restarted per result.
    void queryClient.invalidateQueries(
      { queryKey: ['git-changes', sessionId] },
      { cancelRefetch: false },
    )
  }, [results, deps.running, sessionId, queryClient])

  const query = useQuery({
    queryKey: ['git-changes', sessionId, cwd],
    enabled: connected && deps.loaded && cwd !== null,
    staleTime: 5_000,
    // Edits made outside Droi (an editor, a terminal, a commit) show up on this beat.
    refetchInterval: 15_000,
    retry: false,
    queryFn: async (): Promise<GitChanges | null> => {
      const found = await connection.workspaces.changes(cwd!)
      if (!found.isGitRepository) return null
      return {
        branch: found.detached ? 'detached HEAD' : found.branch,
        files: found.files,
        additions: found.additions,
        deletions: found.deletions,
      }
    },
  })
  return query.data ?? null
}
