import test from 'node:test'
import assert from 'node:assert/strict'

import {
  filterMissionWorkers,
  getMissionWorkerCounts,
  getMissionWorkerProgressItems,
  getMissionWorkerSummaries,
} from '../src/renderer/src/lib/missionWorkerList.ts'
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
    pausedWorkerSessionId: undefined,
    completedFeatures: 0,
    totalFeatures: 0,
    isCompleted: false,
    lastSource: undefined,
    supplemental: null,
    ...overrides,
  }
}

test('Mission WorkerList derives worker summaries, counts, and filters from mission state', () => {
  const mission = createMissionState({
    state: {
      workerSessionIds: ['worker-success', 'worker-failed', 'worker-paused'],
    },
    currentState: 'paused',
    pausedWorkerSessionId: 'worker-paused',
    currentFeatureId: 'feature-c',
    features: [
      {
        id: 'feature-a',
        description: 'Completed worker feature',
        status: 'completed',
        workerSessionIds: ['worker-success'],
        completedWorkerSessionId: 'worker-success',
      },
      {
        id: 'feature-b',
        description: 'Failed worker feature',
        status: 'pending',
        workerSessionIds: ['worker-failed'],
      },
      {
        id: 'feature-c',
        description: 'Paused worker feature',
        status: 'in_progress',
        workerSessionIds: ['worker-paused'],
        currentWorkerSessionId: 'worker-paused',
      },
    ],
    handoffs: [
      {
        fileName: 'feature-a.json',
        payload: {
          featureId: 'feature-a',
          successState: 'success',
          handoff: {
            salientSummary: 'Finished feature A.',
            whatWasImplemented: 'Implemented feature A.',
          },
        },
      },
    ],
    progressEntries: [
      {
        timestamp: '2026-03-11T07:31:39.631Z',
        type: 'worker_started',
        workerSessionId: 'worker-success',
        featureId: 'feature-a',
      },
      {
        timestamp: '2026-03-11T07:33:47.382Z',
        type: 'worker_completed',
        workerSessionId: 'worker-success',
        featureId: 'feature-a',
        successState: 'success',
      },
      {
        timestamp: '2026-03-11T07:34:00.000Z',
        type: 'worker_started',
        workerSessionId: 'worker-failed',
        featureId: 'feature-b',
      },
      {
        timestamp: '2026-03-11T07:35:00.000Z',
        type: 'worker_failed',
        workerSessionId: 'worker-failed',
        featureId: 'feature-b',
        reason: 'Killed by user',
      },
      {
        timestamp: '2026-03-11T07:38:31.470Z',
        type: 'worker_selected_feature',
        workerSessionId: 'worker-paused',
        featureId: 'feature-c',
      },
      {
        timestamp: '2026-03-11T07:38:31.471Z',
        type: 'worker_started',
        workerSessionId: 'worker-paused',
        featureId: 'feature-c',
      },
      {
        timestamp: '2026-03-11T07:41:44.430Z',
        type: 'worker_paused',
        workerSessionId: 'worker-paused',
        featureId: 'feature-c',
      },
    ],
  })

  const workers = getMissionWorkerSummaries(mission, { now: Date.parse('2026-03-11T07:42:00.000Z') })
  assert.equal(workers.length, 3)
  assert.deepEqual(
    workers.map((worker) => [worker.workerSessionId, worker.status]),
    [
      ['worker-paused', 'paused'],
      ['worker-failed', 'failed'],
      ['worker-success', 'success'],
    ],
  )

  const successWorker = workers.find((worker) => worker.workerSessionId === 'worker-success')
  const failedWorker = workers.find((worker) => worker.workerSessionId === 'worker-failed')
  const pausedWorker = workers.find((worker) => worker.workerSessionId === 'worker-paused')

  assert.equal(successWorker?.featureTitle, 'Completed worker feature')
  assert.equal(successWorker?.hasHandoff, true)
  assert.equal(failedWorker?.failureReason, 'Killed by user')
  assert.equal(pausedWorker?.isCurrent, true)

  assert.deepEqual(getMissionWorkerCounts(workers), {
    all: 3,
    active: 1,
    completed: 1,
    failed: 1,
  })
  assert.deepEqual(
    filterMissionWorkers(workers, 'completed').map((worker) => worker.workerSessionId),
    ['worker-success'],
  )
  assert.deepEqual(
    filterMissionWorkers(workers, 'failed').map((worker) => worker.workerSessionId),
    ['worker-failed'],
  )
})

test('Mission WorkerList keeps running workers active and exposes worker-specific progress history', () => {
  const mission = createMissionState({
    currentState: 'running',
    liveWorkerSessionId: 'worker-live',
    currentWorkerSessionId: 'worker-live',
    features: [
      {
        id: 'feature-live',
        description: 'Live worker feature',
        status: 'in_progress',
        workerSessionIds: ['worker-live'],
        currentWorkerSessionId: 'worker-live',
      },
    ],
    progressEntries: [
      {
        timestamp: '2026-03-11T08:00:00.000Z',
        type: 'worker_selected_feature',
        workerSessionId: 'worker-live',
        featureId: 'feature-live',
      },
      {
        timestamp: '2026-03-11T08:00:05.000Z',
        type: 'worker_started',
        workerSessionId: 'worker-live',
        featureId: 'feature-live',
      },
    ],
  })

  const workers = getMissionWorkerSummaries(mission, { now: Date.parse('2026-03-11T08:01:05.000Z') })
  assert.equal(workers[0]?.status, 'running')
  assert.equal(workers[0]?.durationMs, 60_000)

  assert.deepEqual(
    getMissionWorkerProgressItems(mission, 'worker-live').map((item) => item.eventLabel),
    ['Worker selected feature', 'Worker started'],
  )
  assert.match(
    getMissionWorkerProgressItems(mission, 'worker-live')[0]?.detailLabel || '',
    /feature-live/i,
  )
})
