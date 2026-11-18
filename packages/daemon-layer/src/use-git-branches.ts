// The Git branches of a Workspace, which also tells whether it is a Git
// repository at all (the Daemon makes worktrees only of those).
import { useQuery } from '@tanstack/react-query'
import { useConnectionState, useDaemonConnection } from './connection-context'

export interface GitBranches {
  isGitRepository: boolean
  branches: string[]
  currentBranch: string | null
}

/** Undefined until the Daemon has answered; null path (no Workspace) is never asked. */
export function useGitBranches(path: string | null): GitBranches | undefined {
  const connection = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const query = useQuery({
    queryKey: ['git-branches', path],
    enabled: connected && path !== null,
    staleTime: 30_000,
    queryFn: async (): Promise<GitBranches> => {
      const result = await connection.controller.listGitBranches(path!)
      return {
        isGitRepository: result.isGitRepository,
        branches: result.branches,
        currentBranch: result.currentBranch,
      }
    },
  })
  return path === null ? undefined : query.data
}
