import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import { DroidJsonRpcManager } from '../src/backend/droid/jsonrpc/droidJsonRpcManager.ts'
import {
  buildMissionValidationHarnessEnv,
  KILL_WORKER_MISSION_VALIDATION_HARNESS,
  MISSION_VALIDATION_HARNESS_ENV,
  VALIDATION_DROID_PATH_ENV,
} from '../src/backend/droid/missionValidationHarness.ts'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

async function waitFor<T>(fn: () => T | undefined, timeoutMs = 5000): Promise<T> {
  const startedAt = Date.now()
  while (Date.now() - startedAt < timeoutMs) {
    const value = fn()
    if (value !== undefined) return value
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
  throw new Error('Timed out waiting for condition')
}

function getNotification(event: any): Record<string, unknown> | null {
  if (event?.type !== 'rpc-notification') return null
  const notification = event.message?.params?.notification
  return notification && typeof notification === 'object'
    ? (notification as Record<string, unknown>)
    : null
}

function getRequest(event: any): Record<string, unknown> | null {
  const request = event?.type === 'rpc-request' ? event.message : null
  return request && typeof request === 'object' ? (request as Record<string, unknown>) : null
}

test('buildMissionValidationHarnessEnv maps kill-worker mode to the built-in fake droid path', () => {
  const env = buildMissionValidationHarnessEnv({
    env: { [MISSION_VALIDATION_HARNESS_ENV]: KILL_WORKER_MISSION_VALIDATION_HARNESS },
    repoRoot,
  })

  const droidPath = String(env[VALIDATION_DROID_PATH_ENV] || '')
  assert.equal(droidPath, resolve(repoRoot, 'scripts/mission-kill-worker-droid.cjs'))
  assert.ok(existsSync(droidPath))
})

test('explicit kill-worker validation launchers are wired for package scripts and mission services', () => {
  const packageJson = JSON.parse(readFileSync(resolve(repoRoot, 'package.json'), 'utf-8'))
  assert.equal(
    packageJson?.scripts?.['dev:test:kill-worker'],
    'DROI_MISSION_VALIDATION_HARNESS=kill-worker pnpm dev:test',
  )

  const servicesYaml = readFileSync(resolve(repoRoot, '.factory/services.yaml'), 'utf-8')
  assert.match(
    servicesYaml,
    /^  mission-kill-worker-harness: DROI_MISSION_VALIDATION_HARNESS=kill-worker pnpm dev:test$/m,
  )
})

test(
  'mission kill-worker harness replays one live kill -> paused -> same-session continuation flow',
  { skip: process.platform === 'win32' },
  async () => {
    const previousOverride = process.env[VALIDATION_DROID_PATH_ENV]
    process.env[VALIDATION_DROID_PATH_ENV] = resolve(repoRoot, 'scripts/mission-kill-worker-droid.cjs')

    const events: any[] = []
    const manager = new DroidJsonRpcManager({
      emit: (event) => {
        events.push(event)
      },
    })

    try {
      const sessionId = await manager.createSession({
        machineId: 'm-test',
        cwd: repoRoot,
        interactionMode: 'agi' as any,
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
        isMission: true,
        sessionKind: 'mission',
        env: {},
      })

      await manager.sendUserMessage({
        sessionId,
        resumeSessionId: sessionId,
        machineId: 'm-test',
        cwd: repoRoot,
        prompt: 'Drive the deterministic kill-worker proof flow.',
        interactionMode: 'agi' as any,
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
        isMission: true,
        sessionKind: 'mission',
        env: {},
      })

      const proposalPermission = await waitFor(() =>
        events.find((event) => {
          const request = getRequest(event)
          return (
            request?.method === 'droid.request_permission' &&
            (request.params as any)?.confirmationType === 'propose_mission'
          )
        }),
      )

      manager.respondPermission({
        sessionId,
        requestId: String((proposalPermission.message as any).id || ''),
        selectedOption: 'proceed_once',
      })

      const proposeResult = await waitFor(() =>
        events.find((event) => {
          const notification = getNotification(event)
          return (
            notification?.type === 'tool_result' &&
            notification?.toolUseId === 'mission-harness-propose'
          )
        }),
      )
      const missionDir = String(((getNotification(proposeResult) as any)?.content || {}).missionDir || '')
      assert.ok(missionDir)
      assert.ok(existsSync(resolve(missionDir, 'state.json')))
      assert.ok(existsSync(resolve(missionDir, 'features.json')))

      const featuresChanged = await waitFor(() =>
        events.find((event) => {
          const notification = getNotification(event)
          return notification?.type === 'mission_features_changed'
        }),
      )
      assert.match(JSON.stringify((getNotification(featuresChanged) as any)?.features || []), /mission-cross-flow-kill-worker-repro-harness/)

      const startPermission = await waitFor(() =>
        events.find((event) => {
          const request = getRequest(event)
          return (
            request?.method === 'droid.request_permission' &&
            (request.params as any)?.confirmationType === 'start_mission_run'
          )
        }),
      )

      manager.respondPermission({
        sessionId,
        requestId: String((startPermission.message as any).id || ''),
        selectedOption: 'proceed_once',
      })

      const workerStarted = await waitFor(() =>
        events.find((event) => {
          const notification = getNotification(event)
          return notification?.type === 'mission_worker_started'
        }),
      )

      const workerSessionId = String((getNotification(workerStarted) as any)?.workerSessionId || '')
      assert.match(workerSessionId, /^worker-/)

      await manager.killWorkerSession({
        sessionId,
        workerSessionId,
      })

      const killedEntry = await waitFor(() =>
        events.find((event) => {
          const notification = getNotification(event)
          return (
            notification?.type === 'mission_progress_entry' &&
            /Killed by user/i.test(String((notification.entry as any)?.reason || ''))
          )
        }),
      )
      assert.ok(killedEntry)

      const pausedState = await waitFor(() =>
        events.find((event) => {
          const notification = getNotification(event)
          return (
            notification?.type === 'mission_state_changed' &&
            (notification.state as any)?.state === 'paused'
          )
        }),
      )
      assert.ok(pausedState)

      const progressLog = readFileSync(resolve(missionDir, 'progress_log.jsonl'), 'utf-8')
      assert.match(progressLog, /worker_selected_feature/)
      assert.match(progressLog, /worker_started/)
      assert.match(progressLog, /Killed by user/)
      assert.match(progressLog, /mission_paused/)

      const pausedStateFile = JSON.parse(readFileSync(resolve(missionDir, 'state.json'), 'utf-8'))
      assert.equal(pausedStateFile.baseSessionId, sessionId)
      assert.equal(pausedStateFile.state, 'paused')
      assert.equal(pausedStateFile.currentFeatureId, 'mission-cross-flow-kill-worker-repro-harness')
      assert.equal(pausedStateFile.currentWorkerSessionId, undefined)

      const firstTurnEventCount = events.length

      await manager.sendUserMessage({
        sessionId,
        resumeSessionId: sessionId,
        machineId: 'm-test',
        cwd: repoRoot,
        prompt: 'continue after the kill-worker proof',
        interactionMode: 'agi' as any,
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
        isMission: true,
        sessionKind: 'mission',
        env: {},
      })

      await waitFor(() =>
        events
          .slice(firstTurnEventCount)
          .find((event) => event?.type === 'turn-end' && event.sessionId === sessionId),
      )

      const continuationEvents = events.slice(firstTurnEventCount)
      assert.equal(
        continuationEvents.some((event) => {
          const request = getRequest(event)
          return request?.method === 'droid.request_permission'
        }),
        false,
      )

      const continuationLoadRequests = continuationEvents.filter((event) => {
        const request = getRequest(event)
        return request?.method === 'droid.load_session'
      })
      assert.equal(continuationLoadRequests.length, 0)

      const followUpAssistant = continuationEvents.find((event) => {
        const notification = getNotification(event)
        return (
          notification?.type === 'create_message' &&
          /same session after the kill-worker path/i.test(
            JSON.stringify((notification as any).message || {}),
          )
        )
      })
      assert.ok(followUpAssistant)
    } finally {
      manager.disposeAllSessions()
      if (previousOverride === undefined) delete process.env[VALIDATION_DROID_PATH_ENV]
      else process.env[VALIDATION_DROID_PATH_ENV] = previousOverride
    }
  },
)
