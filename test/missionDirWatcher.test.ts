import test from 'node:test'
import assert from 'node:assert/strict'
import type { FSWatcher } from 'node:fs'
import { appendFile, mkdir, mkdtemp, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { readMissionDirSnapshot, resolveMissionDirPath } from '../src/backend/mission/missionDirReader.ts'
import { MissionDirWatcher } from '../src/backend/mission/missionDirWatcher.ts'

async function waitFor(predicate: () => boolean, timeoutMs = 1500): Promise<void> {
  const startedAt = Date.now()
  while (!predicate()) {
    if (Date.now() - startedAt > timeoutMs) {
      throw new Error(`Timed out after ${timeoutMs}ms`)
    }
    await delay(20)
  }
}

function createDeferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((innerResolve) => {
    resolve = innerResolve
  })
  return { promise, resolve }
}

test('readMissionDirSnapshot reads MissionDir payloads and resolves fallback missionDir paths', async () => {
  const missionDir = await mkdtemp(join(tmpdir(), 'droi-mission-dir-'))
  await writeFile(
    join(missionDir, 'state.json'),
    JSON.stringify({ missionState: 'running', updatedAt: '2026-03-09T10:00:00.000Z' }),
  )
  await writeFile(
    join(missionDir, 'features.json'),
    JSON.stringify({ features: [{ id: 'feature-1', status: 'in_progress' }] }),
  )
  await writeFile(
    join(missionDir, 'progress_log.jsonl'),
    `${JSON.stringify({ type: 'mission_run_started' })}\n${JSON.stringify({ type: 'worker_started' })}\n`,
  )
  await mkdir(join(missionDir, 'handoffs'))
  await writeFile(
    join(missionDir, 'handoffs', 'feature-1.json'),
    JSON.stringify({ salientSummary: 'Worker finished feature-1' }),
  )
  await writeFile(
    join(missionDir, 'validation-state.json'),
    JSON.stringify({ status: 'running' }),
  )

  const snapshot = await readMissionDirSnapshot(missionDir)
  assert.equal(snapshot.exists, true)
  assert.equal(snapshot.missionDir, missionDir)
  assert.equal(snapshot.state?.missionState, 'running')
  assert.deepEqual(snapshot.features?.map((feature) => feature.id), ['feature-1'])
  assert.equal(snapshot.progressEntries.length, 2)
  assert.equal(snapshot.handoffs.length, 1)
  assert.equal(snapshot.handoffs[0]?.fileName, 'feature-1.json')
  assert.equal(snapshot.validationState?.status, 'running')

  assert.equal(
    resolveMissionDirPath({ sessionId: 'base-session-123' }),
    join(homedir(), '.factory', 'missions', 'base-session-123'),
  )
  assert.equal(
    resolveMissionDirPath({ sessionId: 'base-session-123', missionDir: '~/.factory/missions/custom' }),
    join(homedir(), '.factory', 'missions', 'custom'),
  )
  assert.equal(
    resolveMissionDirPath({
      sessionId: 'replaced-session-456',
      missionBaseSessionId: 'base-session-123',
    }),
    join(homedir(), '.factory', 'missions', 'base-session-123'),
  )
})

test('MissionDirWatcher tolerates late missionDir creation and late handoffs or validation files', async () => {
  const root = await mkdtemp(join(tmpdir(), 'droi-mission-watch-'))
  const missionDir = join(root, 'late-mission')
  const events: Array<{ changedPaths: string[]; snapshot: { exists: boolean } }> = []
  const watcher = new MissionDirWatcher({
    sessionId: 'mission-late',
    missionDir,
    pollIntervalMs: 25,
    onChange: (event) => {
      events.push({ changedPaths: event.changedPaths, snapshot: { exists: event.snapshot.exists } })
    },
  })

  try {
    await watcher.start()
    await delay(80)
    assert.equal(events.length, 0)

    await mkdir(missionDir)
    await writeFile(
      join(missionDir, 'state.json'),
      JSON.stringify({ missionState: 'running', updatedAt: '2026-03-09T10:00:00.000Z' }),
    )
    await writeFile(
      join(missionDir, 'progress_log.jsonl'),
      `${JSON.stringify({ type: 'mission_run_started' })}\n`,
    )

    await waitFor(() => events.some((event) => event.snapshot.exists))
    assert.equal(events.some((event) => event.changedPaths.includes('state.json')), true)

    await mkdir(join(missionDir, 'handoffs'))
    await writeFile(
      join(missionDir, 'handoffs', 'worker-1.json'),
      JSON.stringify({ salientSummary: 'Late handoff landed' }),
    )
    await writeFile(join(missionDir, 'validation-state.json'), JSON.stringify({ status: 'passed' }))

    await waitFor(
      () =>
        events.some((event) => event.changedPaths.includes('handoffs/worker-1.json')) &&
        events.some((event) => event.changedPaths.includes('validation-state.json')),
    )
  } finally {
    await watcher.stop()
  }
})

