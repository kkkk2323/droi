import type {
  DroidAutonomyLevel,
  DroidInteractionMode,
  LoadSessionResponse,
} from '../../shared/protocol'
import {
  resolveSessionProtocolFields,
  type DecompSessionType,
  type SessionKind,
} from '../../shared/sessionProtocol.ts'

export function toInteractionMode(autoLevel: unknown): DroidInteractionMode {
  const v = typeof autoLevel === 'string' ? autoLevel : 'default'
  return v === 'default' ? 'spec' : 'auto'
}

export function toAutonomyLevel(autoLevel: unknown): DroidAutonomyLevel {
  const v = typeof autoLevel === 'string' ? autoLevel : 'default'
  if (v === 'default') return 'off'
  if (v === 'low') return 'low'
  if (v === 'medium') return 'medium'
  if (v === 'high') return 'high'
  return 'low'
}

export function coerceInteractionMode(value: unknown): DroidInteractionMode | undefined {
  return value === 'spec' || value === 'auto' || value === 'agi' ? value : undefined
}

export function coerceAutonomyLevel(value: unknown): DroidAutonomyLevel | undefined {
  return value === 'off' || value === 'low' || value === 'medium' || value === 'high'
    ? value
    : undefined
}

export function coerceSessionKind(value: unknown): SessionKind | undefined {
  return value === 'mission' || value === 'normal' ? value : undefined
}

export function coerceDecompSessionType(value: unknown): DecompSessionType | undefined {
  return value === 'orchestrator' ? value : undefined
}

export function resolveProtocolPayload(payload: {
  autoLevel?: unknown
  isMission?: unknown
  sessionKind?: unknown
  interactionMode?: unknown
  autonomyLevel?: unknown
  decompSessionType?: unknown
}) {
  return resolveSessionProtocolFields({
    autoLevel: payload.autoLevel,
    explicit: {
      isMission: payload.isMission === true ? true : undefined,
      sessionKind: coerceSessionKind(payload.sessionKind),
      interactionMode: coerceInteractionMode(payload.interactionMode),
      autonomyLevel: coerceAutonomyLevel(payload.autonomyLevel),
      decompSessionType: coerceDecompSessionType(payload.decompSessionType),
    },
  })
}

export function mergeLoadSessionResponse(
  stored: LoadSessionResponse | null,
  live: Record<string, unknown> | null,
): LoadSessionResponse | null {
  if (!stored || !live) return stored

  const settings =
    live.settings && typeof live.settings === 'object'
      ? (live.settings as Record<string, unknown>)
      : {}
  const protocol = resolveSessionProtocolFields({
    autoLevel: stored.autoLevel,
    explicit: {
      isMission: (live as any).isMission ?? stored.isMission,
      sessionKind: coerceSessionKind((live as any).sessionKind) || stored.sessionKind,
      interactionMode:
        coerceInteractionMode((live as any).interactionMode) ||
        coerceInteractionMode(settings.interactionMode) ||
        stored.interactionMode,
      autonomyLevel:
        coerceAutonomyLevel((live as any).autonomyLevel) ||
        coerceAutonomyLevel(settings.autonomyLevel) ||
        stored.autonomyLevel,
      decompSessionType:
        coerceDecompSessionType((live as any).decompSessionType) || stored.decompSessionType,
    },
  })

  return {
    ...stored,
    model:
      (typeof (live as any).modelId === 'string' ? (live as any).modelId : undefined) ||
      (typeof settings.modelId === 'string' ? settings.modelId : undefined) ||
      stored.model,
    missionDir:
      (typeof (live as any).missionDir === 'string' ? (live as any).missionDir : undefined) ||
      stored.missionDir,
    isMission: protocol.isMission,
    sessionKind: protocol.sessionKind,
    interactionMode: protocol.interactionMode,
    autonomyLevel: protocol.autonomyLevel,
    decompSessionType: protocol.decompSessionType,
    reasoningEffort:
      (typeof (live as any).reasoningEffort === 'string'
        ? (live as any).reasoningEffort
        : undefined) ||
      (typeof settings.reasoningEffort === 'string' ? settings.reasoningEffort : undefined) ||
      stored.reasoningEffort,
    messages: stored.messages,
    pendingPermissions: [],
    pendingAskUserRequests: [],
    isAgentLoopInProgress:
      typeof (live as any).isAgentLoopInProgress === 'boolean'
        ? Boolean((live as any).isAgentLoopInProgress)
        : stored.isAgentLoopInProgress,
    mission:
      live.mission && typeof live.mission === 'object'
        ? ({ ...(live.mission as Record<string, unknown>) } as any)
        : stored.mission,
  }
}
