import type { PendingPermissionRequest } from '@/state/appReducer'
import type { MissionState } from '@/state/missionState'
import type { ChatMessage, DroidPermissionOption, ToolCallBlock } from '@/types'

export const MISSION_RUNNING_INPUT_PLACEHOLDER = 'Mission is running. Pause to send a message.'

type MissionPermissionKind = 'propose_mission' | 'start_mission_run'

export interface MissionPermissionCardPresentation {
  badgeLabel: string
  title: string
  description: string
  primaryActionLabel: string
  secondaryActionLabel: string
}

export interface MissionActionState {
  canPause: boolean
  canKillWorker: boolean
  workerSessionId?: string
}

export interface MissionInputSemantics {
  disabled: boolean
  placeholder?: string
}

export type MissionRuntimeKind =
  | 'idle'
  | 'running'
  | 'validation-pending'
  | 'pause-pending'
  | 'kill-pending'
  | 'paused-by-user'
  | 'paused-after-user-kill'
  | 'daemon-retrying'
  | 'daemon-failed'
  | 'ready-to-continue'
  | 'completed'

export interface MissionRuntimeStatus {
  kind: MissionRuntimeKind
  title: string
  description: string
  tone: 'default' | 'warning' | 'danger' | 'success'
}

function asTrimmedString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

function normalizeLower(value: unknown): string {
  return String(value || '')
    .trim()
    .toLowerCase()
}

function normalizeToolName(value: unknown): string {
  const raw = asTrimmedString(value) ?? ''
  if (!raw) return ''
  const parts = raw.split('.')
  return (parts[parts.length - 1] || raw).replace(/[^a-z0-9]/gi, '').toLowerCase()
}

function extractPermissionToolName(item: unknown): string {
  const raw = (item as any)?.toolUse || item
  if (!raw || typeof raw !== 'object') return ''
  return (
    normalizeToolName((raw as any).name) ||
    normalizeToolName((raw as any).toolName) ||
    normalizeToolName((raw as any).recipient_name)
  )
}

function getMissionPermissionKind(
  request?: Pick<PendingPermissionRequest, 'confirmationType' | 'toolUses'> | null,
): MissionPermissionKind | null {
  const confirmationType = normalizeLower(request?.confirmationType)
  if (confirmationType === 'propose_mission' || confirmationType === 'start_mission_run') {
    return confirmationType
  }

  for (const item of request?.toolUses || []) {
    const toolName = extractPermissionToolName(item)
    if (toolName === 'proposemission') return 'propose_mission'
    if (toolName === 'startmissionrun') return 'start_mission_run'
  }

  return null
}

function getDefaultPermissionLabel(opt: DroidPermissionOption): string {
  switch (opt) {
    case 'proceed_once':
      return 'Proceed once'
    case 'proceed_always':
      return 'Proceed always'
    case 'proceed_auto_run':
      return 'Auto-run'
    case 'proceed_auto_run_low':
      return 'Auto-run (Low)'
    case 'proceed_auto_run_medium':
      return 'Auto-run (Medium)'
    case 'proceed_auto_run_high':
      return 'Auto-run (High)'
    case 'proceed_edit':
      return 'Proceed edit'
    case 'cancel':
      return 'Cancel'
  }
}

export function getMissionPermissionCardPresentation(
  request?: Pick<PendingPermissionRequest, 'confirmationType' | 'toolUses'> | null,
): MissionPermissionCardPresentation | null {
  const kind = getMissionPermissionKind(request)
  if (kind === 'propose_mission') {
    return {
      badgeLabel: 'Mission permission',
      title: 'Mission proposal ready',
      description:
        'Review the proposed Mission plan before launching work for this orchestrator session.',
      primaryActionLabel: 'Accept Mission Proposal',
      secondaryActionLabel: 'Cancel',
    }
  }

  if (kind === 'start_mission_run') {
    return {
      badgeLabel: 'Mission permission',
      title: 'Mission run is ready to start',
      description:
        'Approve the run when you are ready for the orchestrator to launch worker execution.',
      primaryActionLabel: 'Start Mission Run',
      secondaryActionLabel: 'Cancel',
    }
  }

  return null
}

export function getMissionPermissionOptionLabel(
  request: Pick<PendingPermissionRequest, 'confirmationType' | 'toolUses'> | null | undefined,
  option: DroidPermissionOption,
): string {
  if (option === 'cancel') {
    return getMissionPermissionCardPresentation(request)?.secondaryActionLabel || 'Cancel'
  }
  return (
    getMissionPermissionCardPresentation(request)?.primaryActionLabel ||
    getDefaultPermissionLabel(option)
  )
}

