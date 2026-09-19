import type { AvailableModelConfig, MissionModelSettings } from '../types.ts'
import { getRuntimeModelDefaultReasoning } from './modelCatalog.ts'
import { DEFAULT_MODEL } from '../state/appReducer.ts'

function normalizeString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

export function getMissionOrchestratorModel(
  missionModelSettings?: MissionModelSettings | null,
  fallbackModel?: string | null,
): string {
  return (
    normalizeString(missionModelSettings?.orchestratorModel) ||
    normalizeString(fallbackModel) ||
    DEFAULT_MODEL
  )
}

export function resolveSessionRuntimeSelection(params: {
  isMission?: boolean
  sessionModel?: string | null
  sessionReasoningEffort?: string | null
  missionModelSettings?: MissionModelSettings | null
  availableModels?: AvailableModelConfig[]
}): {
  model: string
  reasoningEffort: string
} {
  const sessionModel = normalizeString(params.sessionModel) || DEFAULT_MODEL
  const sessionReasoningEffort = normalizeString(params.sessionReasoningEffort) || ''
  const model = params.isMission
    ? getMissionOrchestratorModel(params.missionModelSettings, sessionModel)
    : sessionModel

  if (!params.isMission || model === sessionModel) {
    return {
      model,
      reasoningEffort: sessionReasoningEffort,
    }
  }

  return {
    model,
    reasoningEffort: getRuntimeModelDefaultReasoning(model, params.availableModels) || '',
  }
}
