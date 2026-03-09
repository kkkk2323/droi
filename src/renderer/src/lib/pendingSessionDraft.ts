import {
  autonomyLevelFromAutoLevel,
  resolveSessionProtocolFields,
  type SessionKind,
} from '../../../shared/sessionProtocol.ts'

export type PendingSessionDraftMode = 'local' | 'new-worktree'

export interface PendingSessionDraft {
  repoRoot: string
  projectDir?: string
  workspaceDir?: string
  cwdSubpath?: string
  branch: string
  isExistingBranch?: boolean
  mode?: PendingSessionDraftMode
  sessionKind?: SessionKind
}

export function mergePendingSessionDraft<T extends PendingSessionDraft>(
  current: T,
  patch?: Partial<T>,
): T {
  const nextPatch: Partial<T> = patch || {}

  return {
    ...current,
    ...nextPatch,
    repoRoot:
      typeof nextPatch.repoRoot === 'string' ? String(nextPatch.repoRoot).trim() : current.repoRoot,
    projectDir:
      typeof nextPatch.projectDir === 'string'
        ? String(nextPatch.projectDir).trim()
        : current.projectDir,
    workspaceDir:
      typeof nextPatch.workspaceDir === 'string'
        ? String(nextPatch.workspaceDir).trim()
        : current.workspaceDir,
    cwdSubpath:
      typeof nextPatch.cwdSubpath === 'string'
        ? String(nextPatch.cwdSubpath).trim()
        : current.cwdSubpath,
    branch: typeof nextPatch.branch === 'string' ? String(nextPatch.branch).trim() : current.branch,
    mode: nextPatch.mode || current.mode || 'local',
    sessionKind: nextPatch.sessionKind || current.sessionKind || 'normal',
  }
}

export function getPendingSessionProtocol(
  pending: Pick<PendingSessionDraft, 'sessionKind'> | null | undefined,
  autoLevel: unknown,
) {
  const protocol = resolveSessionProtocolFields({
    autoLevel,
    explicit: {
      sessionKind: pending?.sessionKind,
    },
  })

  if (pending?.sessionKind === 'mission' && !protocol.autonomyLevel) {
    return {
      ...protocol,
      autonomyLevel: autonomyLevelFromAutoLevel(autoLevel),
    }
  }

  return protocol
}
