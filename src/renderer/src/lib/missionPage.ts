import type { MissionDiskObject } from '../../../shared/mission.ts'
import type { MissionState } from '@/state/missionState'

export type MissionViewMode = 'chat' | 'mission-control'

export interface MissionSessionViewState {
  viewMode: MissionViewMode
  manualOverrideAt?: number
}

export const MISSION_AUTO_SWITCH_COOLDOWN_MS = 30_000

export function getPreferredMissionView(mission?: MissionState | null): MissionViewMode | null {
  const state = String(mission?.currentState || '')
    .trim()
    .toLowerCase()
  if (state === 'running') return 'mission-control'
  if (state === 'paused' || state === 'orchestrator_turn') return 'chat'
  return null
}

export function shouldApplyMissionAutoSwitch(params: {
  currentView: MissionViewMode
  preferredView: MissionViewMode | null
  manualOverrideAt?: number
  now?: number
}): boolean {
  const { currentView, preferredView, manualOverrideAt } = params
  const now = params.now ?? Date.now()
  if (!preferredView || preferredView === currentView) return false
  if (typeof manualOverrideAt !== 'number') return true
  return now - manualOverrideAt >= MISSION_AUTO_SWITCH_COOLDOWN_MS
}

export function getMissionSessionViewState(params: {
  sessionId?: string | null
  mission?: MissionState | null
  sessionViewStates?: Record<string, MissionSessionViewState>
}): MissionSessionViewState {
  const preferredView = getPreferredMissionView(params.mission) ?? 'chat'
  const sessionId = String(params.sessionId || '').trim()
  if (!sessionId) {
    return {
      viewMode: preferredView,
      manualOverrideAt: undefined,
    }
  }

  return (
    params.sessionViewStates?.[sessionId] ?? {
      viewMode: preferredView,
      manualOverrideAt: undefined,
    }
  )
}

function toTitleCase(value: string): string {
  if (!value) return 'Unknown'
  return value
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function findCurrentFeature(mission?: MissionState | null): MissionDiskObject | undefined {
  if (!mission?.currentFeatureId) return undefined
  return mission.features.find(
    (feature) => String(feature?.id || '').trim() === mission.currentFeatureId,
  )
}

export function truncateWorkerSessionId(workerSessionId?: string | null): string {
  const value = String(workerSessionId || '').trim()
  if (!value) return 'No active worker'
  if (value.length <= 16) return value
  return `${value.slice(0, 8)}…${value.slice(-4)}`
}

export function getMissionStatusSummary(mission?: MissionState | null): {
  stateLabel: string
  progressLabel: string
  currentFeatureLabel: string
  workerLabel: string
} {
  const completed = mission?.completedFeatures ?? 0
  const total = mission?.totalFeatures ?? 0
  const feature = findCurrentFeature(mission)
  const featureLabel =
    String((feature?.description as string) || '').trim() ||
    String((feature?.title as string) || '').trim() ||
    String(mission?.currentFeatureId || '').trim() ||
    'Waiting for feature selection'

  return {
    stateLabel: toTitleCase(String(mission?.currentState || '').trim() || 'unknown'),
    progressLabel: `${completed}/${total}`,
    currentFeatureLabel: featureLabel,
    workerLabel: truncateWorkerSessionId(
      mission?.liveWorkerSessionId || mission?.currentWorkerSessionId || undefined,
    ),
  }
}
