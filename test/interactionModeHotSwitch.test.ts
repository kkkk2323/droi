import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, chmod, mkdir } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DroidJsonRpcManager } from '../src/backend/droid/jsonrpc/droidJsonRpcManager.ts'

async function waitFor<T>(fn: () => T | undefined, timeoutMs = 3000): Promise<T> {
  const startedAt = Date.now()
  while (Date.now() - startedAt < timeoutMs) {
    const v = fn()
    if (v !== undefined) return v
    await new Promise((r) => setTimeout(r, 10))
  }
  throw new Error('Timed out waiting for condition')
}

test(
  'interactionMode switch uses update_session_settings (no re-init)',
  { skip: process.platform === 'win32' },
  async () => {
    const baseDir = await mkdtemp(join(tmpdir(), 'droi-hot-switch-'))
    const projectDir = join(baseDir, 'project')
    await mkdir(projectDir, { recursive: true })

    const fakeDroidPath = join(baseDir, 'fake-droid')
    const fakeDroid = `#!/usr/bin/env node
const { createInterface } = require('node:readline')
const { randomUUID } = require('node:crypto')

const JSONRPC_VERSION = '2.0'
const FACTORY_API_VERSION = '1.0.0'

function send(obj) {
  process.stdout.write(JSON.stringify(obj) + '\\n')
}

function sendResponse(id, result, error) {
  const msg = {
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'response',
    id,
    ...(error ? { error } : { result }),
  }
  send(msg)
}

function sendIdleNotification() {
  send({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'notification',
    method: 'droid.session_notification',
    params: { notification: { type: 'working_state_changed', newState: 'idle' } },
  })
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity })
rl.on('line', (line) => {
  let msg
  try { msg = JSON.parse(line) } catch { return }
  if (!msg || msg.type !== 'request' || typeof msg.method !== 'string') return

  if (msg.method === 'droid.initialize_session') {
    const mode = (msg.params && msg.params.interactionMode) || ''
    process.stdout.write('init interactionMode=' + mode + '\\n')
    sendResponse(msg.id, { sessionId: randomUUID() })
    return
  }

  if (msg.method === 'droid.load_session') {
    sendResponse(msg.id, { ok: true })
    return
  }

  if (msg.method === 'droid.update_session_settings') {
    sendResponse(msg.id, { ok: true })
    return
  }

  if (msg.method === 'droid.add_user_message') {
    sendResponse(msg.id, { ok: true })
    sendIdleNotification()
    return
  }

  if (msg.method === 'droid.interrupt_session') {
    sendResponse(msg.id, { ok: true })
    return
  }

  sendResponse(msg.id, null, { code: -32601, message: 'Method not found' })
})
`

    await writeFile(fakeDroidPath, fakeDroid, 'utf-8')
    await chmod(fakeDroidPath, 0o755)

    const events: any[] = []
    const manager = new DroidJsonRpcManager({
      droidPath: fakeDroidPath,
      emit: (ev) => {
        events.push(ev)
      },
    })

    try {
      await manager.sendUserMessage({
        sessionId: 'local-session',
        cwd: projectDir,
        machineId: 'm-test',
        prompt: 'hello',
        modelId: 'gpt-5.2',
        interactionMode: 'auto',
        autonomyLevel: 'low',
        env: {},
      })

      const replaced = await waitFor(
        () =>
          events.find(
            (e) => e.type === 'session-id-replaced' && e.oldSessionId === 'local-session',
          ),
        5000,
      ).catch((err) => {
        const debug = events
          .filter((e) => e.type === 'debug')
          .slice(-30)
          .map((e) => String(e.message || ''))
          .join('\n')
        const errors = events
          .filter((e) => e.type === 'error')
          .slice(-10)
          .map((e) => String(e.message || ''))
          .join('\n')
        const stderr = events
          .filter((e) => e.type === 'stderr')
          .slice(-10)
          .map((e) => String(e.data || ''))
          .join('\n')
        throw new Error(`${(err as Error).message}\n\nlast debug:\n${debug}\n\nlast errors:\n${errors}\n\nlast stderr:\n${stderr}`)
      })
      const engineSessionId = String(replaced.newSessionId || '')
      assert.ok(engineSessionId)

      const initReqCount = () =>
        events.filter(
          (e) => e.type === 'debug' && String(e.message || '').startsWith('request: droid.initialize_session'),
        ).length

      await waitFor(() => (initReqCount() >= 1 ? true : undefined))

      const turnEndCount = () =>
        events.filter((e) => e.type === 'turn-end' && e.sessionId === engineSessionId).length

      await waitFor(() => (turnEndCount() >= 1 ? true : undefined))

      await manager.sendUserMessage({
        sessionId: engineSessionId,
        resumeSessionId: engineSessionId,
        cwd: projectDir,
        machineId: 'm-test',
        prompt: 'second',
        modelId: 'gpt-5.2',
        interactionMode: 'spec',
        autonomyLevel: 'off',
        env: {},
      })

      await waitFor(
        () =>
          events.find(
            (e) =>
              e.type === 'debug' &&
              String(e.message || '').startsWith('request: droid.update_session_settings') &&
              String(e.message || '').includes('"interactionMode":"spec"'),
          ),
        5000,
      ).catch((err) => {
        const debug = events
          .filter((e) => e.type === 'debug')
          .slice(-40)
          .map((e) => String(e.message || ''))
          .join('\n')
        throw new Error(`${(err as Error).message}\n\nlast debug:\n${debug}`)
      })

      assert.equal(initReqCount(), 1)
    } finally {
      manager.disposeAllSessions()
    }
  },
)

