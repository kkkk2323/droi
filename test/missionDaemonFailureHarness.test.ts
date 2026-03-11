import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import { DroidJsonRpcManager } from '../src/backend/droid/jsonrpc/droidJsonRpcManager.ts'
import {
  buildMissionValidationHarnessEnv,
  DAEMON_FAILURE_MISSION_VALIDATION_HARNESS,
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

test('buildMissionValidationHarnessEnv maps daemon-failure mode to the built-in fake droid path', () => {
  const env = buildMissionValidationHarnessEnv({
    env: { [MISSION_VALIDATION_HARNESS_ENV]: DAEMON_FAILURE_MISSION_VALIDATION_HARNESS },
    repoRoot,
  })

  const droidPath = String(env[VALIDATION_DROID_PATH_ENV] || '')
  assert.equal(droidPath, resolve(repoRoot, 'scripts/mission-daemon-failure-droid.cjs'))
  assert.ok(existsSync(droidPath))
})

test(
  'mission daemon failure harness replays propose -> retry -> orchestrator_turn on one session',
  { skip: process.platform === 'win32' },
  async () => {
    const previousOverride = process.env[VALIDATION_DROID_PATH_ENV]
    process.env[VALIDATION_DROID_PATH_ENV] = resolve(repoRoot, 'scripts/mission-daemon-failure-droid.cjs')

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
        prompt: 'Reproduce the daemon failure path.',
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

      await waitFor(() =>
        events.find((event) => event?.type === 'turn-end' && event.sessionId === sessionId),
      )

      const sessionIdReplacements = events.filter((event) => event?.type === 'session-id-replaced')
      assert.equal(sessionIdReplacements.length, 0)

      const runningState = events.find((event) => {
        const notification = getNotification(event)
        return (
          notification?.type === 'mission_state_changed' &&
          (notification.state as any)?.state === 'running'
        )
      })
      assert.ok(runningState)

      const finalState = events.find((event) => {
        const notification = getNotification(event)
        return (
          notification?.type === 'mission_state_changed' &&
          (notification.state as any)?.state === 'orchestrator_turn'
        )
      })
      assert.ok(finalState)

      const retryUpdate = events.find((event) => {
        const notification = getNotification(event)
        return (
          notification?.type === 'tool_progress_update' &&
          notification?.toolName === 'StartMissionRun'
        )
      })
      assert.match(
        JSON.stringify(getNotification(retryUpdate)?.update || {}),
        /Retrying once after refreshing the daemon session/i,
      )

      const failureEntries = events
        .map((event) => getNotification(event))
        .filter((notification) => notification?.type === 'mission_progress_entry')
      assert.ok(
        failureEntries.some((notification) =>
          /factoryd authentication failed after retry/i.test(
            String((notification?.entry as any)?.reason || ''),
          ),
        ),
      )

      const progressLog = readFileSync(resolve(missionDir, 'progress_log.jsonl'), 'utf-8')
      assert.match(progressLog, /Retrying mission run once after factoryd authentication failure/i)
      assert.match(progressLog, /Mission paused after daemon failure/i)

      const firstTurnEventCount = events.length

      await manager.sendUserMessage({
        sessionId,
        resumeSessionId: sessionId,
        machineId: 'm-test',
        cwd: repoRoot,
        prompt: 'continue mission after daemon recovery',
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

      const followUpAssistant = continuationEvents.find((event) => {
        const notification = getNotification(event)
        return (
          notification?.type === 'create_message' &&
          /preserved the same session after the daemon failure path/i.test(
            JSON.stringify((notification as any).message || {}),
          )
        )
      })
      assert.ok(followUpAssistant)

      const continuationLoadRequests = continuationEvents.filter((event) => {
        const request = getRequest(event)
        return request?.method === 'droid.load_session'
      })
      assert.equal(continuationLoadRequests.length, 0)
    } finally {
      manager.disposeAllSessions()
      if (previousOverride === undefined) delete process.env[VALIDATION_DROID_PATH_ENV]
      else process.env[VALIDATION_DROID_PATH_ENV] = previousOverride
    }
  },
)
