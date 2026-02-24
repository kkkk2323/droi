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
  'interactionMode hot-switch re-initializes session with new mode',
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

      const initLines = await waitFor(
        () => {
          const lines = events
            .filter((e) => e.type === 'stdout')
            .map((e) => String(e.data || ''))
            .filter((l) => l.startsWith('init interactionMode='))
          const hasAuto = lines.some((l) => l.trim() === 'init interactionMode=auto')
          const hasSpec = lines.some((l) => l.trim() === 'init interactionMode=spec')
          return hasAuto && hasSpec ? lines : undefined
        },
        5000,
      ).catch((err) => {
        const debug = events
          .filter((e) => e.type === 'debug')
          .slice(-20)
          .map((e) => String(e.message || ''))
          .join('\n')
        const stdout = events
          .filter((e) => e.type === 'stdout')
          .slice(-20)
          .map((e) => String(e.data || ''))
          .join('\n')
        throw new Error(`${(err as Error).message}\n\nlast debug:\n${debug}\n\nlast stdout:\n${stdout}`)
      })

      assert.ok(initLines.length >= 2)
      const autoIndex = initLines.findIndex((l) => l.trim() === 'init interactionMode=auto')
      const specIndex = initLines.findIndex((l) => l.trim() === 'init interactionMode=spec')
      assert.ok(autoIndex >= 0)
      assert.ok(specIndex >= 0)
      assert.ok(specIndex > autoIndex)
    } finally {
      manager.disposeAllSessions()
    }
  },
)