test(
  'mission session plumbing preserves agi/orchestrator settings and supports kill_worker_session',
  { skip: process.platform === 'win32' },
  async () => {
    const baseDir = await mkdtemp(join(tmpdir(), 'droi-mission-plumbing-'))
    const projectDir = join(baseDir, 'project')
    await mkdir(projectDir, { recursive: true })

    const fakeDroidPath = join(baseDir, 'fake-droid')
    const fakeDroid = `#!/usr/bin/env node
const { createInterface } = require('node:readline')
const { randomUUID } = require('node:crypto')

const JSONRPC_VERSION = '2.0'
const FACTORY_API_VERSION = '1.0.0'

function send(obj) {
  process.stdout.write(JSON.stringify(obj) + '\\n')
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

function sendIdleNotification() {
  send({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'notification',
    method: 'droid.session_notification',
    params: { notification: { type: 'working_state_changed', newState: 'idle' } },
  })
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity })
rl.on('line', (line) => {
  let msg
  try { msg = JSON.parse(line) } catch { return }
  if (!msg || msg.type !== 'request' || typeof msg.method !== 'string') return

  if (msg.method === 'droid.initialize_session') {
    sendResponse(msg.id, { sessionId: randomUUID() })
    return
  }

  if (msg.method === 'droid.load_session') {
    sendResponse(msg.id, { ok: true })
    return
  }

  if (
    msg.method === 'droid.update_session_settings' ||
    msg.method === 'droid.add_user_message' ||
    msg.method === 'droid.kill_worker_session' ||
    msg.method === 'droid.interrupt_session'
  ) {
    sendResponse(msg.id, { ok: true })
    if (msg.method === 'droid.add_user_message') sendIdleNotification()
    return
  }

  sendResponse(msg.id, null, { code: -32601, message: 'Method not found' })
})
`

    await writeFile(fakeDroidPath, fakeDroid, 'utf-8')
    await chmod(fakeDroidPath, 0o755)

    const events: any[] = []
    const manager = new DroidJsonRpcManager({
      droidPath: fakeDroidPath,
      emit: (ev) => {
        events.push(ev)
      },
    })

    const findDebug = (needle: string) =>
      events.find(
        (e) => e.type === 'debug' && String(e.message || '').includes(needle),
      )

    const countDebug = (needle: string) =>
      events.filter(
        (e) => e.type === 'debug' && String(e.message || '').includes(needle),
      ).length

    try {
      await manager.sendUserMessage({
        sessionId: 'mission-local',
        cwd: projectDir,
        machineId: 'm-test',
        prompt: 'start mission',
        modelId: 'gpt-5.2',
        interactionMode: 'agi' as any,
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
        isMission: true,
        sessionKind: 'mission',
        env: {},
      })

      const replaced = await waitFor(
        () =>
          events.find(
            (e) => e.type === 'session-id-replaced' && e.oldSessionId === 'mission-local',
          ),
        5000,
      )
      const engineSessionId = String(replaced.newSessionId || '')
      assert.ok(engineSessionId)

      await waitFor(
        () =>
          findDebug('request: droid.initialize_session') &&
          findDebug('"interactionMode":"agi"') &&
          findDebug('"decompSessionType":"orchestrator"')
            ? true
            : undefined,
        5000,
      )

      await waitFor(
        () =>
          findDebug('request: droid.update_session_settings') &&
          countDebug('request: droid.update_session_settings') >= 1
            ? true
            : undefined,
        5000,
      )

      const missionUpdateRequestsAfterFirstSend = events.filter(
        (e) =>
          e.type === 'debug' &&
          String(e.message || '').startsWith('request: droid.update_session_settings'),
      )
      assert.ok(
        missionUpdateRequestsAfterFirstSend.some((e) =>
          String(e.message || '').includes('"interactionMode":"agi"'),
        ),
      )
      assert.ok(
        missionUpdateRequestsAfterFirstSend.some((e) =>
          String(e.message || '').includes('"autonomyLevel":"high"'),
        ),
      )

      await manager.updateSessionSettings({
        sessionId: engineSessionId,
        interactionMode: 'spec',
        autonomyLevel: 'off',
      })

      await manager.sendUserMessage({
        sessionId: engineSessionId,
        resumeSessionId: engineSessionId,
        cwd: projectDir,
        machineId: 'm-test',
        prompt: 'continue mission',
        modelId: 'gpt-5.2',
        interactionMode: 'spec',
        autonomyLevel: 'off',
        env: {},
      })

      await waitFor(
        () =>
          countDebug('request: droid.update_session_settings') >= 3 ? true : undefined,
        5000,
      )

      const updateRequests = events.filter(
        (e) =>
          e.type === 'debug' &&
          String(e.message || '').startsWith('request: droid.update_session_settings'),
      )
      assert.equal(updateRequests.length, 3)
      for (const request of updateRequests) {
        const message = String(request.message || '')
        assert.match(message, /"interactionMode":"agi"/)
        assert.match(message, /"autonomyLevel":"high"/)
        assert.doesNotMatch(message, /"interactionMode":"spec"/)
        assert.doesNotMatch(message, /"autonomyLevel":"off"/)
      }

      await manager.killWorkerSession({
        sessionId: engineSessionId,
        workerSessionId: 'worker-123',
      })

      await waitFor(
        () => (findDebug('request: droid.kill_worker_session') ? true : undefined),
        5000,
      )
      assert.ok(findDebug('"workerSessionId":"worker-123"'))
    } finally {
      manager.disposeAllSessions()
    }
  },
)

