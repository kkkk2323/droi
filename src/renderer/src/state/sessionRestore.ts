import type { JsonRpcRequest, LoadSessionResponse, SessionMeta, WorkspaceInfo } from '@/types'
import type { MissionModelSettings } from '@/types'
import { FACTORY_API_VERSION, JSONRPC_VERSION } from '../../../shared/protocol.ts'
import { resolveSessionProtocolFields } from '../../../shared/sessionProtocol.ts'
import {
  DEFAULT_AUTO_LEVEL,
  DEFAULT_MODEL,
  applyRpcRequest,
  makeBuffer,
  type SessionBuffer,
} from './appReducer.ts'
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

function createRestoredRpcRequest(
  requestId: string,
  method: 'droid.request_permission' | 'droid.ask_user',
  params: Record<string, unknown>,
): JsonRpcRequest {
  return {
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'request',
    id: requestId,
    method,
    params,
  }
}

function restorePendingSnapshot(
  sessionId: string,
  buffer: SessionBuffer,
  data?: LoadSessionResponse | null,
): SessionBuffer {
  let next = new Map<string, SessionBuffer>([[sessionId, buffer]])

  for (const pending of data?.pendingPermissions ?? []) {
    const requestId = typeof pending?.requestId === 'string' ? pending.requestId.trim() : ''
    if (!requestId) continue
    next = applyRpcRequest(
      next,
      sessionId,
      createRestoredRpcRequest(requestId, 'droid.request_permission', {
        toolUses: Array.isArray(pending.toolUses) ? pending.toolUses : [],
        confirmationType:
          typeof pending.confirmationType === 'string' ? pending.confirmationType : undefined,
        options: Array.isArray(pending.options) ? pending.options : [],
      }),
    )
  }

  for (const pending of data?.pendingAskUserRequests ?? []) {
    const requestId = typeof pending?.requestId === 'string' ? pending.requestId.trim() : ''
    if (!requestId) continue
    next = applyRpcRequest(
      next,
      sessionId,
      createRestoredRpcRequest(requestId, 'droid.ask_user', {
        toolCallId: typeof pending.toolCallId === 'string' ? pending.toolCallId : '',
        questions: Array.isArray(pending.questions) ? pending.questions : [],
      }),
    )
  }

  const restored = next.get(sessionId) ?? buffer
  const hasPendingAttention =
    (restored.pendingPermissionRequests?.length ?? 0) > 0 ||
    (restored.pendingAskUserRequests?.length ?? 0) > 0
  const isRunning = Boolean(data?.isAgentLoopInProgress || hasPendingAttention)

  return {
    ...restored,
    isRunning,
    workingState: hasPendingAttention
      ? 'waiting_for_tool_confirmation'
      : isRunning
        ? 'streaming_assistant_message'
        : undefined,
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
    data?.id || 'restored-session',
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
