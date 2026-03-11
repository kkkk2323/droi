import type { MissionDiskObject } from '../../../shared/mission.ts'
import { getMissionHandoffCards, type MissionHandoffCardData } from './missionControl.ts'
import type { MissionState } from '@/state/missionState'

export type MissionWorkerStatus =
  | 'running'
  | 'paused'
  | 'success'
  | 'partial'
  | 'failed'
  | 'unknown'
export type MissionWorkerListFilter = 'all' | 'active' | 'completed' | 'failed'

export interface MissionWorkerSummary {
  workerSessionId: string
  featureId?: string
  featureTitle: string
  status: MissionWorkerStatus
  statusLabel: string
  successState?: string
  failureReason?: string
  startedAt?: number
  endedAt?: number
  lastUpdatedAt?: number
  durationMs?: number
  isCurrent: boolean
  hasHandoff: boolean
}

export interface MissionWorkerCounts {
  all: number
  active: number
  completed: number
  failed: number
}

export interface MissionWorkerProgressItem {
  eventLabel: string
  detailLabel: string
  timestampLabel: string
  timestampMs?: number
}

type WorkerAccumulator = {
  workerSessionId: string
  featureId?: string
  startedAt?: number
  completedAt?: number
  failedAt?: number
  pausedAt?: number
  lastEventType?: string
  lastEventTimestamp?: number
  successState?: string
  failureReason?: string
}

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function asTrimmedString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map((entry) => asTrimmedString(entry)).filter(Boolean) as string[]
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

function readIsoTimestamp(value: unknown): number | undefined {
  const raw = asTrimmedString(value)
  if (!raw) return undefined
  const parsed = Date.parse(raw)
  return Number.isFinite(parsed) ? parsed : undefined
}

function formatMissionTimestamp(value?: number): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return 'Pending timestamp'
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(value))
}

function featureStatus(feature: MissionDiskObject | undefined): string {
  return (
    asTrimmedString(feature?.status) ??
    asTrimmedString(feature?.state) ??
    'pending'
  ).toLowerCase()
}

