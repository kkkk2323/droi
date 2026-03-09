import test from 'node:test'
import assert from 'node:assert/strict'

import {
  MISSION_AUTO_SWITCH_COOLDOWN_MS,
  getMissionStatusSummary,
  getPreferredMissionView,
  shouldApplyMissionAutoSwitch,
  truncateWorkerSessionId,
  type MissionViewMode,
} from '../src/renderer/src/lib/missionPage.ts'
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

test('getPreferredMissionView maps running states to Mission Control and paused states to Chat', () => {
  assert.equal(getPreferredMissionView(createMissionState({ currentState: 'running' })), 'mission-control')
  assert.equal(getPreferredMissionView(createMissionState({ currentState: 'paused' })), 'chat')
  assert.equal(getPreferredMissionView(createMissionState({ currentState: 'orchestrator_turn' })), 'chat')
  assert.equal(getPreferredMissionView(createMissionState({ currentState: 'completed' })), null)
  assert.equal(getPreferredMissionView(undefined), null)
})

test('manual mission view cooldown suppresses auto-switches for 30 seconds', () => {
  const now = 50_000
  const preferred: MissionViewMode = 'mission-control'

  assert.equal(
    shouldApplyMissionAutoSwitch({
      currentView: 'chat',
      preferredView: preferred,
      manualOverrideAt: now - (MISSION_AUTO_SWITCH_COOLDOWN_MS - 1),
      now,
    }),
    false,
  )

  assert.equal(
    shouldApplyMissionAutoSwitch({
      currentView: 'chat',
      preferredView: preferred,
      manualOverrideAt: now - MISSION_AUTO_SWITCH_COOLDOWN_MS,
      now,
    }),
    true,
  )

  assert.equal(
    shouldApplyMissionAutoSwitch({
      currentView: 'mission-control',
      preferredView: preferred,
      manualOverrideAt: now - 1_000,
      now,
    }),
    false,
  )

  assert.equal(
    shouldApplyMissionAutoSwitch({
      currentView: 'chat',
      preferredView: null,
      manualOverrideAt: undefined,
      now,
    }),
    false,
  )
})

test('getMissionStatusSummary returns mission state, progress, feature title, and worker context', () => {
  const mission = createMissionState({
    currentState: 'running',
    currentFeatureId: 'mission-page-chat-shell-and-view-toggle',
    currentWorkerSessionId: 'worker-session-1234567890',
    completedFeatures: 2,
    totalFeatures: 5,
    features: [
      { id: 'mission-page-chat-shell-and-view-toggle', description: 'Build MissionPage toggle' },
    ],
  })

  assert.deepEqual(getMissionStatusSummary(mission), {
    stateLabel: 'Running',
    progressLabel: '2/5',
    currentFeatureLabel: 'Build MissionPage toggle',
    workerLabel: truncateWorkerSessionId('worker-session-1234567890'),
  })
})

test('getMissionStatusSummary falls back cleanly when feature metadata is missing', () => {
  const mission = createMissionState({
    currentState: 'paused',
    currentFeatureId: 'feature-2',
    completedFeatures: 0,
    totalFeatures: 0,
  })

  assert.deepEqual(getMissionStatusSummary(mission), {
    stateLabel: 'Paused',
    progressLabel: '0/0',
    currentFeatureLabel: 'feature-2',
    workerLabel: 'No active worker',
  })
})