export function getMissionActionState(mission?: MissionState | null): MissionActionState {
  const currentState = normalizeLower(mission?.currentState)
  const workerSessionId =
    currentState === 'running' ? asTrimmedString(mission?.liveWorkerSessionId) : undefined
  return {
    canPause: currentState === 'running',
    canKillWorker: currentState === 'running' && Boolean(workerSessionId),
    workerSessionId,
  }
}

export function getMissionInputSemantics(mission?: MissionState | null): MissionInputSemantics {
  const disabled = normalizeLower(mission?.currentState) === 'running'
  return {
    disabled,
    placeholder: disabled ? MISSION_RUNNING_INPUT_PLACEHOLDER : undefined,
  }
}

function parseStructuredText(value: string): unknown {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  try {
    return JSON.parse(trimmed)
  } catch {
    return trimmed
  }
}

function extractSignalTexts(value: unknown, depth = 0): string[] {
  if (depth > 5 || value == null) return []
  if (typeof value === 'string') {
    const trimmed = value.trim()
    return trimmed ? [trimmed] : []
  }
  if (Array.isArray(value)) return value.flatMap((entry) => extractSignalTexts(entry, depth + 1))
  if (typeof value !== 'object') return []

  const objectValue = value as Record<string, unknown>
  const priorityKeys = ['systemMessage', 'message', 'reason', 'detail', 'error', 'status']
  const prioritized = priorityKeys.flatMap((key) => extractSignalTexts(objectValue[key], depth + 1))
  if (prioritized.length > 0) return prioritized

  return Object.values(objectValue).flatMap((entry) => extractSignalTexts(entry, depth + 1))
}

function isToolCallBlock(block: unknown): block is ToolCallBlock {
  return Boolean(block) && typeof block === 'object' && (block as any).kind === 'tool_call'
}

function getLatestStartMissionRunTexts(messages: ChatMessage[] = []): string[] {
  for (let messageIndex = messages.length - 1; messageIndex >= 0; messageIndex -= 1) {
    const message = messages[messageIndex]
    for (let blockIndex = message.blocks.length - 1; blockIndex >= 0; blockIndex -= 1) {
      const block = message.blocks[blockIndex]
      if (!isToolCallBlock(block)) continue
      if (normalizeToolName(block.toolName) !== 'startmissionrun') continue
      const payloads = [block.result, block.progress]
        .map((entry) => (typeof entry === 'string' ? parseStructuredText(entry) : undefined))
        .filter((entry) => entry !== undefined)
      return payloads.flatMap((entry) => extractSignalTexts(entry))
    }
  }
  return []
}

function getSortedProgressEntries(mission?: MissionState | null): Array<Record<string, unknown>> {
  return (mission?.progressEntries || [])
    .map((entry, index) => {
      const timestamp = Date.parse(String(entry.timestamp || ''))
      return { ...(entry as Record<string, unknown>), __index: index, __timestamp: timestamp }
    })
    .sort((left, right) => {
      const leftTime = Number(left.__timestamp)
      const rightTime = Number(right.__timestamp)
      const leftValid = Number.isFinite(leftTime)
      const rightValid = Number.isFinite(rightTime)
      if (leftValid && rightValid) {
        return leftTime - rightTime || Number(left.__index) - Number(right.__index)
      }
      return Number(left.__index) - Number(right.__index)
    })
}

function isDaemonLikeText(value: string | undefined): boolean {
  const normalized = normalizeLower(value)
  if (!normalized) return false
  return /(factoryd|daemon|authentication|spawn|socket|transport|connection|connect)/.test(
    normalized,
  )
}

function mentionsRetry(value: string | undefined): boolean {
  return /\bretry/i.test(String(value || ''))
}

function getFailureContext(params: { mission?: MissionState | null; messages?: ChatMessage[] }): {
  latestFailureReason?: string
  latestSignalText?: string
  daemonLike: boolean
  retryMentioned: boolean
  userKilled: boolean
  userPaused: boolean
} {
  const progressEntries = getSortedProgressEntries(params.mission)
  const latestFailure = [...progressEntries]
    .reverse()
    .find((entry) => normalizeLower(entry.type) === 'worker_failed')
  const latestPause = [...progressEntries].reverse().find((entry) => {
    const type = normalizeLower(entry.type)
    return type === 'worker_paused' || type === 'mission_paused'
  })

  const latestFailureReason =
    asTrimmedString(latestFailure?.reason) ?? asTrimmedString(latestFailure?.message)
  const signalTexts = [
    ...extractSignalTexts(params.mission?.supplemental?.raw),
    ...getLatestStartMissionRunTexts(params.messages || []),
  ]
  const latestSignalText =
    signalTexts.find((entry) => isDaemonLikeText(entry) || mentionsRetry(entry)) || signalTexts[0]
  const failureLikeText = latestFailureReason || latestSignalText
  const userKilled = /killed by user/i.test(failureLikeText || '')
  const daemonLike = !userKilled && isDaemonLikeText(failureLikeText)
  const retryMentioned = mentionsRetry(failureLikeText)
  const userPaused =
    !daemonLike &&
    !userKilled &&
    Boolean(latestPause) &&
    normalizeLower(params.mission?.currentState) === 'paused'

  return {
    latestFailureReason,
    latestSignalText,
    daemonLike,
    retryMentioned,
    userKilled,
    userPaused,
  }
}

