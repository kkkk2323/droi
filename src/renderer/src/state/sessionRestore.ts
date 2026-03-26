import type { LoadSessionResponse, SessionMeta, WorkspaceInfo } from '@/types'
import type { MissionModelSettings } from '@/types'
import { resolveSessionProtocolFields } from '../../../shared/sessionProtocol.ts'
import { DEFAULT_AUTO_LEVEL, DEFAULT_MODEL, makeBuffer, type SessionBuffer } from './appReducer.ts'
import { applyMissionLoadSnapshot } from './missionState.ts'
import { resolveSessionRuntimeSelection } from '../lib/missionModelState.ts'

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

function restorePendingSnapshot(
  buffer: SessionBuffer,
  data?: LoadSessionResponse | null,
): SessionBuffer {
  const isRunning = Boolean(data?.isAgentLoopInProgress)

  return {
    ...buffer,
    pendingPermissionRequests: [],
    pendingAskUserRequests: [],
    isRunning,
    workingState: isRunning ? 'streaming_assistant_message' : undefined,
  }
}

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
  missionModelSettings?: MissionModelSettings | null
}): SessionBuffer {
  const { projectDir, workspace, meta, data, missionModelSettings } = params
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

  const runtimeSelection = resolveSessionRuntimeSelection({
    isMission: protocol.isMission,
    sessionModel: data?.model || meta?.model,
    sessionReasoningEffort: data?.reasoningEffort || meta?.reasoningEffort,
    missionModelSettings,
  })

  const base = makeBuffer(projectDir, workspace)
  const restored = restorePendingSnapshot(
    {
      ...base,
      messages: (data?.messages as SessionBuffer['messages']) ?? [],
      runtimeLogs: Array.isArray(data?.runtimeLogs) ? data.runtimeLogs : [],
      model: runtimeSelection.model || DEFAULT_MODEL,
      autoLevel: data?.autoLevel || meta?.autoLevel || DEFAULT_AUTO_LEVEL,
      missionDir: data?.missionDir || meta?.missionDir,
      missionBaseSessionId: data?.missionBaseSessionId || meta?.missionBaseSessionId,
      isMission: protocol.isMission,
      sessionKind: protocol.sessionKind,
      interactionMode: protocol.interactionMode,
      autonomyLevel: protocol.autonomyLevel,
      decompSessionType: protocol.decompSessionType,
      reasoningEffort: runtimeSelection.reasoningEffort,
      apiKeyFingerprint: data?.apiKeyFingerprint || meta?.apiKeyFingerprint,
    },
    data,
  )

  if (!protocol.isMission) return restored
  return {
    ...restored,
    mission: applyMissionLoadSnapshot(undefined, data?.mission),
  }
}
