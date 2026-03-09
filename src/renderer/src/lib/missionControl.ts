import type { MissionDiskHandoff, MissionDiskObject } from '../../../shared/mission.ts'
import type { MissionState } from '@/state/missionState'

export interface MissionControlStatusSummary {
  stateLabel: string
  progressLabel: string
  currentFeatureLabel: string
  phaseLabel: string
}

export interface MissionFeatureQueueItem {
  id: string
  description: string
  status: string
  statusLabel: string
  isCurrent: boolean
  isValidator: boolean
  testId: string
}

export interface MissionProgressTimelineItem {
  eventLabel: string
  detailLabel: string
  timestampLabel: string
}

export interface MissionHandoffCardData {
  featureId: string
  title: string
  testId: string
  successState: string
  salientSummary: string
  whatWasImplemented: string
  commandResults: string[]
  interactiveResults: string[]
}

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function asTrimmedString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

function toTitleCase(value: string): string {
  if (!value) return 'Unknown'
  return value
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function toSentenceCaseLabel(value: string): string {
  const normalized = value
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.toLowerCase())
    .join(' ')
  if (!normalized) return 'Mission event'
  return normalized.charAt(0).toUpperCase() + normalized.slice(1)
}

function isDaemonLikeText(value: string | undefined): boolean {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
  if (!normalized) return false
  return /(factoryd|daemon|authentication|spawn|socket|transport|connection|connect)/.test(
    normalized,
  )
}

