#!/usr/bin/env node

const { randomUUID } = require('node:crypto')
const { appendFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } = require('node:fs')
const { tmpdir } = require('node:os')
const { join } = require('node:path')
const { createInterface } = require('node:readline')

const JSONRPC_VERSION = '2.0'
const FACTORY_API_VERSION = '1.0.0'
const FEATURE_ID = 'mission-cross-flow-kill-worker-repro-harness'
const FEATURE_TITLE = 'Deterministic kill worker repro harness'
const PROPOSE_TOOL_ID = 'mission-harness-propose'
const START_TOOL_ID = 'mission-harness-start-run'

if (process.argv.includes('--version')) {
  process.stdout.write('droid mission-kill-worker-harness\n')
  process.exit(0)
}

let engineSessionId = ''
let missionDir = ''
let missionState = 'idle'
let requestSeq = 0
let progressOffsetMs = 0
let currentPermission = null
let progressEntries = []
let features = []
let liveWorkerSessionId = ''
let currentFeatureId = FEATURE_ID
let killScheduled = false

function send(message) {
  process.stdout.write(JSON.stringify(message) + '\n')
}

function sendResponse(id, result, error) {
  send({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'response',
    id,
    ...(error ? { error } : { result }),
  })
}

function sendSessionNotification(notification) {
  send({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'notification',
    method: 'droid.session_notification',
    params: { notification },
  })
}

function sendPermissionRequest(params) {
  const id = `mission-harness-request-${++requestSeq}`
  send({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'request',
    id,
    method: 'droid.request_permission',
    params,
  })
  return id
}

function getTimestamp() {
  const timestamp = new Date(Date.now() + progressOffsetMs).toISOString()
  progressOffsetMs += 1000
  return timestamp
}

function getMissionRoot() {
  return join(
    process.env.DROID_APP_DATA_DIR || tmpdir(),
    'mission-kill-worker-harness',
    engineSessionId || 'pending-session',
  )
}

function ensureMissionDir() {
  if (!missionDir) missionDir = getMissionRoot()
  mkdirSync(missionDir, { recursive: true })
  return missionDir
}

function buildStateSnapshot() {
  const currentWorkerSessionId = missionState === 'running' ? liveWorkerSessionId || undefined : undefined
  return {
    baseSessionId: engineSessionId || undefined,
    state: missionState,
    currentFeatureId,
    currentWorkerSessionId,
    completedFeatures: 0,
    totalFeatures: 1,
    updatedAt: new Date().toISOString(),
  }
}

function restoreMissionSnapshotIfPresent() {
  const dir = getMissionRoot()
  if (!existsSync(dir)) return

  missionDir = dir

  const statePath = join(dir, 'state.json')
  if (existsSync(statePath)) {
    try {
      const state = JSON.parse(readFileSync(statePath, 'utf8'))
      if (typeof state?.state === 'string' && state.state.trim()) {
        missionState = state.state.trim()
      }
      if (typeof state?.currentFeatureId === 'string' && state.currentFeatureId.trim()) {
        currentFeatureId = state.currentFeatureId.trim()
      }
      if (typeof state?.currentWorkerSessionId === 'string' && state.currentWorkerSessionId.trim()) {
        liveWorkerSessionId = state.currentWorkerSessionId.trim()
      }
    } catch {}
  }

  const featuresPath = join(dir, 'features.json')
  if (existsSync(featuresPath)) {
    try {
      const parsed = JSON.parse(readFileSync(featuresPath, 'utf8'))
      if (Array.isArray(parsed)) features = parsed
    } catch {}
  }
}

function writeMissionSnapshot() {
  const dir = ensureMissionDir()
  writeFileSync(join(dir, 'state.json'), JSON.stringify(buildStateSnapshot(), null, 2))
  writeFileSync(join(dir, 'features.json'), JSON.stringify(features, null, 2))
}

function appendProgressEntry(entry) {
  const normalized = {
    timestamp: getTimestamp(),
    ...entry,
  }
  progressEntries.push(normalized)
  appendFileSync(join(ensureMissionDir(), 'progress_log.jsonl'), JSON.stringify(normalized) + '\n')
  sendSessionNotification({ type: 'mission_progress_entry', entry: normalized })
}

