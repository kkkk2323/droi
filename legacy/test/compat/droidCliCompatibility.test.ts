import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import crypto from 'node:crypto'
import { mkdtemp, writeFile, chmod } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

type Platform = 'darwin' | 'linux'
type FetchText = (url: string) => Promise<string>
type FetchBytes = (url: string) => Promise<Uint8Array>

function getPlatform(): Platform | null {
  if (process.platform === 'darwin') return 'darwin'
  if (process.platform === 'linux') return 'linux'
  return null
}

function getArchCandidates(): string[] {
  if (process.arch === 'arm64') return ['arm64']
  if (process.arch === 'x64') return ['x64', 'x64-baseline']
  return []
}

async function defaultFetchText(url: string): Promise<string> {
  if (typeof (globalThis as any).fetch !== 'function') throw new Error('global fetch() is not available in this Node runtime')
  const res = await fetch(url)
  if (!res.ok) throw new Error(`HTTP ${res.status} for ${url}`)
  return await res.text()
}

async function defaultFetchBytes(url: string): Promise<Uint8Array> {
  if (typeof (globalThis as any).fetch !== 'function') throw new Error('global fetch() is not available in this Node runtime')
  const res = await fetch(url)
  if (!res.ok) throw new Error(`HTTP ${res.status} for ${url}`)
  return new Uint8Array(await res.arrayBuffer())
}

function sha256Hex(bytes: Uint8Array): string {
  return crypto.createHash('sha256').update(bytes).digest('hex')
}

function parseSha256Text(text: string): string {
  return text.trim().split(/\s+/)[0]
}

async function downloadDroidCli(opts: {
  version: string
  baseUrl?: string
  platform?: Platform | null
  archCandidates?: string[]
  fetchText?: FetchText
  fetchBytes?: FetchBytes
}): Promise<{ binPath: string; cleanupDir: string } > {
  const platform = opts.platform === undefined ? getPlatform() : opts.platform
  if (!platform) throw new Error(`Unsupported platform for compatibility tests: ${process.platform}`)
  const archCandidates = opts.archCandidates === undefined ? getArchCandidates() : opts.archCandidates
  if (archCandidates.length === 0) throw new Error(`Unsupported architecture for compatibility tests: ${process.arch}`)

  const baseUrl = opts.baseUrl || process.env.FACTORY_DOWNLOADS_BASE_URL || 'https://downloads.factory.ai'
  const fetchText = opts.fetchText || defaultFetchText
  const fetchBytes = opts.fetchBytes || defaultFetchBytes

  const tmp = await mkdtemp(join(tmpdir(), 'droi-droid-cli-'))
  const binPath = join(tmp, 'droid')

  let lastErr: unknown = null
  for (const droidArch of archCandidates) {
    const url = `${baseUrl}/factory-cli/releases/${opts.version}/${platform}/${droidArch}/droid`
    const shaUrl = `${url}.sha256`
    try {
      const bytes = await fetchBytes(url)
      const expectedSha = parseSha256Text(await fetchText(shaUrl))
      const actualSha = sha256Hex(bytes)
      assert.ok(expectedSha && /^[a-f0-9]{64}$/i.test(expectedSha), `Invalid sha256 content from ${shaUrl}`)
      assert.equal(actualSha, expectedSha)

      await writeFile(binPath, bytes)
      await chmod(binPath, 0o755)
      return { binPath, cleanupDir: tmp }
    } catch (err) {
      lastErr = err
    }
  }

  throw new Error(`Failed to download Factory CLI v${opts.version} for ${platform}/${process.arch}: ${String((lastErr as any)?.message || lastErr)}`)
}

const downloadCache = new Map<string, Promise<{ binPath: string; cleanupDir: string }>>()
function downloadDroidCliCached(version: string): Promise<{ binPath: string; cleanupDir: string }> {
  const baseUrl = process.env.FACTORY_DOWNLOADS_BASE_URL || 'https://downloads.factory.ai'
  const key = `${baseUrl}|${version}|${process.platform}|${process.arch}`
  const existing = downloadCache.get(key)
  if (existing) return existing
  const p = downloadDroidCli({ version, baseUrl })
  downloadCache.set(key, p)
  return p
}

