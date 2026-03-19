import type { WorkspaceInfo, WorkspaceType } from '@/types'

export function isLocalWorkspaceType(workspaceType?: WorkspaceType | null): boolean {
  return workspaceType === 'local'
}

export function supportsGitWorkspace(workspaceType?: WorkspaceType | null): boolean {
  return workspaceType !== 'local'
}

export function getLaunchProjectDir(params: {
  projectDir?: string | null
  workspaceDir?: string | null
  repoRoot?: string | null
}): string {
  const projectDir = String(params.projectDir || '').trim()
  const workspaceDir = String(params.workspaceDir || '').trim()
  const repoRoot = String(params.repoRoot || '').trim()
  return projectDir || workspaceDir || repoRoot
}

export function getGitWorkspaceDir(params: {
  workspaceDir?: string | null
  projectDir?: string | null
}): string {
  const workspaceDir = String(params.workspaceDir || '').trim()
  const projectDir = String(params.projectDir || '').trim()
  return workspaceDir || projectDir
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