test('MissionDirWatcher emits paused MissionDir updates when only state.json and progress_log.jsonl change', async () => {
  const missionDir = await mkdtemp(join(tmpdir(), 'droi-mission-paused-'))
  await writeFile(
    join(missionDir, 'state.json'),
    JSON.stringify({ missionState: 'running', updatedAt: '2026-03-09T10:00:00.000Z' }),
  )
  await writeFile(
    join(missionDir, 'progress_log.jsonl'),
    `${JSON.stringify({ type: 'mission_run_started' })}\n`,
  )

  const events: Array<{ changedPaths: string[] }> = []
  const watcher = new MissionDirWatcher({
    sessionId: 'mission-paused',
    missionDir,
    pollIntervalMs: 25,
    onChange: (event) => {
      events.push({ changedPaths: event.changedPaths })
    },
  })

  try {
    await watcher.start()
    await waitFor(() => events.length > 0)
    const initialEventCount = events.length

    await writeFile(
      join(missionDir, 'state.json'),
      JSON.stringify({ missionState: 'paused', updatedAt: '2026-03-09T10:00:05.000Z' }),
    )
    await appendFile(
      join(missionDir, 'progress_log.jsonl'),
      `${JSON.stringify({ type: 'mission_paused' })}\n`,
    )

    await waitFor(() => {
      const subsequentEvents = events.slice(initialEventCount)
      return subsequentEvents.some(
        (event) =>
          event.changedPaths.includes('state.json') &&
          event.changedPaths.includes('progress_log.jsonl'),
      )
    })

    const pausedEvent = events
      .slice(initialEventCount)
      .find(
        (event) =>
          event.changedPaths.includes('state.json') &&
          event.changedPaths.includes('progress_log.jsonl'),
      )
    assert.ok(pausedEvent)
    assert.equal(pausedEvent.changedPaths.includes('features.json'), false)
    assert.equal(pausedEvent.changedPaths.some((value) => value.startsWith('handoffs/')), false)
  } finally {
    await watcher.stop()
  }
})

test(
  'MissionDirWatcher stop waits for an in-flight sync without leaking watchers or late events',
  { timeout: 2_000 },
  async () => {
    const missionDir = await mkdtemp(join(tmpdir(), 'droi-mission-stop-race-'))
    const blockedMissionDirCheck = createDeferred<boolean>()
    const events: Array<{ changedPaths: string[]; missionState: string | null }> = []
    let missionDirChecks = 0
    let snapshotReads = 0
    let activeWatchers = 0
    let missionDirListener: (() => void) | null = null

    const watcher = new MissionDirWatcher({
      sessionId: 'mission-stop-race',
      missionDir,
      pollIntervalMs: 25,
      isDirectory: async (path) => {
        if (path === missionDir) {
          missionDirChecks += 1
          if (missionDirChecks === 3) {
            return blockedMissionDirCheck.promise
          }
          return true
        }
        if (path === join(missionDir, 'handoffs')) return false
        if (path === dirname(missionDir)) return true
        return false
      },
      readSnapshot: async () => {
        snapshotReads += 1
        const missionState = snapshotReads === 1 ? 'running' : 'paused'
        return {
          missionDir,
          exists: true,
          state: { missionState },
          features: null,
          progressEntries: [],
          handoffs: [],
          validationState: null,
        }
      },
      watchPath: (path, listener) => {
        activeWatchers += 1
        if (path === missionDir) missionDirListener = listener
        let closed = false
        return {
          close() {
            if (closed) return
            closed = true
            activeWatchers -= 1
          },
          ref() {
            return this
          },
          unref() {
            return this
          },
        } as unknown as FSWatcher
      },
      onChange: (event) => {
        events.push({
          changedPaths: event.changedPaths,
          missionState:
            typeof event.snapshot.state?.missionState === 'string'
              ? event.snapshot.state.missionState
              : null,
        })
      },
    })

    await watcher.start()
    assert.equal(activeWatchers, 1)
    assert.deepEqual(events, [{ changedPaths: ['state.json'], missionState: 'running' }])

    missionDirListener?.()
    await waitFor(() => missionDirChecks >= 3)

    const stopPromise = watcher.stop()
    await delay(40)
    assert.equal(activeWatchers, 0)

    blockedMissionDirCheck.resolve(true)
    await stopPromise
    await delay(40)

    assert.equal(activeWatchers, 0)
    assert.deepEqual(events, [{ changedPaths: ['state.json'], missionState: 'running' }])
  },
)
