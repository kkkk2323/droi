// What the Session has changed in its Workspace, as the Daemon sees it. The
// Daemon's get_git_diff reports several ranges; the header wants the
// uncommitted one (working tree against HEAD, untracked files included).
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useDaemonConnection } from './connection-context'

export interface ChangedFile {
  path: string
  /** The Daemon's word: added, modified, deleted, renamed, ... */
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
  const { controller, sessionState } = useDaemonConnection()
  const queryClient = useQueryClient()
  const queryKey = ['git-changes', sessionId]
  // The Daemon edits files while a turn runs: read again after each tool
  // result, and once more when the turn rests.
  useEffect(() => {
    if (!deps.running) void queryClient.invalidateQueries({ queryKey: ['git-changes', sessionId] })
  }, [deps.running, sessionId, queryClient])
  useEffect(() => {
    // The notification does not name its Session. This one's working state in
    // the SDK (current already, unlike React's view of it) says whether it is busy.
    const refresh = () => {
      const state = sessionState.getSessionManager(sessionId)?.getDroidWorkingState()
      if (!state || state === 'idle') return
      // A read already under way is left to finish rather than restarted per result.
      void queryClient.invalidateQueries(
        { queryKey: ['git-changes', sessionId] },
        { cancelRefetch: false },
      )
    }
    controller.on('toolResult', refresh)
    return () => {
      controller.off('toolResult', refresh)
    }
  }, [controller, sessionState, sessionId, queryClient])
  const query = useQuery({
    queryKey,
    enabled: deps.loaded,
    staleTime: 5_000,
    // Edits made outside Droi (an editor, a terminal, a commit) show up on this beat.
    refetchInterval: 15_000,
    retry: false,
    queryFn: async (): Promise<GitChanges | null> => {
      const result = await controller.getGitDiff({ sessionId, statsOnly: true })
      if (!result.success) return null
      const { data } = result
      return {
        branch: data.isDetachedHead ? 'detached HEAD' : data.branch,
        files: data.unstagedFiles,
        additions: data.unstagedTotalAdditions,
        deletions: data.unstagedTotalDeletions,
      }
    },
  })
  return query.data ?? null
}