async function run(binPath: string, args: string[], opts?: { cwd?: string; env?: Record<string, string | undefined>; timeoutMs?: number }): Promise<{ code: number | null; stdout: string; stderr: string }> {
  const timeoutMs = opts?.timeoutMs ?? 60_000
  const child = spawn(binPath, args, {
    cwd: opts?.cwd,
    env: { ...process.env, ...(opts?.env || {}) },
    stdio: ['ignore', 'pipe', 'pipe'],
  })

  let stdout = ''
  let stderr = ''
  child.stdout.on('data', (b) => { stdout += b.toString() })
  child.stderr.on('data', (b) => { stderr += b.toString() })

  const code = await new Promise<number | null>((resolve, reject) => {
    const t = setTimeout(() => {
      try { child.kill('SIGKILL') } catch { /* ignore */ }
      reject(new Error(`timeout after ${timeoutMs}ms`))
    }, timeoutMs)
    child.on('close', (c) => {
      clearTimeout(t)
      resolve(typeof c === 'number' ? c : null)
    })
    child.on('error', (err) => {
      clearTimeout(t)
      reject(err)
    })
  })

  return { code, stdout, stderr }
}

function requireApiKey(env: Record<string, string | undefined> = process.env): string {
  const v = String(env.FACTORY_API_KEY || '').trim()
  if (!v) throw new Error('FACTORY_API_KEY is required for compatibility tests')
  return v
}

test('parseSha256Text extracts the hash from common formats', () => {
  const hash = 'a'.repeat(64)
  assert.equal(parseSha256Text(`${hash}  droid`), hash)
  assert.equal(parseSha256Text(`${hash}\tdroid`), hash)
  assert.equal(parseSha256Text(`\n${hash}\n`), hash)
})

test('downloadDroidCli tries arch candidates in order (falls back on failures)', { timeout: 30_000 }, async () => {
  const bytes = new TextEncoder().encode('hello')
  const sha = sha256Hex(bytes)

  const seen: string[] = []
  const baseUrl = 'https://downloads.factory.ai'
  const version = '0.0.0-test'

  const fetchBytes: FetchBytes = async (url) => {
    seen.push(url)
    if (url.includes('/x64/droid')) throw new Error('simulated 404')
    if (url.includes('/x64-baseline/droid')) return bytes
    throw new Error(`unexpected url: ${url}`)
  }
  const fetchText: FetchText = async (url) => {
    seen.push(url)
    if (url.endsWith('/x64-baseline/droid.sha256')) return `${sha}  droid\n`
    if (url.endsWith('/x64/droid.sha256')) return `${sha}  droid\n`
    throw new Error(`unexpected url: ${url}`)
  }

  const res = await downloadDroidCli({
    version,
    baseUrl,
    platform: 'darwin',
    archCandidates: ['x64', 'x64-baseline'],
    fetchBytes,
    fetchText,
  })

  assert.ok(res.binPath.endsWith('/droid'))
  assert.ok(seen.some((u) => u.includes('/x64/droid')))
  assert.ok(seen.some((u) => u.includes('/x64-baseline/droid')))
})

test('downloadDroidCli rejects on invalid sha256 content', { timeout: 30_000 }, async () => {
  const bytes = new TextEncoder().encode('hello')
  const fetchBytes: FetchBytes = async () => bytes
  const fetchText: FetchText = async () => 'not-a-sha\n'

  await assert.rejects(
    downloadDroidCli({
      version: '0.0.0-test',
      baseUrl: 'https://downloads.factory.ai',
      platform: 'darwin',
      archCandidates: ['x64'],
      fetchBytes,
      fetchText,
    }),
    /Invalid sha256 content/
  )
})

test('downloadDroidCli rejects on sha256 mismatch', { timeout: 30_000 }, async () => {
  const bytes = new TextEncoder().encode('hello')
  const fetchBytes: FetchBytes = async () => bytes
  const fetchText: FetchText = async () => `${'b'.repeat(64)}\n`

  await assert.rejects(
    downloadDroidCli({
      version: '0.0.0-test',
      baseUrl: 'https://downloads.factory.ai',
      platform: 'darwin',
      archCandidates: ['x64'],
      fetchBytes,
      fetchText,
    }),
    /Expected values to be strictly equal/
  )
})