test(
  'mission continuation via normal chat reuses the same mission session without a separate resume RPC',
  { skip: process.platform === 'win32' },
  async () => {
    const baseDir = await mkdtemp(join(tmpdir(), 'droi-mission-continue-'))
    const projectDir = join(baseDir, 'project')
    await mkdir(projectDir, { recursive: true })

    const fakeDroidPath = join(baseDir, 'fake-droid')
    const fakeDroid = `#!/usr/bin/env node
const { createInterface } = require('node:readline')
const { randomUUID } = require('node:crypto')

const JSONRPC_VERSION = '2.0'
const FACTORY_API_VERSION = '1.0.0'

function send(obj) {
  process.stdout.write(JSON.stringify(obj) + '\\n')
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

function sendIdleNotification() {
  send({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'notification',
    method: 'droid.session_notification',
    params: { notification: { type: 'working_state_changed', newState: 'idle' } },
  })
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity })
rl.on('line', (line) => {
  let msg
  try { msg = JSON.parse(line) } catch { return }
  if (!msg || msg.type !== 'request' || typeof msg.method !== 'string') return

  if (msg.method === 'droid.initialize_session') {
    sendResponse(msg.id, { sessionId: randomUUID() })
    return
  }

  if (msg.method === 'droid.load_session') {
    sendResponse(msg.id, { ok: true })
    return
  }

  if (msg.method === 'droid.update_session_settings' || msg.method === 'droid.add_user_message') {
    sendResponse(msg.id, { ok: true })
    if (msg.method === 'droid.add_user_message') sendIdleNotification()
    return
  }

  sendResponse(msg.id, null, { code: -32601, message: 'Method not found' })
})
`

    await writeFile(fakeDroidPath, fakeDroid, 'utf-8')
    await chmod(fakeDroidPath, 0o755)

    const events: any[] = []
    const manager = new DroidJsonRpcManager({
      droidPath: fakeDroidPath,
      emit: (ev) => {
        events.push(ev)
      },
    })

    try {
      await manager.sendUserMessage({
        sessionId: 'mission-local',
        cwd: projectDir,
        machineId: 'm-test',
        prompt: 'start mission',
        modelId: 'gpt-5.2',
        interactionMode: 'agi' as any,
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
        isMission: true,
        sessionKind: 'mission',
        env: {},
      })

      const replaced = await waitFor(
        () =>
          events.find(
            (e) => e.type === 'session-id-replaced' && e.oldSessionId === 'mission-local',
          ),
        5000,
      )
      const engineSessionId = String(replaced.newSessionId || '')
      assert.ok(engineSessionId)

      const turnEndCount = () =>
        events.filter((e) => e.type === 'turn-end' && e.sessionId === engineSessionId).length

      await waitFor(() => (turnEndCount() >= 1 ? true : undefined))

      await manager.sendUserMessage({
        sessionId: engineSessionId,
        resumeSessionId: engineSessionId,
        cwd: projectDir,
        machineId: 'm-test',
        prompt: 'continue mission',
        modelId: 'gpt-5.2',
        interactionMode: 'spec',
        autonomyLevel: 'off',
        env: {},
      })

      await waitFor(() => (turnEndCount() >= 2 ? true : undefined))

      const sessionIdReplacements = events.filter((e) => e.type === 'session-id-replaced')
      assert.equal(sessionIdReplacements.length, 1)
      assert.equal(sessionIdReplacements[0]?.newSessionId, engineSessionId)

      const loadRequests = events.filter(
        (e) =>
          e.type === 'debug' &&
          String(e.message || '').startsWith('request: droid.load_session'),
      )
      assert.equal(loadRequests.length, 0)

      const addUserMessageStages = events.filter(
        (e) => e.type === 'debug' && e.message === 'sendUserMessage: addUserMessage start',
      )
      assert.equal(addUserMessageStages.length, 2)
      for (const event of addUserMessageStages) {
        assert.equal(event.sessionId, engineSessionId)
      }

      const addUserMessageRequests = events.filter(
        (e) =>
          e.type === 'debug' &&
          String(e.message || '').startsWith('request: droid.add_user_message'),
      )
      assert.equal(addUserMessageRequests.length, 2)
      assert.match(String(addUserMessageRequests[1]?.message || ''), /"text":"continue mission"/)

      const updateRequests = events.filter(
        (e) =>
          e.type === 'debug' &&
          String(e.message || '').startsWith('request: droid.update_session_settings'),
      )
      assert.equal(updateRequests.length, 2)
      for (const request of updateRequests) {
        const message = String(request.message || '')
        assert.match(message, /"interactionMode":"agi"/)
        assert.match(message, /"autonomyLevel":"high"/)
        assert.doesNotMatch(message, /"interactionMode":"spec"/)
        assert.doesNotMatch(message, /"autonomyLevel":"off"/)
      }
    } finally {
      manager.disposeAllSessions()
    }
  },
)
