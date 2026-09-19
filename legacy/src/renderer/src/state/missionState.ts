import type {
  MissionDirSnapshot,
  MissionDiskHandoff,
  MissionDiskObject,
  MissionLoadSnapshot,
} from '../../../shared/mission.ts'

export type MissionStateSource = 'load_session' | 'notification' | 'disk'

export interface MissionSupplementalState {
  missionState?: string
  currentFeatureId?: string
  currentWorkerSessionId?: string
  completedFeatures?: number
  totalFeatures?: number
  features?: MissionDiskObject[]
  raw?: MissionDiskObject | null
}

export interface MissionState {
  state: MissionDiskObject | null
  features: MissionDiskObject[]
  progressEntries: MissionDiskObject[]
  handoffs: MissionDiskHandoff[]
  validationState: MissionDiskObject | null
  currentState?: string
  currentFeatureId?: string
  currentWorkerSessionId?: string
  liveWorkerSessionId?: string
  pausedWorkerSessionId?: string
  completedFeatures: number
  totalFeatures: number
  isCompleted: boolean
  lastSource?: MissionStateSource
  supplemental?: MissionSupplementalState | null
}

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function hasOwnKey(value: unknown, key: string): boolean {
  return isObject(value) && Object.prototype.hasOwnProperty.call(value, key)
}