function hasPendingCompletionGate(mission?: MissionState | null): boolean {
  return normalizeLower(mission?.currentState) === 'completed' && !mission?.isCompleted
}

export function getMissionRuntimeStatus(params: {
  mission?: MissionState | null
  messages?: ChatMessage[]
  pendingAction?: 'pause' | 'kill' | null
}): MissionRuntimeStatus {
  const { mission, messages = [], pendingAction = null } = params
  const currentState = normalizeLower(mission?.currentState)
  const failureContext = getFailureContext({ mission, messages })
  const liveWorkerSessionId = asTrimmedString(mission?.liveWorkerSessionId)

  if (!mission) {
    return {
      kind: 'idle',
      title: 'Mission status unavailable',
      description: 'Mission details will appear here when the orchestrator reports them.',
      tone: 'default',
    }
  }

  if (pendingAction === 'kill' && currentState === 'running') {
    return {
      kind: 'kill-pending',
      title: 'Kill request sent',
      description:
        'Waiting for the runner to stop the active worker and return the Mission to a paused state.',
      tone: 'warning',
    }
  }

  if (pendingAction === 'pause' && currentState === 'running') {
    return {
      kind: 'pause-pending',
      title: 'Pause request sent',
      description:
        'Waiting for the Mission to acknowledge the pause before normal chat input is re-enabled.',
      tone: 'warning',
    }
  }

  if (hasPendingCompletionGate(mission)) {
    return {
      kind: 'validation-pending',
      title: 'Validation pending',
      description:
        'Mission work has finished, but this run is not complete until validation settles.',
      tone: 'warning',
    }
  }

  if (currentState === 'completed') {
    return {
      kind: 'completed',
      title: 'Mission completed',
      description: 'All currently tracked Mission work has finished.',
      tone: 'success',
    }
  }

  if (currentState === 'running') {
    if (!liveWorkerSessionId && failureContext.daemonLike && failureContext.retryMentioned) {
      return {
        kind: 'daemon-retrying',
        title: 'Retrying Mission run after daemon failure',
        description:
          failureContext.latestSignalText ||
          'The runner hit a daemon/factoryd issue and is retrying once before returning control.',
        tone: 'warning',
      }
    }

    return {
      kind: 'running',
      title: 'Mission is running',
      description:
        'Pause the Mission to send a normal chat message while the orchestrator and worker continue their current run.',
      tone: 'default',
    }
  }

  if (failureContext.userKilled) {
    return {
      kind: 'paused-after-user-kill',
      title: 'Worker killed by user',
      description:
        'The active worker was terminated at your request. Inspect the Mission state, then continue via normal chat when ready.',
      tone: 'warning',
    }
  }

  if (failureContext.daemonLike) {
    return {
      kind: 'daemon-failed',
      title: failureContext.retryMentioned
        ? 'Daemon retry exhausted'
        : 'Mission paused after daemon failure',
      description: [
        failureContext.latestFailureReason || failureContext.latestSignalText,
        'Continue via normal chat after the daemon/factoryd issue is resolved, or restart the daemon if needed.',
      ]
        .filter(Boolean)
        .join(' '),
      tone: 'danger',
    }
  }

  if (failureContext.userPaused) {
    return {
      kind: 'paused-by-user',
      title: 'Mission paused by user',
      description:
        'You paused the Mission. Continue via normal chat when ready; the orchestrator will decide whether to call Start Mission Run again.',
      tone: 'warning',
    }
  }

  if (currentState === 'paused' || currentState === 'orchestrator_turn') {
    return {
      kind: 'ready-to-continue',
      title: 'Mission is waiting for guidance',
      description:
        'Continue via normal chat input. The orchestrator decides whether to call Start Mission Run again.',
      tone: 'default',
    }
  }

  return {
    kind: 'idle',
    title: 'Mission state unavailable',
    description: 'Mission details will appear here when the orchestrator reports them.',
    tone: 'default',
  }
}
