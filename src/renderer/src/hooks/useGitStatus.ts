import { useQuery } from '@tanstack/react-query'
import { getDroidClient } from '@/droidClient'

export function useGitStatusQuery(projectDir: string, enabled = true, refetchInterval?: number | false) {
  return useQuery({
    queryKey: ['gitStatus', projectDir],
    queryFn: () => getDroidClient().getGitStatus({ projectDir }),
    enabled: Boolean(projectDir) && enabled,
    staleTime: 2000,
    refetchInterval: refetchInterval ?? (enabled ? 5000 : false),
  })
}

export function useGitBranchQuery(projectDir: string, enabled = true) {
  return useQuery({
    queryKey: ['gitBranch', projectDir],
    queryFn: () => getDroidClient().getGitBranch({ projectDir }),
    enabled: Boolean(projectDir) && enabled,
  })
}

export function useGitBranchesQuery(projectDir: string, enabled = true) {
  return useQuery({
    queryKey: ['gitBranches', projectDir],
    queryFn: () => getDroidClient().listGitBranches({ projectDir }),
    enabled: Boolean(projectDir) && enabled,
  })
}

export function useGitWorktreeBranchesInUseQuery(repoRoot: string, enabled = true) {
  return useQuery({
    queryKey: ['gitWorktreeBranchesInUse', repoRoot],
    queryFn: () => getDroidClient().listGitWorktreeBranchesInUse({ repoRoot }),
    enabled: Boolean(repoRoot) && enabled,
    staleTime: 2000,
  })
}
