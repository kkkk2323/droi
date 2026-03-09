import test from 'node:test'
import assert from 'node:assert/strict'

import {
  MISSION_RUNNING_INPUT_PLACEHOLDER,
  getMissionActionState,
  getMissionInputSemantics,
  getMissionPermissionCardPresentation,
  getMissionPermissionOptionLabel,
  getMissionRuntimeStatus,
} from '../src/renderer/src/lib/missionUiSemantics.ts'
import type { ChatMessage } from '../src/renderer/src/types.ts'
import type { PendingPermissionRequest } from '../src/renderer/src/state/appReducer.ts'
import type { MissionState } from '../src/renderer/src/state/missionState.ts'

function createMissionState(overrides: Partial<MissionState> = {}): MissionState {
  return {
    state: null,
    features: [],
    progressEntries: [],
    handoffs: [],
    validationState: null,
    currentState: undefined,
    currentFeatureId: undefined,
    currentWorkerSessionId: undefined,
    liveWorkerSessionId: undefined,
    completedFeatures: 0,
    totalFeatures: 0,
    isCompleted: false,
    lastSource: undefined,
    supplemental: null,
    ...overrides,
  }
}

function createPermissionRequest(
  overrides: Partial<PendingPermissionRequest> = {},
): PendingPermissionRequest {
  return {
    requestId: 'perm-1',
    toolUses: [],
    options: ['proceed_once', 'cancel'],
    optionsMeta: [],
    raw: {
      jsonrpc: '2.0',
      factoryApiVersion: '1.0.0',
      type: 'request',
      id: 'perm-1',
      method: 'droid.request_permission',
      params: {},
    } as any,
    ...overrides,
  }
}

function createStartMissionRunMessage(payload: unknown): ChatMessage {
  return {
    id: 'assistant-start-mission-run',
    role: 'assistant',
    blocks: [
      {
        kind: 'tool_call',
        callId: 'tool-start-mission-run',
        toolName: 'StartMissionRun',
        parameters: {},
        result: JSON.stringify(payload),
      },
    ],
    timestamp: Date.now(),
  }
}

test('permission copy uses Mission-specific labels for propose_mission and start_mission_run', () => {
  const propose = createPermissionRequest({ confirmationType: 'propose_mission' })
  const run = createPermissionRequest({ confirmationType: 'start_mission_run' })

  assert.deepEqual(getMissionPermissionCardPresentation(propose), {
    badgeLabel: 'Mission permission',
    title: 'Mission proposal ready',
    description: 'Review the proposed Mission plan before launching work for this orchestrator session.',
    primaryActionLabel: 'Accept Mission Proposal',
    secondaryActionLabel: 'Cancel',
  })
  assert.equal(getMissionPermissionOptionLabel(propose, 'proceed_once'), 'Accept Mission Proposal')
  assert.equal(getMissionPermissionOptionLabel(run, 'proceed_once'), 'Start Mission Run')
  assert.equal(getMissionPermissionOptionLabel(run, 'cancel'), 'Cancel')
})

test('mission permission options preserve distinct backend approval semantics when multiple choices are offered', () => {
  const proposal = createPermissionRequest({
    confirmationType: 'propose_mission',
    options: ['proceed_once', 'proceed_always', 'proceed_auto_run_high', 'cancel'],
    optionsMeta: [
      { value: 'proceed_once', label: 'Approve once' },
      { value: 'proceed_always', label: 'Always allow for this Mission' },
      { value: 'proceed_auto_run_high', label: 'Auto-run Mission workers (High)' },
      { value: 'cancel', label: 'Decline' },
    ],
  })

  assert.equal(getMissionPermissionOptionLabel(proposal, 'proceed_once'), 'Approve once')
  assert.equal(
    getMissionPermissionOptionLabel(proposal, 'proceed_always'),
    'Always allow for this Mission',
  )
  assert.equal(
    getMissionPermissionOptionLabel(proposal, 'proceed_auto_run_high'),
    'Auto-run Mission workers (High)',
  )
  assert.equal(getMissionPermissionOptionLabel(proposal, 'cancel'), 'Cancel')

  const run = createPermissionRequest({
    confirmationType: 'start_mission_run',
    options: ['proceed_once', 'proceed_auto_run_medium', 'cancel'],
  })

  assert.equal(getMissionPermissionOptionLabel(run, 'proceed_once'), 'Proceed once')
  assert.equal(getMissionPermissionOptionLabel(run, 'proceed_auto_run_medium'), 'Auto-run (Medium)')
})

