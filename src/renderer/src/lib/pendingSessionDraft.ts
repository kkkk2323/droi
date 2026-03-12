import {
  autonomyLevelFromAutoLevel,
  resolveSessionProtocolFields,
  type SessionKind,
} from '../../../shared/sessionProtocol.ts'
import type { WorkspaceType } from '@/types'

export type PendingSessionDraftMode = 'local' | 'new-worktree'

export interface PendingSessionDraft {
  repoRoot: string
  projectDir?: string
  workspaceDir?: string
  cwdSubpath?: string
  workspaceType?: WorkspaceType
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
    ...(nextPatch.workspaceType === 'local' || current.workspaceType === 'local'
      ? {
          branch: '',
          isExistingBranch: false,
          mode: 'local' as const,
          sessionKind: 'normal' as const,
        }
      : {}),
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
    workspaceType: nextPatch.workspaceType || current.workspaceType,
    branch:
      nextPatch.workspaceType === 'local' || current.workspaceType === 'local'
        ? ''
        : typeof nextPatch.branch === 'string'
          ? String(nextPatch.branch).trim()
          : current.branch,
    mode:
      nextPatch.workspaceType === 'local' || current.workspaceType === 'local'
        ? 'local'
        : nextPatch.mode || current.mode || 'local',
    sessionKind:
      nextPatch.workspaceType === 'local' || current.workspaceType === 'local'
        ? 'normal'
        : nextPatch.sessionKind || current.sessionKind || 'normal',
    isExistingBranch:
      nextPatch.workspaceType === 'local' || current.workspaceType === 'local'
        ? false
        : (nextPatch.isExistingBranch ?? current.isExistingBranch),
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
