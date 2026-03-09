import test from 'node:test'
import assert from 'node:assert/strict'

import {
  getMissionControlStatus,
  getMissionFeatureQueueItems,
  getMissionHandoffCards,
  getMissionProgressTimelineItems,
} from '../src/renderer/src/lib/missionControl.ts'
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

test('Mission Control status and feature queue preserve feature order while marking validator work', () => {
  const mission = createMissionState({
    currentState: 'running',
    currentFeatureId: 'scrutiny-validator-mission-sync-recovery',
    completedFeatures: 2,
    totalFeatures: 4,
    features: [
      {
        id: 'mission-dir-watcher-ipc-and-path-capture',
        description: 'Implement Mission dir watcher',
        status: 'completed',
      },
      {
        id: 'mission-store-restore-and-reconciliation',
        description: 'Implement Mission restore state reconciliation',
        status: 'completed',
      },
      {
        id: 'scrutiny-validator-mission-sync-recovery',
        description: 'Scrutiny validation for mission-sync-recovery',
        status: 'in_progress',
        skillName: 'scrutiny-validator',
      },
      {
        id: 'user-testing-validator-mission-sync-recovery',
        description: 'User testing validation for mission-sync-recovery',
        status: 'pending',
        skillName: 'user-testing-validator',
      },
    ],
  })

  assert.deepEqual(getMissionControlStatus(mission), {
    stateLabel: 'Running',
    progressLabel: '2/4 completed',
    currentFeatureLabel: 'Scrutiny validation for mission-sync-recovery',
    phaseLabel: 'Validation in progress',
  })

  const queue = getMissionFeatureQueueItems(mission)
  assert.deepEqual(
    queue.map((item) => item.id),
    [
      'mission-dir-watcher-ipc-and-path-capture',
      'mission-store-restore-and-reconciliation',
      'scrutiny-validator-mission-sync-recovery',
      'user-testing-validator-mission-sync-recovery',
    ],
  )
  assert.equal(queue[2]?.isCurrent, true)
  assert.equal(queue[2]?.isValidator, true)
  assert.equal(queue[3]?.isValidator, true)
  assert.equal(queue[2]?.testId, 'mission-feature-scrutiny-validator-mission-sync-recovery')
})

test('Mission Control timeline sorts chronologically, labels events, and deduplicates repeats', () => {
  const mission = createMissionState({
    progressEntries: [
      {
        timestamp: '2026-03-09T02:07:12.035Z',
        type: 'milestone_validation_triggered',
        milestone: 'mission-foundation',
      },
      {
        timestamp: '2026-03-09T01:29:00.000Z',
        type: 'worker_completed',
        featureId: 'mission-session-entry-routing-and-sidebar',
        successState: 'success',
      },
      {
        timestamp: '2026-03-09T01:29:00.000Z',
        type: 'worker_completed',
        featureId: 'mission-session-entry-routing-and-sidebar',
        successState: 'success',
      },
      {
        timestamp: '2026-03-09T01:28:53.000Z',
        type: 'worker_started',
        featureId: 'mission-session-entry-routing-and-sidebar',
        workerSessionId: 'worker-123',
      },
    ],
  })

  const timeline = getMissionProgressTimelineItems(mission)
  assert.equal(timeline.length, 3)
  assert.deepEqual(
    timeline.map((item) => item.eventLabel),
    ['Worker started', 'Worker completed', 'Milestone validation triggered'],
  )
  assert.match(timeline[0]?.detailLabel || '', /worker-123/)
  assert.match(timeline[1]?.detailLabel || '', /Succeeded/i)
  assert.match(timeline[2]?.detailLabel || '', /mission-foundation/)
  assert.ok(timeline.every((item) => item.timestampLabel.length > 0))
})

test('Mission Control timeline distinguishes user kill and daemon failure events', () => {
  const mission = createMissionState({
    progressEntries: [
      {
        timestamp: '2026-03-09T01:28:53.000Z',
        type: 'worker_failed',
        reason: 'factoryd authentication failed after retry',
      },
      {
        timestamp: '2026-03-09T01:29:00.000Z',
        type: 'worker_failed',
        reason: 'Killed by user',
      },
      {
        timestamp: '2026-03-09T01:29:05.000Z',
        type: 'mission_paused',
      },
    ],
  })

  const timeline = getMissionProgressTimelineItems(mission)
  assert.deepEqual(
    timeline.map((item) => item.eventLabel),
    ['Daemon failure', 'Worker killed by user', 'Mission paused'],
  )
  assert.match(timeline[0]?.detailLabel || '', /factoryd authentication failed after retry/i)
  assert.match(timeline[1]?.detailLabel || '', /Killed by user/i)
})

test('Mission Control handoff cards extract required summary and verification fields', () => {
  const mission = createMissionState({
    handoffs: [
      {
        fileName: 'mission-page-chat-shell-and-view-toggle.json',
        payload: {
          featureId: 'mission-page-chat-shell-and-view-toggle',
          successState: 'success',
          handoff: {
            salientSummary: 'Implemented MissionPage shell and toggle.',
            whatWasImplemented: 'Built MissionPage with Chat and Mission Control views.',
            verification: {
              commandsRun: [
                {
                  command: 'pnpm check',
                  exitCode: 0,
                  observation: 'All validators passed.',
                },
              ],
              interactiveChecks: [
                {
                  action: 'Opened Mission page',
                  observed: 'Mission status bar remained visible.',
                },
              ],
            },
          },
        },
      },
    ],
  })

  const cards = getMissionHandoffCards(mission)
  assert.equal(cards.length, 1)
  assert.deepEqual(cards[0], {
    featureId: 'mission-page-chat-shell-and-view-toggle',
    title: 'mission-page-chat-shell-and-view-toggle',
    testId: 'mission-handoff-mission-page-chat-shell-and-view-toggle',
    successState: 'success',
    salientSummary: 'Implemented MissionPage shell and toggle.',
    whatWasImplemented: 'Built MissionPage with Chat and Mission Control views.',
    commandResults: ['pnpm check — All validators passed.'],
    interactiveResults: ['Opened Mission page — Mission status bar remained visible.'],
  })
})