function getFeatureLabel(feature: MissionDiskObject | undefined, fallback?: string): string {
  return (
    asTrimmedString(feature?.description) ??
    asTrimmedString(feature?.title) ??
    fallback ??
    'Unassigned feature'
  )
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

function sortProgressEntries(entries: MissionDiskObject[]): MissionDiskObject[] {
  return entries
    .map((entry, index) => ({
      entry,
      index,
      timestamp: readIsoTimestamp(entry.timestamp) ?? Number.NaN,
    }))
    .sort((left, right) => {
      const leftValid = Number.isFinite(left.timestamp)
      const rightValid = Number.isFinite(right.timestamp)
      if (leftValid && rightValid)
        return left.timestamp - right.timestamp || left.index - right.index
      return left.index - right.index
    })
    .map((item) => item.entry)
}

function isCompletedWorkerStatus(status: MissionWorkerStatus): boolean {
  return status === 'success' || status === 'partial'
}

function isActiveWorkerStatus(status: MissionWorkerStatus): boolean {
  return status === 'running' || status === 'paused'
}

function statusToLabel(status: MissionWorkerStatus): string {
  return status === 'unknown' ? 'Unknown' : toTitleCase(status)
}

function compareWorkers(left: MissionWorkerSummary, right: MissionWorkerSummary): number {
  const leftTimestamp = left.startedAt ?? left.lastUpdatedAt ?? 0
  const rightTimestamp = right.startedAt ?? right.lastUpdatedAt ?? 0
  if (leftTimestamp !== rightTimestamp) return rightTimestamp - leftTimestamp
  return left.workerSessionId.localeCompare(right.workerSessionId)
}

function getFeatureIdFromWorker(featureIdByWorker: Map<string, string>, workerSessionId: string) {
  return featureIdByWorker.get(workerSessionId)
}

function getCurrentWorkerIds(mission?: MissionState | null): Set<string> {
  const ids = new Set<string>()
  const runningWorker = asTrimmedString(
    mission?.liveWorkerSessionId ?? mission?.currentWorkerSessionId,
  )
  const pausedWorker = asTrimmedString(mission?.pausedWorkerSessionId)
  if (runningWorker) ids.add(runningWorker)
  if (pausedWorker) ids.add(pausedWorker)
  return ids
}

function getWorkerStatus(params: {
  mission?: MissionState | null
  worker: WorkerAccumulator
}): MissionWorkerStatus {
  const { mission, worker } = params
  const workerSessionId = worker.workerSessionId
  const currentState = asTrimmedString(mission?.currentState)?.toLowerCase()
  const runningWorker = asTrimmedString(
    mission?.liveWorkerSessionId ?? mission?.currentWorkerSessionId,
  )
  const pausedWorker = asTrimmedString(
    mission?.pausedWorkerSessionId ?? mission?.currentWorkerSessionId,
  )

  if (worker.successState === 'success') return 'success'
  if (worker.successState === 'partial') return 'partial'
  if (worker.successState === 'failure') return 'failed'
  if (worker.failedAt || worker.lastEventType === 'worker_failed') return 'failed'
  if (currentState === 'paused' && pausedWorker === workerSessionId) return 'paused'
  if (worker.lastEventType === 'worker_paused') return 'paused'
  if (currentState === 'running' && runningWorker === workerSessionId) return 'running'
  if (
    worker.lastEventType === 'worker_started' ||
    worker.lastEventType === 'worker_selected_feature'
  ) {
    return 'running'
  }

  const feature = (mission?.features || []).find((entry) => {
    const currentWorkerSessionId = asTrimmedString((entry as any)?.currentWorkerSessionId)
    return currentWorkerSessionId === workerSessionId
  })
  if (feature) {
    const status = featureStatus(feature)
    if (status === 'in_progress' || status === 'running') return 'running'
  }

  return 'unknown'
}

function getWorkerDurationMs(worker: WorkerAccumulator, status: MissionWorkerStatus, now: number) {
  if (!worker.startedAt) return undefined
  const finishedAt = worker.completedAt ?? worker.failedAt
  if (finishedAt && finishedAt >= worker.startedAt) return finishedAt - worker.startedAt
  if (isActiveWorkerStatus(status)) return Math.max(0, now - worker.startedAt)
  if (worker.lastEventTimestamp && worker.lastEventTimestamp >= worker.startedAt) {
    return worker.lastEventTimestamp - worker.startedAt
  }
  return undefined
}

export function getMissionWorkerSummaries(
  mission?: MissionState | null,
  params?: { now?: number },
): MissionWorkerSummary[] {
  if (!mission) return []

  const now = params?.now ?? Date.now()
  const featureById = new Map<string, MissionDiskObject>()
  const featureIdByWorker = new Map<string, string>()
  const workerMap = new Map<string, WorkerAccumulator>()
  const handoffFeatureIds = new Set(
    getMissionHandoffCards(mission).map((handoff) => handoff.featureId),
  )

  const ensureWorker = (workerSessionId: string) => {
    const existing = workerMap.get(workerSessionId)
    if (existing) return existing
    const next: WorkerAccumulator = { workerSessionId }
    workerMap.set(workerSessionId, next)
    return next
  }

  const registerWorkerIds = (workerSessionIds: string[]) => {
    for (const workerSessionId of workerSessionIds) ensureWorker(workerSessionId)
  }

  registerWorkerIds(asStringArray((mission.state as any)?.workerSessionIds))
  registerWorkerIds(
    [mission.currentWorkerSessionId, mission.liveWorkerSessionId, mission.pausedWorkerSessionId]
      .map((entry) => asTrimmedString(entry))
      .filter(Boolean) as string[],
  )

  for (const feature of mission.features || []) {
    const featureId = asTrimmedString(feature.id)
    if (featureId) featureById.set(featureId, feature)
    const workerSessionIds = asStringArray((feature as any)?.workerSessionIds)
    registerWorkerIds(workerSessionIds)
    for (const workerSessionId of workerSessionIds) {
      if (featureId && !featureIdByWorker.has(workerSessionId)) {
        featureIdByWorker.set(workerSessionId, featureId)
      }
    }

    for (const workerSessionId of [
      asTrimmedString((feature as any)?.currentWorkerSessionId),
      asTrimmedString((feature as any)?.completedWorkerSessionId),
    ].filter(Boolean) as string[]) {
      ensureWorker(workerSessionId)
      if (featureId) featureIdByWorker.set(workerSessionId, featureId)
    }
  }

  for (const entry of mission.progressEntries || []) {
    const workerSessionId = asTrimmedString((entry as any)?.workerSessionId)
    if (!workerSessionId) continue
    const worker = ensureWorker(workerSessionId)
    const featureId = asTrimmedString((entry as any)?.featureId)
    if (featureId) {
      worker.featureId = featureId
      featureIdByWorker.set(workerSessionId, featureId)
    }
    const type = asTrimmedString((entry as any)?.type)?.toLowerCase()
    const timestamp = readIsoTimestamp((entry as any)?.timestamp)
    if (timestamp !== undefined) worker.lastEventTimestamp = timestamp
    if (type) worker.lastEventType = type

    if (type === 'worker_started' && timestamp !== undefined) {
      worker.startedAt =
        worker.startedAt === undefined ? timestamp : Math.min(worker.startedAt, timestamp)
    }
    if (type === 'worker_completed') {
      worker.completedAt = timestamp ?? worker.completedAt
      worker.successState = asTrimmedString((entry as any)?.successState)?.toLowerCase()
      if (worker.successState === 'failure') {
        worker.failureReason = asTrimmedString((entry as any)?.reason) ?? worker.failureReason
      }
    }
    if (type === 'worker_failed') {
      worker.failedAt = timestamp ?? worker.failedAt
      worker.failureReason = asTrimmedString((entry as any)?.reason) ?? worker.failureReason
    }
    if (type === 'worker_paused') {
      worker.pausedAt = timestamp ?? worker.pausedAt
    }
  }

  const currentWorkerIds = getCurrentWorkerIds(mission)

  return Array.from(workerMap.values())
    .map((worker) => {
      const featureId =
        worker.featureId ?? getFeatureIdFromWorker(featureIdByWorker, worker.workerSessionId)
      const feature = featureId ? featureById.get(featureId) : undefined
      const status = getWorkerStatus({ mission, worker })
      const endedAt = worker.completedAt ?? worker.failedAt
      return {
        workerSessionId: worker.workerSessionId,
        featureId,
        featureTitle: getFeatureLabel(feature, featureId),
        status,
        statusLabel: statusToLabel(status),
        successState: worker.successState,
        failureReason: worker.failureReason,
        startedAt: worker.startedAt,
        endedAt,
        lastUpdatedAt: worker.lastEventTimestamp ?? endedAt ?? worker.startedAt,
        durationMs: getWorkerDurationMs(worker, status, now),
        isCurrent: currentWorkerIds.has(worker.workerSessionId),
        hasHandoff: Boolean(featureId && handoffFeatureIds.has(featureId)),
      }
    })
    .sort(compareWorkers)
}

export function getMissionWorkerCounts(workers: MissionWorkerSummary[]): MissionWorkerCounts {
  return workers.reduce<MissionWorkerCounts>(
    (counts, worker) => {
      counts.all += 1
      if (isActiveWorkerStatus(worker.status)) counts.active += 1
      if (isCompletedWorkerStatus(worker.status)) counts.completed += 1
      if (worker.status === 'failed') counts.failed += 1
      return counts
    },
    { all: 0, active: 0, completed: 0, failed: 0 },
  )
}

export function filterMissionWorkers(
  workers: MissionWorkerSummary[],
  filter: MissionWorkerListFilter,
): MissionWorkerSummary[] {
  if (filter === 'all') return workers
  if (filter === 'active') return workers.filter((worker) => isActiveWorkerStatus(worker.status))
  if (filter === 'completed') {
    return workers.filter((worker) => isCompletedWorkerStatus(worker.status))
  }
  return workers.filter((worker) => worker.status === 'failed')
}

export function getMissionWorkerProgressItems(
  mission: MissionState | null | undefined,
  workerSessionId: string,
): MissionWorkerProgressItem[] {
  if (!mission || !workerSessionId) return []

  return sortProgressEntries(mission.progressEntries || [])
    .filter((entry) => asTrimmedString((entry as any)?.workerSessionId) === workerSessionId)
    .map((entry) => {
      const timestampMs = readIsoTimestamp((entry as any)?.timestamp)
      return {
        eventLabel: getProgressEventLabel(entry),
        detailLabel: getProgressEntryDetail(entry),
        timestampLabel: formatMissionTimestamp(timestampMs),
        timestampMs,
      }
    })
}

export function getMissionWorkerHandoffs(
  mission: MissionState | null | undefined,
  worker: Pick<MissionWorkerSummary, 'featureId'> | null | undefined,
): MissionHandoffCardData[] {
  const featureId = asTrimmedString(worker?.featureId)
  if (!mission || !featureId) return []
  return getMissionHandoffCards(mission).filter((handoff) => handoff.featureId === featureId)
}

export function getMissionWorkerStatusVariant(
  status: MissionWorkerStatus,
): 'default' | 'secondary' | 'destructive' | 'warning' | 'outline' {
  if (status === 'success') return 'default'
  if (status === 'running' || status === 'partial') return 'secondary'
  if (status === 'paused') return 'warning'
  if (status === 'failed') return 'destructive'
  return 'outline'
}

export function getMissionWorkerStateCopy(worker: MissionWorkerSummary): string {
  if (worker.status === 'failed') {
    return worker.failureReason || 'This worker ended with a failure.'
  }
  if (worker.status === 'paused') return 'This worker is paused and can resume from Mission chat.'
  if (worker.status === 'running')
    return 'This worker is still running and its runtime transcript will keep updating.'
  if (worker.status === 'partial') return 'This worker returned a partial handoff.'
  if (worker.status === 'success') return 'This worker completed successfully.'
  return 'This worker has not reported a terminal status yet.'
}

export function isMissionWorkerRecord(value: unknown): value is MissionDiskObject {
  return isObject(value)
}