function stableStringify(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map((entry) => stableStringify(entry)).join(',')}]`
  if (isObject(value)) {
    return `{${Object.entries(value)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, entry]) => `${JSON.stringify(key)}:${stableStringify(entry)}`)
      .join(',')}}`
  }
  return JSON.stringify(value)
}

function asTrimmedString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

function asNumber(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

function normalizeDiskObject(value: unknown): MissionDiskObject | null {
  return isObject(value) ? ({ ...(value as MissionDiskObject) } as MissionDiskObject) : null
}

function normalizeDiskObjectArray(value: unknown): MissionDiskObject[] {
  if (!Array.isArray(value)) return []
  return value.map((entry) => normalizeDiskObject(entry)).filter(Boolean) as MissionDiskObject[]
}

function normalizeHandoffs(value: unknown): MissionDiskHandoff[] {
  if (!value) return []
  if (Array.isArray(value)) {
    return value
      .map((entry, index) => {
        if (
          isObject(entry) &&
          typeof (entry as any).fileName === 'string' &&
          isObject((entry as any).payload)
        ) {
          return {
            fileName: String((entry as any).fileName),
            payload: { ...((entry as any).payload as MissionDiskObject) },
          }
        }
        const payload = normalizeDiskObject(entry)
        if (!payload) return null
        const featureId = asTrimmedString((payload as any).featureId)
        return {
          fileName: featureId ? `${featureId}.json` : `handoff-${index + 1}.json`,
          payload,
        }
      })
      .filter(Boolean) as MissionDiskHandoff[]
  }

  if (!isObject(value)) return []

  return Object.entries(value)
    .map(([fileName, payload]) => {
      const normalized = normalizeDiskObject(payload)
      if (!normalized) return null
      return { fileName, payload: normalized }
    })
    .filter(Boolean) as MissionDiskHandoff[]
}

function normalizeFileName(value: unknown): string | undefined {
  const raw = asTrimmedString(value)
  if (!raw) return undefined
  const normalized = raw.split(/[\\/]/).pop()?.trim()
  return normalized || undefined
}

function readIsoTimestamp(value: unknown): number | undefined {
  const raw = asTrimmedString(value)
  if (!raw) return undefined
  const parsed = Date.parse(raw)
  return Number.isFinite(parsed) ? parsed : undefined
}

function readMissionObjectTimestamp(value: MissionDiskObject | null): number | undefined {
  if (!value) return undefined
  return (
    readIsoTimestamp((value as any).updatedAt) ??
    readIsoTimestamp((value as any).timestamp) ??
    readIsoTimestamp((value as any).createdAt)
  )
}

function readMissionStateValue(state: MissionDiskObject | null): string | undefined {
  return asTrimmedString((state as any)?.state) ?? asTrimmedString((state as any)?.missionState)
}

function normalizeMissionStatePatch(value: unknown): MissionDiskObject | null {
  if (!isObject(value)) return null

  const patch: MissionDiskObject = {}
  const state =
    asTrimmedString((value as any).missionState) ??
    asTrimmedString((value as any).currentState) ??
    asTrimmedString((value as any).state)
  if (state) patch.state = state

  const currentFeatureId = asTrimmedString((value as any).currentFeatureId)
  if (currentFeatureId) patch.currentFeatureId = currentFeatureId

  const currentWorkerSessionId = asTrimmedString((value as any).currentWorkerSessionId)
  if (currentWorkerSessionId) patch.currentWorkerSessionId = currentWorkerSessionId

  const completedFeatures = asNumber((value as any).completedFeatures)
  if (completedFeatures !== undefined) patch.completedFeatures = completedFeatures

  const totalFeatures = asNumber((value as any).totalFeatures)
  if (totalFeatures !== undefined) patch.totalFeatures = totalFeatures

  const updatedAt = asTrimmedString((value as any).updatedAt)
  if (updatedAt) patch.updatedAt = updatedAt

  const createdAt = asTrimmedString((value as any).createdAt)
  if (createdAt) patch.createdAt = createdAt

  const timestamp = asTrimmedString((value as any).timestamp)
  if (timestamp) patch.timestamp = timestamp

  const milestonesWithValidationPlanned = (value as any).milestonesWithValidationPlanned
  if (
    Array.isArray(milestonesWithValidationPlanned) &&
    milestonesWithValidationPlanned.length > 0
  ) {
    patch.milestonesWithValidationPlanned = milestonesWithValidationPlanned
  }

  return Object.keys(patch).length > 0 ? patch : null
}

function readCurrentFeatureId(
  state: MissionDiskObject | null,
  features: MissionDiskObject[],
): string | undefined {
  const fromState = asTrimmedString((state as any)?.currentFeatureId)
  if (fromState) return fromState
  const liveFeature = features.find((feature) => {
    const status = featureStatus(feature)
    return status === 'in_progress' || status === 'running'
  })
  return asTrimmedString((liveFeature as any)?.id)
}

function readCurrentWorkerSessionId(state: MissionDiskObject | null): string | undefined {
  return asTrimmedString((state as any)?.currentWorkerSessionId)
}

function featureStatus(feature: MissionDiskObject | null | undefined): string | undefined {
  const status =
    asTrimmedString((feature as any)?.status) ?? asTrimmedString((feature as any)?.state)
  return status ? status.toLowerCase() : undefined
}

function isCompletedFeature(feature: MissionDiskObject): boolean {
  const status = featureStatus(feature)
  return status === 'completed' || status === 'done' || (feature as any).completed === true
}

function countCompletedFeatures(features: MissionDiskObject[]): number {
  return features.reduce((count, feature) => count + (isCompletedFeature(feature) ? 1 : 0), 0)
}

function validationFlowPlanned(
  state: MissionDiskObject | null,
  features: MissionDiskObject[],
  validationState: MissionDiskObject | null,
): boolean {
  if (validationState) return true
  const milestones = (state as any)?.milestonesWithValidationPlanned
  if (Array.isArray(milestones) && milestones.length > 0) return true
  return features.some((feature) => {
    const skillName = asTrimmedString((feature as any).skillName) ?? ''
    return skillName === 'scrutiny-validator' || skillName === 'user-testing-validator'
  })
}

function validationSettled(validationState: MissionDiskObject | null): boolean {
  if (!validationState) return false
  const status = asTrimmedString((validationState as any).status)
  if (status) {
    const normalized = status.toLowerCase()
    if (normalized === 'passed' || normalized === 'failed' || normalized === 'completed')
      return true
  }

  const assertions = (validationState as any).assertions
  if (!isObject(assertions)) return false
  const statuses = Object.values(assertions)
    .map((entry) => (isObject(entry) ? asTrimmedString((entry as any).status) : undefined))
    .filter(Boolean) as string[]
  return statuses.length > 0 && statuses.every((entry) => entry.toLowerCase() !== 'pending')
}

function countNonEmptyFields(value: MissionDiskObject | null): number {
  if (!value) return 0
  return Object.entries(value).reduce((count, [, entry]) => {
    if (entry === null || entry === undefined) return count
    if (typeof entry === 'string' && !entry.trim()) return count
    if (Array.isArray(entry) && entry.length === 0) return count
    if (isObject(entry) && Object.keys(entry).length === 0) return count
    return count + 1
  }, 0)
}

function mergeStateSnapshots(
  currentState: MissionDiskObject | null,
  incomingState: MissionDiskObject | null,
): MissionDiskObject | null {
  if (!incomingState) return currentState
  if (!currentState) return incomingState

  const currentTimestamp = readMissionObjectTimestamp(currentState)
  const incomingTimestamp = readMissionObjectTimestamp(incomingState)
  if (incomingTimestamp !== undefined && currentTimestamp !== undefined) {
    return incomingTimestamp >= currentTimestamp
      ? { ...currentState, ...incomingState }
      : { ...incomingState, ...currentState }
  }
  if (incomingTimestamp !== undefined) return { ...currentState, ...incomingState }
  if (currentTimestamp !== undefined) return { ...incomingState, ...currentState }

  const incomingFields = countNonEmptyFields(incomingState)
  const currentFields = countNonEmptyFields(currentState)
  return incomingFields >= currentFields
    ? { ...currentState, ...incomingState }
    : { ...incomingState, ...currentState }
}

function mergeValidationState(
  currentValue: MissionDiskObject | null,
  incomingValue: MissionDiskObject | null,
): MissionDiskObject | null {
  if (!incomingValue) return currentValue
  if (!currentValue) return incomingValue
  const currentSettled = validationSettled(currentValue)
  const incomingSettled = validationSettled(incomingValue)
  if (incomingSettled && !currentSettled) return incomingValue
  if (currentSettled && !incomingSettled) return currentValue
  return mergeStateSnapshots(currentValue, incomingValue)
}

function mergeProgressEntries(
  currentEntries: MissionDiskObject[],
  incomingEntries: MissionDiskObject[],
): MissionDiskObject[] {
  if (incomingEntries.length === 0) return currentEntries
  const next = [...currentEntries]
  const seen = new Set(next.map((entry) => stableStringify(entry)))
  for (const entry of incomingEntries) {
    const fingerprint = stableStringify(entry)
    if (seen.has(fingerprint)) continue
    seen.add(fingerprint)
    next.push(entry)
  }
  return next
}

function mergeHandoffs(
  currentHandoffs: MissionDiskHandoff[],
  incomingHandoffs: MissionDiskHandoff[],
): MissionDiskHandoff[] {
  if (incomingHandoffs.length === 0) return currentHandoffs
  const next = new Map(currentHandoffs.map((handoff) => [handoff.fileName, handoff]))
  for (const handoff of incomingHandoffs) next.set(handoff.fileName, handoff)
  return Array.from(next.values())
}

function normalizeLiveWorkerSessionId(
  current: MissionState | undefined,
  nextCurrentWorkerSessionId: string | undefined,
  nextState: string | undefined,
): string | undefined {
  if (nextState !== 'running') return undefined
  const liveWorker = asTrimmedString(current?.liveWorkerSessionId)
  if (!liveWorker || !nextCurrentWorkerSessionId) return undefined
  return liveWorker === nextCurrentWorkerSessionId ? liveWorker : undefined
}

function inferPausedWorkerSessionId(params: {
  state: MissionDiskObject | null
  progressEntries: MissionDiskObject[]
  currentState?: string
  currentWorkerSessionId?: string
  liveWorkerSessionId?: string
  existingPausedWorkerSessionId?: string
}): string | undefined {
  const currentState = asTrimmedString(params.currentState)?.toLowerCase()
  if (currentState !== 'paused') return undefined

  const explicit =
    asTrimmedString((params.state as any)?.pausedWorkerSessionId) ??
    asTrimmedString((params.state as any)?.interruptedWorkerSessionId)
  if (explicit) return explicit

  const carried =
    asTrimmedString(params.liveWorkerSessionId) ?? asTrimmedString(params.currentWorkerSessionId)
  if (carried) return carried

  const latestWorkerEntry = [...params.progressEntries].reverse().find((entry) => {
    return Boolean(asTrimmedString((entry as any)?.workerSessionId))
  })
  const latestWorkerSessionId = asTrimmedString((latestWorkerEntry as any)?.workerSessionId)
  if (!latestWorkerSessionId) return asTrimmedString(params.existingPausedWorkerSessionId)

  const latestWorkerTimestamp = readIsoTimestamp((latestWorkerEntry as any)?.timestamp)
  const latestCompletionEntry = [...params.progressEntries].reverse().find((entry) => {
    return asTrimmedString((entry as any)?.type)?.toLowerCase() === 'worker_completed'
  })
  const latestCompletionTimestamp = readIsoTimestamp((latestCompletionEntry as any)?.timestamp)
  if (
    latestWorkerTimestamp !== undefined &&
    latestCompletionTimestamp !== undefined &&
    latestCompletionTimestamp >= latestWorkerTimestamp
  ) {
    return undefined
  }

  return latestWorkerSessionId
}

function getLatestProgressEntry(
  progressEntries: MissionDiskObject[],
): MissionDiskObject | undefined {
  return [...progressEntries].sort((left, right) => {
    const leftTime = readIsoTimestamp((left as any).timestamp) ?? Number.NEGATIVE_INFINITY
    const rightTime = readIsoTimestamp((right as any).timestamp) ?? Number.NEGATIVE_INFINITY
    return rightTime - leftTime
  })[0]
}

function inferMissionStateFromProgress(params: {
  progressEntries: MissionDiskObject[]
  completedFeatures: number
  totalFeatures: number
}): string | undefined {
  const latestProgressEntry = getLatestProgressEntry(params.progressEntries)
  const latestType = asTrimmedString((latestProgressEntry as any)?.type)?.toLowerCase()
  if (!latestType) return undefined

  if (
    latestType === 'mission_run_started' ||
    latestType === 'mission_resumed' ||
    latestType === 'worker_started' ||
    latestType === 'worker_selected_feature'
  ) {
    return 'running'
  }

  if (latestType === 'mission_paused' || latestType === 'worker_paused') return 'paused'
  if (latestType === 'mission_completed') return 'completed'

  if (
    latestType === 'worker_completed' ||
    latestType === 'worker_failed' ||
    latestType === 'handoff_items_dismissed'
  ) {
    const featuresSettled =
      params.totalFeatures > 0 && params.completedFeatures >= params.totalFeatures
    return featuresSettled ? 'completed' : 'orchestrator_turn'
  }

  return undefined
}

function inferMissionState(params: {
  state: MissionDiskObject | null
  features: MissionDiskObject[]
  progressEntries: MissionDiskObject[]
  validationState: MissionDiskObject | null
  currentWorkerSessionId?: string
  completedFeatures: number
  totalFeatures: number
}): string | undefined {
  const explicit = readMissionStateValue(params.state)
  const explicitTimestamp = readMissionObjectTimestamp(params.state)
  const latestProgressEntry = getLatestProgressEntry(params.progressEntries)
  const latestProgressTimestamp = readIsoTimestamp((latestProgressEntry as any)?.timestamp)
  const progressDerivedState = inferMissionStateFromProgress(params)

  if (
    progressDerivedState &&
    explicitTimestamp !== undefined &&
    latestProgressTimestamp !== undefined &&
    latestProgressTimestamp > explicitTimestamp
  ) {
    return progressDerivedState
  }
  if (explicit) return explicit

  if (params.currentWorkerSessionId) return 'running'
  if (
    params.features.some((feature) => {
      const status = featureStatus(feature)
      return status === 'in_progress' || status === 'running'
    })
  ) {
    return 'running'
  }

  const featuresSettled =
    params.totalFeatures > 0 && params.completedFeatures >= params.totalFeatures
  if (featuresSettled) {
    return 'completed'
  }

  if (progressDerivedState) return progressDerivedState

  return undefined
}

function extractNotificationHandoffs(notification: Record<string, unknown>): MissionDiskHandoff[] {
  const explicit = normalizeHandoffs((notification as any).handoffs)
  if (explicit.length > 0) return explicit

  const nestedHandoff = normalizeDiskObject((notification as any).handoff)
  if (!nestedHandoff) return []

  const featureId =
    asTrimmedString((notification as any).featureId) ??
    asTrimmedString((nestedHandoff as any).featureId)
  const successState = asTrimmedString((notification as any).successState)
  const payload: MissionDiskObject = {
    ...(featureId ? { featureId } : {}),
    ...(successState ? { successState } : {}),
    handoff: nestedHandoff,
  }
  const fileName =
    normalizeFileName((notification as any).handoffFileName) ??
    normalizeFileName((notification as any).handoffFile) ??
    normalizeFileName((notification as any).handoffPath) ??
    `${featureId || 'handoff'}.json`

  return [{ fileName, payload }]
}

function finalizeMissionState(input: MissionState): MissionState {
  const completedFromState = asNumber((input.state as any)?.completedFeatures) ?? 0
  const completedFromSupplemental = input.supplemental?.completedFeatures ?? 0
  const completedFromFeatures = countCompletedFeatures(input.features)
  const completedFeatures = Math.max(
    completedFromState,
    completedFromSupplemental,
    completedFromFeatures,
    input.completedFeatures,
  )

  const totalFeatures =
    input.features.length > 0
      ? input.features.length
      : Math.max(
          asNumber((input.state as any)?.totalFeatures) ?? 0,
          input.supplemental?.totalFeatures ?? 0,
          input.totalFeatures ?? 0,
        )

  const currentWorkerFromState = readCurrentWorkerSessionId(input.state)
  let currentWorkerSessionId = currentWorkerFromState
  if (!currentWorkerSessionId && !hasOwnKey(input.state, 'currentWorkerSessionId')) {
    currentWorkerSessionId =
      input.supplemental?.currentWorkerSessionId ?? input.currentWorkerSessionId
  }
  const currentState =
    inferMissionState({
      state: input.state,
      features: input.features,
      progressEntries: input.progressEntries,
      validationState: input.validationState,
      currentWorkerSessionId,
      completedFeatures,
      totalFeatures,
    }) ?? input.supplemental?.missionState
  let liveWorkerSessionId = normalizeLiveWorkerSessionId(
    input,
    currentWorkerSessionId,
    currentState,
  )
  const pausedWorkerSessionId = inferPausedWorkerSessionId({
    state: input.state,
    progressEntries: input.progressEntries,
    currentState,
    currentWorkerSessionId,
    liveWorkerSessionId,
    existingPausedWorkerSessionId: input.pausedWorkerSessionId,
  })

  if (currentState && currentState !== 'running') {
    liveWorkerSessionId = undefined
    if (
      currentState === 'completed' ||
      currentState === 'paused' ||
      currentState === 'orchestrator_turn'
    ) {
      currentWorkerSessionId = undefined
    }
  }

  const currentFeatureFromState = readCurrentFeatureId(input.state, input.features)
  const currentFeatureId =
    currentFeatureFromState || hasOwnKey(input.state, 'currentFeatureId')
      ? currentFeatureFromState
      : (input.supplemental?.currentFeatureId ?? input.currentFeatureId)
  const validationPlanned = validationFlowPlanned(
    input.state,
    input.features,
    input.validationState,
  )
  const featuresSettled = totalFeatures === 0 || completedFeatures >= totalFeatures
  const isCompleted =
    currentState === 'completed' &&
    featuresSettled &&
    (!validationPlanned || validationSettled(input.validationState))

  return {
    ...input,
    currentState,
    currentFeatureId,
    currentWorkerSessionId,
    liveWorkerSessionId,
    pausedWorkerSessionId,
    completedFeatures,
    totalFeatures,
    isCompleted,
  }
}

export function createEmptyMissionState(): MissionState {
  return finalizeMissionState({
    state: null,
    features: [],
    progressEntries: [],
    handoffs: [],
    validationState: null,
    pausedWorkerSessionId: undefined,
    completedFeatures: 0,
    totalFeatures: 0,
    isCompleted: false,
    supplemental: null,
  })
}

function normalizeMissionLoadSnapshot(snapshot: MissionLoadSnapshot) {
  const features = normalizeDiskObjectArray(snapshot.features)
  const progressEntries = normalizeDiskObjectArray(snapshot.progressEntries ?? snapshot.progressLog)
  const handoffs = normalizeHandoffs(snapshot.handoffs)
  return {
    state: mergeStateSnapshots(
      normalizeDiskObject(snapshot.state),
      normalizeMissionStatePatch(snapshot),
    ),
    features,
    progressEntries,
    handoffs,
    validationState: normalizeDiskObject(snapshot.validationState),
  }
}

export function applyMissionLoadSnapshot(
  current: MissionState | undefined,
  snapshot: MissionLoadSnapshot | null | undefined,
): MissionState | undefined {
  if (!snapshot) return current
  const normalized = normalizeMissionLoadSnapshot(snapshot)
  const base = current ?? createEmptyMissionState()
  return finalizeMissionState({
    ...base,
    state: mergeStateSnapshots(base.state, normalized.state),
    features: normalized.features.length > 0 ? normalized.features : base.features,
    progressEntries: mergeProgressEntries(base.progressEntries, normalized.progressEntries),
    handoffs: mergeHandoffs(base.handoffs, normalized.handoffs),
    validationState: mergeValidationState(base.validationState, normalized.validationState),
    lastSource: 'load_session',
  })
}

export function applyMissionDirSnapshot(
  current: MissionState | undefined,
  snapshot: MissionDirSnapshot,
): MissionState {
  const base = current ?? createEmptyMissionState()
  return finalizeMissionState({
    ...base,
    state: mergeStateSnapshots(base.state, normalizeDiskObject(snapshot.state)),
    features:
      snapshot.features === null ? base.features : normalizeDiskObjectArray(snapshot.features),
    progressEntries: mergeProgressEntries(
      base.progressEntries,
      normalizeDiskObjectArray(snapshot.progressEntries),
    ),
    handoffs: mergeHandoffs(base.handoffs, normalizeHandoffs(snapshot.handoffs)),
    validationState: mergeValidationState(
      base.validationState,
      normalizeDiskObject(snapshot.validationState),
    ),
    lastSource: 'disk',
  })
}

function normalizeMissionProgressEntries(value: unknown): MissionDiskObject[] {
  if (Array.isArray(value)) return normalizeDiskObjectArray(value)
  const normalized = normalizeDiskObject(value)
  return normalized ? [normalized] : []
}

function patchState(
  currentState: MissionDiskObject | null,
  patch: MissionDiskObject,
): MissionDiskObject {
  return { ...(currentState || {}), ...patch }
}

export function applyMissionNotificationUpdate(
  current: MissionState | undefined,
  notification: Record<string, unknown>,
): MissionState {
  const type = asTrimmedString((notification as any).type) ?? ''
  const base = current ?? createEmptyMissionState()

  if (type === 'mission_state_changed') {
    const nextState =
      normalizeDiskObject((notification as any).state) ||
      patchState(base.state, {
        ...(isObject((notification as any).payload)
          ? ((notification as any).payload as MissionDiskObject)
          : {}),
        ...(asTrimmedString((notification as any).missionState)
          ? { state: String((notification as any).missionState).trim() }
          : {}),
        ...(asTrimmedString((notification as any).newState)
          ? { state: String((notification as any).newState).trim() }
          : {}),
        ...(asTrimmedString((notification as any).state)
          ? { state: String((notification as any).state).trim() }
          : {}),
      })
    return finalizeMissionState({ ...base, state: nextState, lastSource: 'notification' })
  }

  if (type === 'mission_features_changed') {
    const nextFeatures = normalizeDiskObjectArray((notification as any).features)
    return finalizeMissionState({
      ...base,
      features: nextFeatures,
      lastSource: 'notification',
    })
  }

  if (type === 'mission_progress_entry') {
    const entries = normalizeMissionProgressEntries(
      Array.isArray((notification as any).entries)
        ? (notification as any).entries
        : (notification as any).entry,
    )
    return finalizeMissionState({
      ...base,
      progressEntries: mergeProgressEntries(base.progressEntries, entries),
      lastSource: 'notification',
    })
  }

  if (type === 'mission_worker_started') {
    const workerSessionId = asTrimmedString((notification as any).workerSessionId)
    const featureId = asTrimmedString((notification as any).featureId)
    const nextState = patchState(base.state, {
      ...(workerSessionId ? { currentWorkerSessionId: workerSessionId } : {}),
      ...(featureId ? { currentFeatureId: featureId } : {}),
      ...(asTrimmedString((notification as any).missionState)
        ? { state: String((notification as any).missionState).trim() }
        : {}),
      ...(asTrimmedString((notification as any).newState)
        ? { state: String((notification as any).newState).trim() }
        : {}),
    })
    return finalizeMissionState({
      ...base,
      state: nextState,
      liveWorkerSessionId: workerSessionId,
      lastSource: 'notification',
    })
  }

  if (type === 'mission_worker_completed') {
    const completed = base.completedFeatures + 1
    const handoffs = extractNotificationHandoffs(notification)
    const nextState = patchState(base.state, {
      currentWorkerSessionId: undefined,
      ...(completed ? { completedFeatures: completed } : {}),
      ...(asTrimmedString((notification as any).featureId)
        ? { currentFeatureId: String((notification as any).featureId).trim() }
        : {}),
      ...(asTrimmedString((notification as any).missionState)
        ? { state: String((notification as any).missionState).trim() }
        : {}),
      ...(asTrimmedString((notification as any).newState)
        ? { state: String((notification as any).newState).trim() }
        : {}),
    })
    return finalizeMissionState({
      ...base,
      state: nextState,
      handoffs: mergeHandoffs(base.handoffs, handoffs),
      liveWorkerSessionId: undefined,
      lastSource: 'notification',
    })
  }

  return base
}

function extractSupplementalMissionFields(value: unknown): MissionSupplementalState | null {
  if (!isObject(value)) return null

  const direct: MissionSupplementalState = {
    missionState:
      asTrimmedString((value as any).missionState) ?? asTrimmedString((value as any).state),
    currentFeatureId: asTrimmedString((value as any).currentFeatureId),
    currentWorkerSessionId: asTrimmedString((value as any).currentWorkerSessionId),
    completedFeatures: asNumber((value as any).completedFeatures),
    totalFeatures: asNumber((value as any).totalFeatures),
    features: Array.isArray((value as any).features)
      ? normalizeDiskObjectArray((value as any).features)
      : undefined,
    raw: normalizeDiskObject(value),
  }

  const hasDirect =
    Boolean(direct.missionState || direct.currentFeatureId || direct.currentWorkerSessionId) ||
    direct.completedFeatures !== undefined ||
    direct.totalFeatures !== undefined ||
    (direct.features?.length || 0) > 0
  if (hasDirect) return direct

  const nestedKeys = ['status', 'progress', 'payload', 'data', 'result', 'mission']
  for (const key of nestedKeys) {
    const nested = extractSupplementalMissionFields((value as any)[key])
    if (nested) return nested
  }
  return null
}

export function applyStartMissionProgressUpdate(
  current: MissionState | undefined,
  update: unknown,
): MissionState {
  const supplemental = extractSupplementalMissionFields(update)
  const base = current ?? createEmptyMissionState()
  if (!supplemental) return base
  return finalizeMissionState({
    ...base,
    supplemental: {
      ...(base.supplemental || {}),
      ...supplemental,
    },
  })
}