function setMissionState(nextState) {
  missionState = nextState
  writeMissionSnapshot()
  sendSessionNotification({
    type: 'mission_state_changed',
    state: buildStateSnapshot(),
  })
}

function sendMissionFeatures(status) {
  features = [{ id: FEATURE_ID, title: FEATURE_TITLE, status }]
  writeMissionSnapshot()
  sendSessionNotification({ type: 'mission_features_changed', features })
}

function resolvePermission(requestId, toolUseId, selectedOption) {
  sendSessionNotification({
    type: 'permission_resolved',
    requestId,
    toolUseIds: [toolUseId],
    selectedOption,
  })
}

function notifyWorkingState(newState) {
  sendSessionNotification({ type: 'droid_working_state_changed', newState })
}

function handleMissionPrompt() {
  notifyWorkingState('executing_tool')
  sendSessionNotification({
    type: 'tool_use',
    id: PROPOSE_TOOL_ID,
    name: 'ProposeMission',
    input: { task: 'Drive a deterministic live kill-worker repro flow on one Mission session.' },
  })
  const requestId = sendPermissionRequest({
    confirmationType: 'propose_mission',
    toolUses: [{ toolUse: { id: PROPOSE_TOOL_ID, name: 'ProposeMission' } }],
    options: ['proceed_once', 'cancel'],
  })
  currentPermission = { requestId, toolUseId: PROPOSE_TOOL_ID, kind: 'propose' }
}

function handleProposalPermission(message) {
  const selectedOption = String(message.result?.selectedOption || 'cancel')
  resolvePermission(currentPermission.requestId, PROPOSE_TOOL_ID, selectedOption)
  currentPermission = null

  if (selectedOption === 'cancel') {
    sendSessionNotification({
      type: 'tool_result',
      toolUseId: PROPOSE_TOOL_ID,
      isError: true,
      content: { message: 'Mission proposal cancelled by user.' },
    })
    notifyWorkingState('idle')
    return
  }

  sendMissionFeatures('pending')
  appendProgressEntry({ type: 'mission_accepted', message: 'Mission accepted' })
  sendSessionNotification({
    type: 'tool_result',
    toolUseId: PROPOSE_TOOL_ID,
    content: {
      missionDir: ensureMissionDir(),
      summary:
        'Validation harness will start one worker, wait for a real Kill Worker action, and leave the same Mission paused for inspection or normal-chat continuation.',
    },
  })
  setMissionState('orchestrator_turn')
  sendSessionNotification({
    type: 'tool_use',
    id: START_TOOL_ID,
    name: 'StartMissionRun',
    input: { featureId: FEATURE_ID },
  })
  const requestId = sendPermissionRequest({
    confirmationType: 'start_mission_run',
    toolUses: [{ toolUse: { id: START_TOOL_ID, name: 'StartMissionRun' } }],
    options: ['proceed_once', 'cancel'],
  })
  currentPermission = { requestId, toolUseId: START_TOOL_ID, kind: 'start-run' }
}

function emitWorkerStarted() {
  liveWorkerSessionId = liveWorkerSessionId || `worker-${randomUUID()}`
  appendProgressEntry({
    type: 'worker_selected_feature',
    featureId: FEATURE_ID,
    workerSessionId: liveWorkerSessionId,
  })
  appendProgressEntry({
    type: 'worker_started',
    featureId: FEATURE_ID,
    workerSessionId: liveWorkerSessionId,
  })
  writeMissionSnapshot()
  sendSessionNotification({
    type: 'mission_worker_started',
    workerSessionId: liveWorkerSessionId,
    featureId: FEATURE_ID,
    missionState: 'running',
  })
}

