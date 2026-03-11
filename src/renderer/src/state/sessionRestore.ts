import type { LoadSessionResponse, SessionMeta, WorkspaceInfo } from '@/types'
import { resolveSessionProtocolFields } from '../../../shared/sessionProtocol.ts'
import { DEFAULT_AUTO_LEVEL, DEFAULT_MODEL, makeBuffer, type SessionBuffer } from './appReducer.ts'
import { applyMissionLoadSnapshot } from './missionState.ts'

type RestorableSessionMeta = Partial<
  Pick<
    SessionMeta,
    | 'autoLevel'
    | 'model'
    | 'missionDir'
    | 'missionBaseSessionId'
    | 'isMission'
    | 'sessionKind'
    | 'interactionMode'
    | 'autonomyLevel'
    | 'decompSessionType'
    | 'reasoningEffort'
    | 'apiKeyFingerprint'
    | 'baseBranch'
  >
>

export function buildRestoredSessionBuffer(params: {
  projectDir: string
  workspace?: Partial<
    Pick<
      WorkspaceInfo,
      'repoRoot' | 'workspaceDir' | 'cwdSubpath' | 'branch' | 'workspaceType' | 'baseBranch'
    >
  >
  meta?: RestorableSessionMeta | null
  data?: LoadSessionResponse | null
}): SessionBuffer {
  const { projectDir, workspace, meta, data } = params
  const protocol = resolveSessionProtocolFields({
    autoLevel: data?.autoLevel || meta?.autoLevel,
    explicit: {
      isMission: data?.isMission ?? meta?.isMission,
      sessionKind: data?.sessionKind || meta?.sessionKind,
      interactionMode: data?.interactionMode || meta?.interactionMode,
      autonomyLevel: data?.autonomyLevel || meta?.autonomyLevel,
      decompSessionType: data?.decompSessionType || meta?.decompSessionType,
    },
  })

  const base = makeBuffer(projectDir, workspace)
  const restored: SessionBuffer = {
    ...base,
    messages: (data?.messages as SessionBuffer['messages']) ?? [],
    runtimeLogs: Array.isArray(data?.runtimeLogs) ? data.runtimeLogs : [],
    model: data?.model || meta?.model || DEFAULT_MODEL,
    autoLevel: data?.autoLevel || meta?.autoLevel || DEFAULT_AUTO_LEVEL,
    missionDir: data?.missionDir || meta?.missionDir,
    missionBaseSessionId: data?.missionBaseSessionId || meta?.missionBaseSessionId,
    isMission: protocol.isMission,
    sessionKind: protocol.sessionKind,
    interactionMode: protocol.interactionMode,
    autonomyLevel: protocol.autonomyLevel,
    decompSessionType: protocol.decompSessionType,
    reasoningEffort: data?.reasoningEffort || meta?.reasoningEffort || '',
    apiKeyFingerprint: data?.apiKeyFingerprint || meta?.apiKeyFingerprint,
  }

  if (!protocol.isMission) return restored
  return {
    ...restored,
    mission: applyMissionLoadSnapshot(undefined, data?.mission),
  }
}