test('downloadDroidCli rejects when platform/arch are unsupported', { timeout: 30_000 }, async () => {
  await assert.rejects(downloadDroidCli({ version: '0.0.0-test', platform: null, archCandidates: ['x64'] }), /Unsupported platform/)
  await assert.rejects(downloadDroidCli({ version: '0.0.0-test', platform: 'darwin', archCandidates: [] }), /Unsupported architecture/)
})

test('requireApiKey throws when missing', () => {
  assert.throws(() => requireApiKey({}), /FACTORY_API_KEY is required/)
})

test('run captures stdout/stderr and exit code', { timeout: 30_000 }, async () => {
  const res = await run(process.execPath, ['-e', 'console.log("ok"); console.error("err")'], { timeoutMs: 10_000 })
  assert.equal(res.code, 0)
  assert.match(res.stdout, /ok/)
  assert.match(res.stderr, /err/)
})

test('run rejects on timeout', { timeout: 30_000 }, async () => {
  await assert.rejects(
    run(process.execPath, ['-e', 'setTimeout(() => {}, 1e9)'], { timeoutMs: 50 }),
    /timeout after/
  )
})

test('Factory CLI binary can be downloaded and has a version string', { timeout: 120_000 }, async () => {
  const version = String(process.env.FACTORY_DROID_CLI_VERSION || '0.57.14').trim()
  const { binPath, cleanupDir } = await downloadDroidCliCached(version)
  try {
    const res = await run(binPath, ['--version'], { timeoutMs: 30_000 })
    assert.equal(res.code, 0)
    assert.ok(res.stdout.trim() || res.stderr.trim(), 'Expected --version to produce output')
  } finally {
    // tmpdir cleanup is handled by OS; keeping it makes debugging easier.
    void cleanupDir
  }
})

test(
  'JSON-RPC initialize_session respects autonomyLevel=spec (regression test)',
  { timeout: 180_000, skip: !String(process.env.FACTORY_API_KEY || '').trim() },
  async () => {
  requireApiKey()
  const version = String(process.env.FACTORY_DROID_CLI_VERSION || '0.57.14').trim()
  const { binPath } = await downloadDroidCliCached(version)

  const cwd = await mkdtemp(join(tmpdir(), 'droi-jsonrpc-'))
  const modelId = String(process.env.FACTORY_MODEL_ID || 'minimax-m2.5').trim()

  const proc = spawn(binPath, ['exec', '--input-format', 'stream-jsonrpc', '--output-format', 'stream-jsonrpc', '--cwd', cwd], {
    cwd,
    env: { ...process.env, FACTORY_API_KEY: process.env.FACTORY_API_KEY },
    stdio: ['pipe', 'pipe', 'pipe'],
  })

  const timeoutMs = 60_000
  const initReq = {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    type: 'request',
    id: '1',
    method: 'droid.initialize_session',
    params: {
      machineId: 'droi-compat-test',
      cwd,
      modelId,
      autonomyLevel: 'spec',
      reasoningEffort: 'none',
    },
  }

  let stderr = ''
  proc.stderr.on('data', (b) => { stderr += b.toString() })

  proc.stdin.write(`${JSON.stringify(initReq)}\n`)
  proc.stdin.end()

  const res = await new Promise<any>((resolve, reject) => {
    let settled = false
    const finish = (fn: () => void) => {
      if (settled) return
      settled = true
      fn()
    }

    const t = setTimeout(() => {
      finish(() => {
        try { proc.kill('SIGKILL') } catch { /* ignore */ }
        reject(new Error(`timeout after ${timeoutMs}ms; stderr=${stderr.trim().slice(0, 2000)}`))
      })
    }, timeoutMs)

    let buf = ''
    proc.stdout.on('data', (b) => {
      buf += b.toString()
      const lines = buf.split(/\r?\n/)
      buf = lines.pop() || ''

      for (const line of lines) {
        if (!line) continue
        try {
          const msg = JSON.parse(line)
          if (msg?.type === 'response' && msg?.id === '1') {
            finish(() => {
              clearTimeout(t)
              try { proc.kill('SIGTERM') } catch { /* ignore */ }
              resolve(msg)
            })
            return
          }
        } catch {
          // ignore non-JSON lines
        }
      }
    })

    proc.on('close', () => {
      finish(() => {
        clearTimeout(t)
        reject(new Error(`droid exited without initialize_session response; stderr=${stderr.trim().slice(0, 2000)}`))
      })
    })
    proc.on('error', (err) => {
      finish(() => {
        clearTimeout(t)
        reject(err)
      })
    })
  })

  assert.ok(res?.result?.settings, `missing result.settings in response: ${JSON.stringify(res).slice(0, 2000)}`)
  assert.equal(String(res.result.settings.autonomyLevel || ''), 'spec')
})