function handleStartRunPermission(message) {
  const selectedOption = String(message.result?.selectedOption || 'cancel')
  resolvePermission(currentPermission.requestId, START_TOOL_ID, selectedOption)
  currentPermission = null

  if (selectedOption === 'cancel') {
    sendSessionNotification({
      type: 'tool_result',
      toolUseId: START_TOOL_ID,
      isError: true,
      content: { message: 'Mission run cancelled by user.' },
    })
    notifyWorkingState('idle')
    return
  }

  sendMissionFeatures('in_progress')
  setMissionState('running')
  appendProgressEntry({ type: 'mission_run_started', message: 'Mission run started' })
  sendSessionNotification({
    type: 'tool_progress_update',
    toolUseId: START_TOOL_ID,
    toolName: 'StartMissionRun',
    update: {
      missionState: 'running',
      currentFeatureId: FEATURE_ID,
      completedFeatures: 0,
      totalFeatures: 1,
      systemMessage: 'Worker is running; use Kill Worker now to verify the user-kill recovery path.',
    },
  })

  setTimeout(() => {
    emitWorkerStarted()
    notifyWorkingState('idle')
  }, 120)
}

function handleKillWorker(message) {
  sendResponse(message.id, { ok: true })
  if (killScheduled || !liveWorkerSessionId) return
  killScheduled = true

  setTimeout(() => {
    appendProgressEntry({
      type: 'worker_failed',
      featureId: FEATURE_ID,
      workerSessionId: liveWorkerSessionId,
      reason: 'Killed by user',
    })
  }, 700)

  setTimeout(() => {
    appendProgressEntry({
      type: 'mission_paused',
      message: 'Mission paused after the user killed the active worker.',
    })
    liveWorkerSessionId = ''
    setMissionState('paused')
    notifyWorkingState('idle')
  }, 1000)
}

function handleFollowUpPrompt() {
  notifyWorkingState('executing_tool')
  appendProgressEntry({
    type: 'mission_run_started',
    message: 'User resumed the same Mission session through normal chat after the kill-worker repro path.',
  })
  setMissionState('orchestrator_turn')
  sendSessionNotification({
    type: 'create_message',
    message: {
      id: `assistant-${randomUUID()}`,
      role: 'assistant',
      content: [
        {
          type: 'text',
          text: 'Mission validation harness preserved the same session after the kill-worker path.',
        },
      ],
    },
  })
  notifyWorkingState('idle')
}

function handleRequest(message) {
  const method = String(message.method || '')

  if (method === 'droid.initialize_session') {
    engineSessionId = engineSessionId || randomUUID()
    sendResponse(message.id, { sessionId: engineSessionId })
    return
  }

  if (method === 'droid.load_session') {
    const requestedSessionId = String(message.params?.sessionId || '').trim()
    if (requestedSessionId) engineSessionId = requestedSessionId
    restoreMissionSnapshotIfPresent()
    sendResponse(message.id, {
      sessionId: engineSessionId || requestedSessionId,
      isMission: true,
      sessionKind: 'mission',
      interactionMode: 'agi',
      autonomyLevel: 'high',
      decompSessionType: 'orchestrator',
      missionDir: missionDir || undefined,
      mission: {
        ...buildStateSnapshot(),
        features,
      },
    })
    return
  }

  if (method === 'droid.update_session_settings') {
    sendResponse(message.id, { ok: true })
    return
  }

  if (method === 'droid.list_skills') {
    sendResponse(message.id, { skills: [] })
    return
  }

  if (method === 'droid.interrupt_session') {
    sendResponse(message.id, { ok: true })
    return
  }

  if (method === 'droid.kill_worker_session') {
    handleKillWorker(message)
    return
  }

  if (method === 'droid.add_user_message') {
    sendResponse(message.id, { ok: true })
    if (currentPermission) return
    if (!missionDir) {
      handleMissionPrompt()
    } else {
      handleFollowUpPrompt()
    }
    return
  }

  sendResponse(message.id, null, { code: -32601, message: 'Method not found' })
}

function handleResponse(message) {
  if (!currentPermission || message.id !== currentPermission.requestId) return
  if (currentPermission.kind === 'propose') {
    handleProposalPermission(message)
    return
  }
  if (currentPermission.kind === 'start-run') {
    handleStartRunPermission(message)
  }
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity })
rl.on('line', (line) => {
  let message
  try {
    message = JSON.parse(line)
  } catch {
    return
  }

  if (!message || typeof message !== 'object') return
  if (message.type === 'request') {
    handleRequest(message)
    return
  }
  if (message.type === 'response') {
    handleResponse(message)
  }
})
