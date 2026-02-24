import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { SetupScriptRunner } from '../src/backend/session/setupScriptRunner.ts'
import { LocalDiagnostics } from '../src/backend/diagnostics/localDiagnostics.ts'
import { createHonoApp } from '../src/server/hono/app.ts'
import type { HonoAppDeps } from '../src/server/hono/types.ts'
import type { PersistedAppState } from '../src/shared/protocol'

function createTestApp(params?: {
  port?: number
  pairingWebPort?: number
  state?: PersistedAppState
  execManager?: Partial<HonoAppDeps['execManager']>
}) {
  const state = params?.state || { version: 2, machineId: 'm-test' }
  const cachedStateRef = { value: state }

  const execManager = Object.assign({
    onEvent: () => () => {},
    hasSession: () => false,
    createSession: async () => ({ sessionId: 's-new' }),
    send: async () => {},
    respondPermission: () => {},
    respondAskUser: () => {},
    cancel: () => {},
    disposeSession: () => {},
    disposeAllSessions: () => false,
  }, params?.execManager || {}) as HonoAppDeps['execManager']

  const deps: HonoAppDeps = {
    opts: {
      host: '127.0.0.1',
      port: params?.port ?? 3001,
      webRootDir: null,
      pairingWebPort: params?.pairingWebPort,
    },
    runtimePortRef: { value: params?.port ?? 3001 },
    cachedStateRef,
    appStateStore: {
      load: async () => cachedStateRef.value,
      save: async () => {},
      update: async () => cachedStateRef.value,
      filePath: '',
    } as any,
    sessionStore: {
      save: async () => null,
      load: async () => null,
      list: async () => [],
      delete: async () => false,
      clearContext: async () => null,
      replaceSessionId: async () => null,
      sessionsDir: '',
    } as any,
    execManager,
    setupScriptRunner: new SetupScriptRunner(),
    diagnostics: new LocalDiagnostics({ baseDir: join(tmpdir(), 'droi-test-diag'), enabled: false }),
    keyStore: {
      getKeys: async () => [],
      addKeys: async () => ({ added: 0, duplicates: 0 }),
      removeKey: async () => {},
      updateNote: async () => {},
      getUsages: async () => new Map(),
      refreshUsages: async () => new Map(),
      getActiveKey: async () => state.apiKey || null,
    } as any,
  }

  return createHonoApp(deps)
}

test('mobile pairing endpoint returns connectUrl and removed routes return 404', async () => {
  const app = createTestApp({ port: 3001, pairingWebPort: 5173 })

  const pairRes = await app.request('http://localhost/mobile/pair', { method: 'POST' })
  assert.equal(pairRes.status, 200)

  const pairData = await pairRes.json() as { connectUrl?: unknown }
  assert.equal(typeof pairData.connectUrl, 'string')
  assert.ok(String(pairData.connectUrl).startsWith('http://'))
  assert.ok(String(pairData.connectUrl).endsWith(':5173/'))
  assert.equal(String(pairData.connectUrl).includes('token='), false)

  const removed = [
    { method: 'POST', path: '/mobile/verify', body: JSON.stringify({ token: 'x' }) },
    { method: 'GET', path: '/mobile/connections' },
    { method: 'POST', path: '/mobile/disconnect', body: JSON.stringify({ sessionToken: 'x' }) },
  ] as const

  for (const route of removed) {
    const res = await app.request(`http://localhost${route.path}`, {
      method: route.method,
      headers: route.body ? { 'Content-Type': 'application/json' } : undefined,
      body: route.body,
    })
    assert.equal(res.status, 404)
  }
})

test('upload and file endpoints stream files correctly', async () => {
  const baseDir = await mkdtemp(join(tmpdir(), 'droid-server-file-'))
  const projectDir = join(baseDir, 'project')
  await mkdir(projectDir, { recursive: true })

  const app = createTestApp()

  const fd = new FormData()
  fd.append('file', new Blob(['hello-first'], { type: 'text/plain' }), 'first.txt')
  fd.append('file', new Blob([new Uint8Array([1, 2, 3, 4])], { type: 'application/octet-stream' }), 'second.bin')

  const uploadRes = await app.request(`http://localhost/api/upload?projectDir=${encodeURIComponent(projectDir)}`, {
    method: 'POST',
    body: fd,
  })
  assert.equal(uploadRes.status, 200)

  const uploaded = await uploadRes.json() as Array<{ name: string; path: string }>
  assert.equal(uploaded.length, 2)

  const uploadedByName = new Map(uploaded.map((item) => [item.name, item.path]))
  const firstPath = uploadedByName.get('first.txt')
  const secondPath = uploadedByName.get('second.bin')

  assert.ok(firstPath)
  assert.ok(secondPath)
  assert.ok(firstPath!.includes('/.attachment/'))
  assert.ok(secondPath!.includes('/.attachment/'))

  const firstSaved = await readFile(firstPath!, 'utf-8')
  assert.equal(firstSaved, 'hello-first')

  const secondSaved = await readFile(secondPath!)
  assert.deepEqual(Array.from(secondSaved), [1, 2, 3, 4])

  const firstDownload = await app.request(`http://localhost/api/file?path=${encodeURIComponent(firstPath!)}`)
  assert.equal(firstDownload.status, 200)
  assert.equal(await firstDownload.text(), 'hello-first')

  const jsonFile = join(projectDir, 'sample.json')
  await writeFile(jsonFile, JSON.stringify({ ok: true }), 'utf-8')

  const jsonDownload = await app.request(`http://localhost/api/file?path=${encodeURIComponent(jsonFile)}`)
  assert.equal(jsonDownload.status, 200)
  assert.match(jsonDownload.headers.get('content-type') || '', /application\/json/)
  assert.equal(await jsonDownload.text(), '{"ok":true}')
})

