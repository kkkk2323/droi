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
  const { controller } = useDaemonConnection()
  const queryClient = useQueryClient()
  const queryKey = ['git-changes', sessionId]
  // The Daemon edits files while a turn runs; the count is read once it rests.
  useEffect(() => {
    if (!deps.running) void queryClient.invalidateQueries({ queryKey: ['git-changes', sessionId] })
  }, [deps.running, sessionId, queryClient])
  const query = useQuery({
    queryKey,
    enabled: deps.loaded,
    staleTime: 5_000,
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
