// The Git branches of a Workspace, which also tells whether it is a Git repository at all
// (worktrees are made only of those), as the computer's git answers through the Gateway.
import { useQuery } from '@tanstack/react-query'
import { useConnectionState, useKernelConnection } from './connection-context'

export interface GitBranches {
  isGitRepository: boolean
  branches: string[]
  currentBranch: string | null
}

/** Undefined until the computer has answered; null path (no Workspace) is never asked. */
export function useGitBranches(path: string | null): GitBranches | undefined {
  const connection = useKernelConnection()
  const connected = useConnectionState().status === 'connected'
  const query = useQuery({
    queryKey: ['git-branches', path],
    enabled: connected && path !== null,
    staleTime: 30_000,
    queryFn: async (): Promise<GitBranches> => {
      const found = await connection.workspaces.inspect(path!)
      return {
        isGitRepository: found.isGitRepository,
        branches: found.branches,
        currentBranch: found.currentBranch || null,
      }
    },
  })
  return path === null ? undefined : query.data
}
