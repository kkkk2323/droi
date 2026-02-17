import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import type { SetupScriptEvent } from '../src/shared/protocol.ts'
import { SetupScriptRunner } from '../src/backend/session/setupScriptRunner.ts'

function waitForFinished(runner: SetupScriptRunner, sessionId: string): Promise<SetupScriptEvent> {
  return new Promise((resolvePromise, rejectPromise) => {
    const timeout = setTimeout(() => {
      unsub()
      rejectPromise(new Error('Timed out waiting for setup script finish event'))
    }, 5000)

    const unsub = runner.onEvent((event) => {
      if (event.type !== 'finished' || event.sessionId !== sessionId) return
      clearTimeout(timeout)
      unsub()
      resolvePromise(event)
    })
  })
}

test('setupScriptRunner emits output and success finish event', async () => {
  const projectDir = await mkdtemp(join(tmpdir(), 'droid-setup-runner-'))
  const runner = new SetupScriptRunner()
  const events: SetupScriptEvent[] = []
  const unsub = runner.onEvent((event) => events.push(event))

  const finishedPromise = waitForFinished(runner, 's1')
  await runner.run({ sessionId: 's1', projectDir, script: 'echo setup-ok' })
  const finished = await finishedPromise
  unsub()

  assert.equal(events[0]?.type, 'started')
  assert.ok(events.some((e) => e.type === 'output' && e.data.includes('setup-ok')))
  assert.equal(finished.type, 'finished')
  if (finished.type === 'finished') {
    assert.equal(finished.success, true)
    assert.equal(finished.exitCode, 0)
  }
})

test('setupScriptRunner emits failed finish event for non-zero exit', async () => {
  const projectDir = await mkdtemp(join(tmpdir(), 'droid-setup-runner-fail-'))
  const runner = new SetupScriptRunner()

  const finishedPromise = waitForFinished(runner, 's2')
  await runner.run({ sessionId: 's2', projectDir, script: 'echo setup-fail >&2; exit 2' })
  const finished = await finishedPromise

  assert.equal(finished.type, 'finished')
  if (finished.type === 'finished') {
    assert.equal(finished.success, false)
    assert.equal(finished.exitCode, 2)
  }
})