type PermissionPolicy = 'cancel' | 'proceed_once'

async function startStreamJsonRpcDroid(opts: { binPath: string; cwd: string; env?: Record<string, string | undefined> }) {
  const proc = spawn(opts.binPath, ['exec', '--input-format', 'stream-jsonrpc', '--output-format', 'stream-jsonrpc', '--cwd', opts.cwd], {
    cwd: opts.cwd,
    env: { ...process.env, ...(opts.env || {}) },
    stdio: ['pipe', 'pipe', 'pipe'],
  })

  let permissionPolicy: PermissionPolicy = 'cancel'
  let permissionRequests = 0
  let currentState: string | null = null
  let sawActive = false
  let idleResolvers: Array<() => void> = []
  let stderr = ''
  proc.stderr.on('data', (b) => { stderr += b.toString() })

  const pending = new Map<string, { resolve: (msg: any) => void; reject: (err: Error) => void }>()
  let stdoutBuf = ''

  function writeJson(msg: any) {
    proc.stdin.write(`${JSON.stringify(msg)}\n`)
  }

  function onWorkingState(newState: string) {
    currentState = newState
    if (newState !== 'idle') sawActive = true
    if (sawActive && newState === 'idle') {
      const resolvers = idleResolvers
      idleResolvers = []
      for (const r of resolvers) r()
    }
  }

  function handleInbound(msg: any) {
    if (!msg || typeof msg !== 'object') return

    if (msg.type === 'response' && msg.id && pending.has(String(msg.id))) {
      pending.get(String(msg.id))!.resolve(msg)
      pending.delete(String(msg.id))
      return
    }

    if (msg.type === 'request' && msg.id && msg.method === 'droid.request_permission') {
      permissionRequests += 1
      const selectedOption = permissionPolicy

      writeJson({
        jsonrpc: '2.0',
        factoryApiVersion: '1.0.0',
        type: 'response',
        id: msg.id,
        result: { selectedOption },
      })
      return
    }

    if (msg.type === 'request' && msg.id && msg.method === 'droid.ask_user') {
      writeJson({
        jsonrpc: '2.0',
        factoryApiVersion: '1.0.0',
        type: 'response',
        id: msg.id,
        result: { cancelled: true, answers: [] },
      })
      return
    }

    if (msg.type === 'notification' && msg.method === 'droid.session_notification') {
      const n = msg?.params?.notification
      if (n?.type === 'droid_working_state_changed' && typeof n?.newState === 'string') {
        onWorkingState(n.newState)
      }
    }
  }

  proc.stdout.on('data', (b) => {
    stdoutBuf += b.toString()
    const lines = stdoutBuf.split(/\r?\n/)
    stdoutBuf = lines.pop() || ''
    for (const line of lines) {
      if (!line) continue
      try {
        handleInbound(JSON.parse(line))
      } catch {
        // ignore
      }
    }
  })

  proc.on('close', () => {
    const err = new Error(`droid exited unexpectedly; stderr=${stderr.trim().slice(0, 2000)}`)
    for (const { reject } of pending.values()) reject(err)
    pending.clear()
    const resolvers = idleResolvers
    idleResolvers = []
    for (const r of resolvers) r()
  })

  let nextId = 1
  async function request(method: string, params: any, timeoutMs = 60_000): Promise<any> {
    const id = String(nextId++)
    const msg = { jsonrpc: '2.0', factoryApiVersion: '1.0.0', type: 'request', id, method, params }

    const p = new Promise<any>((resolve, reject) => {
      pending.set(id, { resolve, reject })
    })

    writeJson(msg)

    const timeout = new Promise((_, reject) => {
      const t = setTimeout(() => {
        pending.delete(id)
        reject(new Error(`timeout waiting for response to ${method} (${id}); stderr=${stderr.trim().slice(0, 2000)}`))
      }, timeoutMs)
      p.finally(() => clearTimeout(t))
    })

    return await Promise.race([p, timeout])
  }

  async function waitForIdle(timeoutMs = 120_000): Promise<void> {
    if (sawActive && currentState === 'idle') return
    await new Promise<void>((resolve, reject) => {
      const t = setTimeout(() => reject(new Error(`timeout waiting for idle; stderr=${stderr.trim().slice(0, 2000)}`)), timeoutMs)
      idleResolvers.push(() => {
        clearTimeout(t)
        resolve()
      })
    })
  }

  return {
    proc,
    request,
    waitForIdle,
    setPermissionPolicy: (p: PermissionPolicy) => { permissionPolicy = p },
    resetPermissionCounters: () => { permissionRequests = 0 },
    getPermissionRequests: () => permissionRequests,
    getCurrentState: () => currentState,
    getStderr: () => stderr,
    kill: () => {
      try { proc.kill('SIGKILL') } catch { /* ignore */ }
    },
  }
}