function stableStringify(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map((entry) => stableStringify(entry)).join(',')}]`
  if (isObject(value)) {
    return `{${Object.entries(value)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, entry]) => `${JSON.stringify(key)}:${stableStringify(entry)}`)
      .join(',')}}`
  }
  return JSON.stringify(value)
}

function featureStatus(feature: MissionDiskObject | undefined): string {
  return (
    asTrimmedString(feature?.status) ??
    asTrimmedString(feature?.state) ??
    'pending'
  ).toLowerCase()
}

function isValidatorFeature(feature: MissionDiskObject | undefined): boolean {
  const skillName = asTrimmedString(feature?.skillName) ?? ''
  return skillName === 'scrutiny-validator' || skillName === 'user-testing-validator'
}

function getFeatureLabel(feature: MissionDiskObject | undefined, fallback?: string): string {
  return (
    asTrimmedString(feature?.description) ??
    asTrimmedString(feature?.title) ??
    fallback ??
    'Waiting for feature selection'
  )
}

function findCurrentFeature(mission?: MissionState | null): MissionDiskObject | undefined {
  if (!mission?.currentFeatureId) return undefined
  return mission.features.find(
    (feature) => asTrimmedString(feature?.id) === mission.currentFeatureId,
  )
}

function hasPendingValidatorWork(mission?: MissionState | null): boolean {
  return (mission?.features || []).some((feature) => {
    if (!isValidatorFeature(feature)) return false
    const status = featureStatus(feature)
    return status !== 'completed' && status !== 'done' && status !== 'cancelled'
  })
}

function hasPendingCompletionGate(mission?: MissionState | null): boolean {
  return (
    (asTrimmedString(mission?.currentState) ?? '').toLowerCase() === 'completed' &&
    !mission?.isCompleted
  )
}

function formatMissionTimestamp(value: unknown): string {
  const raw = asTrimmedString(value)
  if (!raw) return 'Pending timestamp'
  const parsed = Date.parse(raw)
  if (!Number.isFinite(parsed)) return raw
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(parsed))
}

function getProgressEntryDetail(entry: MissionDiskObject): string {
  const detailParts = [
    asTrimmedString(entry.message),
    asTrimmedString(entry.reason),
    asTrimmedString(entry.milestone),
    asTrimmedString(entry.featureId),
  ].filter(Boolean) as string[]

  const workerSessionId = asTrimmedString(entry.workerSessionId)
  if (workerSessionId) detailParts.push(workerSessionId)

  const successState = asTrimmedString(entry.successState)
  if (successState) {
    const normalized = successState.toLowerCase()
    detailParts.push(
      normalized === 'success'
        ? 'Succeeded'
        : normalized === 'partial'
          ? 'Partially succeeded'
          : normalized === 'failure'
            ? 'Failed'
            : toTitleCase(successState),
    )
  }

  return detailParts.join(' · ')
}

function getProgressEventLabel(entry: MissionDiskObject): string {
  const type = asTrimmedString(entry.type) ?? 'mission_event'
  const normalized = type.toLowerCase()
  const reason = asTrimmedString(entry.reason) ?? asTrimmedString(entry.message)

  if (normalized === 'worker_failed') {
    if (/killed by user/i.test(reason || '')) return 'Worker killed by user'
    if (isDaemonLikeText(reason)) return 'Daemon failure'
    return 'Worker failed'
  }

  if (normalized === 'worker_paused') return 'Worker paused'
  if (normalized === 'mission_paused') return 'Mission paused'

  return toSentenceCaseLabel(type)
}

function resolveHandoffBody(payload: MissionDiskObject): MissionDiskObject {
  const nested = payload.handoff
  return isObject(nested) ? (nested as MissionDiskObject) : payload
}

function mapCommandResults(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value
    .map((entry) => {
      if (!isObject(entry)) return null
      const command = asTrimmedString(entry.command)
      const observation = asTrimmedString(entry.observation)
      if (!command && !observation) return null
      return [command, observation].filter(Boolean).join(' — ')
    })
    .filter(Boolean) as string[]
}

function mapInteractiveResults(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value
    .map((entry) => {
      if (!isObject(entry)) return null
      const action = asTrimmedString(entry.action)
      const observed = asTrimmedString(entry.observed)
      if (!action && !observed) return null
      return [action, observed].filter(Boolean).join(' — ')
    })
    .filter(Boolean) as string[]
}

export function getMissionControlStatus(
  mission?: MissionState | null,
): MissionControlStatusSummary {
  const feature = findCurrentFeature(mission)
  const progressLabel = `${mission?.completedFeatures ?? 0}/${mission?.totalFeatures ?? 0} completed`
  const completionGatePending = hasPendingCompletionGate(mission)
  const validationInProgress = hasPendingValidatorWork(mission)

  return {
    stateLabel: completionGatePending
      ? 'Validation pending'
      : mission?.isCompleted
        ? 'Completed'
        : toTitleCase(asTrimmedString(mission?.currentState) ?? 'unknown'),
    progressLabel,
    currentFeatureLabel: getFeatureLabel(feature, asTrimmedString(mission?.currentFeatureId)),
    phaseLabel: validationInProgress
      ? 'Validation in progress'
      : completionGatePending
        ? 'Waiting for validation settlement'
        : mission?.isCompleted
          ? 'Mission completed'
          : 'Implementation in progress',
  }
}

export function getMissionFeatureQueueItems(
  mission?: MissionState | null,
): MissionFeatureQueueItem[] {
  return (mission?.features || []).map((feature) => {
    const id = asTrimmedString(feature.id) ?? 'unknown-feature'
    const status = featureStatus(feature)
    return {
      id,
      description: getFeatureLabel(feature, id),
      status,
      statusLabel: toTitleCase(status),
      isCurrent: id === mission?.currentFeatureId,
      isValidator: isValidatorFeature(feature),
      testId: `mission-feature-${id}`,
    }
  })
}

export function getMissionProgressTimelineItems(
  mission?: MissionState | null,
): MissionProgressTimelineItem[] {
  const seen = new Set<string>()
  const deduped = (mission?.progressEntries || []).filter((entry) => {
    const fingerprint = stableStringify(entry)
    if (seen.has(fingerprint)) return false
    seen.add(fingerprint)
    return true
  })

  return deduped
    .map((entry, index) => {
      const rawTimestamp = asTrimmedString(entry.timestamp)
      const parsedTimestamp = rawTimestamp ? Date.parse(rawTimestamp) : Number.NaN
      return {
        entry,
        index,
        parsedTimestamp,
      }
    })
    .sort((left, right) => {
      const leftValid = Number.isFinite(left.parsedTimestamp)
      const rightValid = Number.isFinite(right.parsedTimestamp)
      if (leftValid && rightValid) {
        return left.parsedTimestamp - right.parsedTimestamp || left.index - right.index
      }
      return left.index - right.index
    })
    .map(({ entry }) => ({
      eventLabel: getProgressEventLabel(entry),
      detailLabel: getProgressEntryDetail(entry),
      timestampLabel: formatMissionTimestamp(entry.timestamp),
    }))
}

export function getMissionHandoffCards(mission?: MissionState | null): MissionHandoffCardData[] {
  return (mission?.handoffs || []).map((handoff: MissionDiskHandoff) => {
    const payload = handoff.payload || {}
    const body = resolveHandoffBody(payload)
    const featureId =
      asTrimmedString(payload.featureId) ??
      asTrimmedString(body.featureId) ??
      handoff.fileName.replace(/\.json$/i, '')
    const verification = isObject(body.verification) ? body.verification : undefined

    return {
      featureId,
      title: featureId,
      testId: `mission-handoff-${featureId}`,
      successState:
        asTrimmedString(payload.successState) ?? asTrimmedString(body.successState) ?? 'unknown',
      salientSummary: asTrimmedString(body.salientSummary) ?? 'No handoff summary was provided.',
      whatWasImplemented:
        asTrimmedString(body.whatWasImplemented) ?? 'Implementation details were not provided.',
      commandResults: mapCommandResults(verification?.commandsRun),
      interactiveResults: mapInteractiveResults(verification?.interactiveChecks),
    }
  })
}