test('pause and kill controls only appear in valid Mission states', () => {
  assert.deepEqual(
    getMissionActionState(
      createMissionState({ currentState: 'running', liveWorkerSessionId: 'worker-123' }),
    ),
    {
      canPause: true,
      canKillWorker: true,
      workerSessionId: 'worker-123',
    },
  )

  assert.deepEqual(
    getMissionActionState(createMissionState({ currentState: 'running' })),
    {
      canPause: true,
      canKillWorker: false,
      workerSessionId: undefined,
    },
  )

  assert.deepEqual(
    getMissionActionState(
      createMissionState({ currentState: 'paused', liveWorkerSessionId: 'worker-123' }),
    ),
    {
      canPause: false,
      canKillWorker: false,
      workerSessionId: undefined,
    },
  )
})

test('input semantics disable Mission chat while running and re-enable it when paused', () => {
  assert.deepEqual(getMissionInputSemantics(createMissionState({ currentState: 'running' })), {
    disabled: true,
    placeholder: MISSION_RUNNING_INPUT_PLACEHOLDER,
  })

  assert.deepEqual(getMissionInputSemantics(createMissionState({ currentState: 'paused' })), {
    disabled: false,
    placeholder: undefined,
  })

  assert.deepEqual(
    getMissionInputSemantics(createMissionState({ currentState: 'orchestrator_turn' })),
    {
      disabled: false,
      placeholder: undefined,
    },
  )
})

test('pause messaging distinguishes user pauses from normal continue-via-chat recovery', () => {
  const pausedByUser = getMissionRuntimeStatus({
    mission: createMissionState({
      currentState: 'paused',
      progressEntries: [{ type: 'worker_paused', timestamp: '2026-03-09T12:00:00.000Z' }],
    }),
  })
  assert.equal(pausedByUser.kind, 'paused-by-user')
  assert.match(pausedByUser.title, /paused by user/i)
  assert.match(pausedByUser.description, /normal chat/i)

  const orchestratorTurn = getMissionRuntimeStatus({
    mission: createMissionState({ currentState: 'orchestrator_turn' }),
  })
  assert.equal(orchestratorTurn.kind, 'ready-to-continue')
  assert.match(orchestratorTurn.description, /Start Mission Run again/i)
})

test('kill messaging distinguishes user-killed workers from daemon failures and acknowledges pending kill', () => {
  const pendingKill = getMissionRuntimeStatus({
    mission: createMissionState({ currentState: 'running', liveWorkerSessionId: 'worker-123' }),
    pendingAction: 'kill',
  })
  assert.equal(pendingKill.kind, 'kill-pending')
  assert.match(pendingKill.title, /Kill request sent/i)

  const killedByUser = getMissionRuntimeStatus({
    mission: createMissionState({
      currentState: 'paused',
      progressEntries: [
        {
          type: 'worker_failed',
          reason: 'Killed by user',
          timestamp: '2026-03-09T12:05:00.000Z',
        },
        { type: 'mission_paused', timestamp: '2026-03-09T12:05:01.000Z' },
      ],
    }),
  })
  assert.equal(killedByUser.kind, 'paused-after-user-kill')
  assert.match(killedByUser.description, /terminated at your request/i)
})

test('retry messaging surfaces daemon retry guidance and bounded failure recovery', () => {
  const retrying = getMissionRuntimeStatus({
    mission: createMissionState({ currentState: 'running' }),
    messages: [
      createStartMissionRunMessage({
        systemMessage:
          'factoryd authentication failed. Retrying once after refreshing the daemon session.',
      }),
    ],
  })
  assert.equal(retrying.kind, 'daemon-retrying')
  assert.match(retrying.description, /Retrying once/i)

  const failedAfterRetry = getMissionRuntimeStatus({
    mission: createMissionState({
      currentState: 'paused',
      progressEntries: [
        {
          type: 'worker_failed',
          reason: 'factoryd authentication failed after retry',
          timestamp: '2026-03-09T12:10:00.000Z',
        },
        { type: 'mission_paused', timestamp: '2026-03-09T12:10:01.000Z' },
      ],
    }),
    messages: [
      createStartMissionRunMessage({
        systemMessage:
          'factoryd authentication failed. Retrying once after refreshing the daemon session.',
      }),
    ],
  })
  assert.equal(failedAfterRetry.kind, 'daemon-failed')
  assert.match(failedAfterRetry.title, /retry/i)
  assert.match(failedAfterRetry.description, /normal chat/i)
})

test('runtime status keeps Mission pending while validation completion has not settled', () => {
  const validationPending = getMissionRuntimeStatus({
    mission: createMissionState({
      currentState: 'completed',
      isCompleted: false,
      validationState: {
        assertions: {
          'VAL-CONTROL-004': { status: 'pending' },
        },
      },
    }),
  })

  assert.equal(validationPending.kind, 'validation-pending')
  assert.match(validationPending.title, /validation pending/i)
  assert.match(validationPending.description, /not complete until validation settles/i)
})