test(
  'JSON-RPC update_session_settings applies autonomyLevel change (auto-high -> auto-low)',
  { timeout: 180_000, skip: !String(process.env.FACTORY_API_KEY || '').trim() },
  async () => {
    requireApiKey()
    const version = String(process.env.FACTORY_DROID_CLI_VERSION || '0.57.14').trim()
    const { binPath } = await downloadDroidCliCached(version)

    const cwd = await mkdtemp(join(tmpdir(), 'droi-jsonrpc-switch-'))
    const modelId = String(process.env.FACTORY_MODEL_ID || 'minimax-m2.5').trim()

    const sess = await startStreamJsonRpcDroid({ binPath, cwd, env: { FACTORY_API_KEY: process.env.FACTORY_API_KEY } })
    try {
      const init = await sess.request('droid.initialize_session', {
        machineId: 'droi-compat-test',
        cwd,
        modelId,
        autonomyLevel: 'auto-high',
        reasoningEffort: 'none',
      })

      const engineSessionId = String(init?.result?.sessionId || '').trim()
      assert.ok(engineSessionId, `initialize_session did not return sessionId: ${JSON.stringify(init).slice(0, 2000)}`)
      assert.equal(String(init?.result?.settings?.autonomyLevel || ''), 'auto-high')

      await sess.request('droid.update_session_settings', { autonomyLevel: 'auto-low' })

      const loaded = await sess.request('droid.load_session', { sessionId: engineSessionId })
      assert.equal(String(loaded?.result?.settings?.autonomyLevel || ''), 'auto-low')
    } finally {
      sess.kill()
    }
  }
)

test.skip('Task tool can launch a project droid (no Premature close)', { timeout: 240_000 }, async () => {
  requireApiKey()
  const version = String(process.env.FACTORY_DROID_CLI_VERSION || '0.57.14').trim()
  const { binPath } = await downloadDroidCliCached(version)

  const repoRoot = process.cwd()
  const prompt = [
    'Use the Task tool now.',
    'subagent_type: compat-worker',
    'description: compat test',
    'prompt: Reply with exactly COMPAT_OK',
    'Return only the subagent output.',
  ].join('\n')

  const out = await run(
    binPath,
    [
      'exec',
      '--auto', 'medium',
      '--cwd', repoRoot,
      '--enabled-tools', 'Task',
      '-m', String(process.env.FACTORY_MODEL_ID || 'minimax-m2.5').trim(),
      prompt,
    ],
    { cwd: repoRoot, timeoutMs: 180_000, env: { FACTORY_API_KEY: process.env.FACTORY_API_KEY } }
  )

  const combined = `${out.stdout}\n${out.stderr}`
  assert.equal(out.code, 0, combined.slice(0, 4000))
  assert.ok(!/Premature close/i.test(combined), combined.slice(0, 4000))
  assert.ok(/COMPAT_OK/.test(combined), combined.slice(0, 4000))
})