test('POST /api/message calls execManager.send with cwd/machineId/sessionId/prompt', async () => {
  const baseDir = await mkdtemp(join(tmpdir(), 'droid-server-message-'))
  const projectDir = join(baseDir, 'project')
  await mkdir(projectDir, { recursive: true })

  const sendCalls: any[] = []
  const app = createTestApp({
    state: { version: 2, machineId: 'm-test', activeProjectDir: projectDir },
    execManager: {
      send: async (params: any) => { sendCalls.push(params) },
    },
  })

  const res = await app.request('http://localhost/api/message', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      prompt: 'hello',
      sessionId: 's1',
      modelId: 'gpt-5.2',
      autoLevel: 'low',
      reasoningEffort: 'low',
    }),
  })
  assert.equal(res.status, 200)
  const data = await res.json() as any
  assert.equal(data.ok, true)
  assert.equal(sendCalls.length, 1)
  assert.equal(sendCalls[0].sessionId, 's1')
  assert.equal(sendCalls[0].prompt, 'hello')
  assert.equal(sendCalls[0].cwd, projectDir)
  assert.equal(sendCalls[0].machineId, 'm-test')
  assert.equal(sendCalls[0].interactionMode, 'auto')
  assert.equal(sendCalls[0].autonomyLevel, 'low')
})

test('POST /api/message maps autoLevel=default to interactionMode=spec and autonomyLevel=off', async () => {
  const baseDir = await mkdtemp(join(tmpdir(), 'droid-server-message-spec-'))
  const projectDir = join(baseDir, 'project')
  await mkdir(projectDir, { recursive: true })

  const sendCalls: any[] = []
  const app = createTestApp({
    state: { version: 2, machineId: 'm-test', activeProjectDir: projectDir },
    execManager: {
      send: async (params: any) => {
        sendCalls.push(params)
      },
    },
  })

  const res = await app.request('http://localhost/api/message', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      prompt: 'hello',
      sessionId: 's1',
      modelId: 'gpt-5.2',
      autoLevel: 'default',
      reasoningEffort: 'low',
    }),
  })

  assert.equal(res.status, 200)
  const data = (await res.json()) as any
  assert.equal(data.ok, true)
  assert.equal(sendCalls.length, 1)
  assert.equal(sendCalls[0].interactionMode, 'spec')
  assert.equal(sendCalls[0].autonomyLevel, 'off')
})

test('GET /api/stream returns event-stream and writes ok prelude', async () => {
  const app = createTestApp()
  const res = await app.request('http://localhost/api/stream?sessionId=s1', { method: 'GET' })
  assert.equal(res.status, 200)
  assert.match(res.headers.get('content-type') || '', /text\/event-stream/)
  assert.ok(res.body)
  const reader = res.body!.getReader()
  const chunk = await reader.read()
  const text = new TextDecoder().decode(chunk.value || new Uint8Array())
  assert.match(text, /:ok/)
  await reader.cancel()
})

test('session setup run/cancel endpoints work', async () => {
  const projectDir = await mkdtemp(join(tmpdir(), 'droid-setup-run-'))
  const app = createTestApp()

  const runRes = await app.request('http://localhost/api/session/setup/run', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId: 's-setup', projectDir, script: 'echo setup' }),
  })
  assert.equal(runRes.status, 200)
  const runData = await runRes.json() as any
  assert.equal(runData.ok, true)

  const cancelRes = await app.request('http://localhost/api/session/setup/cancel', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId: 's-setup' }),
  })
  assert.equal(cancelRes.status, 200)
  const cancelData = await cancelRes.json() as any
  assert.equal(cancelData.ok, true)
})
