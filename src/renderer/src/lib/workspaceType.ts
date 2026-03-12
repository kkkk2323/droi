import type { WorkspaceInfo, WorkspaceType } from '@/types'

export function isLocalWorkspaceType(workspaceType?: WorkspaceType | null): boolean {
  return workspaceType === 'local'
}

export function supportsGitWorkspace(workspaceType?: WorkspaceType | null): boolean {
  return workspaceType !== 'local'
}

export function createLocalWorkspaceInfo(params: {
  projectDir: string
  repoRoot?: string
  workspaceDir?: string
  cwdSubpath?: string
}): WorkspaceInfo {
  const projectDir = String(params.projectDir || '').trim()
  const workspaceDir = String(params.workspaceDir || projectDir).trim() || projectDir
  const repoRoot = String(params.repoRoot || workspaceDir).trim() || workspaceDir
  const cwdSubpath = String(params.cwdSubpath || '').trim() || undefined

  return {
    repoRoot,
    projectDir,
    workspaceDir,
    branch: '',
    workspaceType: 'local',
    ...(cwdSubpath ? { cwdSubpath } : {}),
  }
}
