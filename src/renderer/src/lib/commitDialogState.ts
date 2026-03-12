export interface CommitDialogHostState {
  open: boolean
  projectDir: string
}

function normalizeProjectDir(projectDir?: string | null): string {
  return typeof projectDir === 'string' ? projectDir.trim() : ''
}

export function resolveCommitDialogHostState(params: {
  activeProjectDir?: string | null
  requestedProjectDir?: string | null
}): CommitDialogHostState {
  const requestedProjectDir = normalizeProjectDir(params.requestedProjectDir)
  const activeProjectDir = normalizeProjectDir(params.activeProjectDir)

  return {
    open: requestedProjectDir.length > 0,
    projectDir: requestedProjectDir || activeProjectDir,
  }
}
