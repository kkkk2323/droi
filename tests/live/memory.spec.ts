// Memory against a real `droid daemon` (ADR 0010, 0011), started with Droi's
// Runtime Overlay and the built Memory Server and hook, the way the Desktop
// Shell starts it. Needs `pnpm build` first (out/main/memory-*.js) and runs
// only when FACTORY_API_KEY is set, like live.spec.ts:
//
//   pnpm build && FACTORY_API_KEY=fk-... pnpm test:live memory.spec.ts
import { expect, test } from '@playwright/test'
import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, readdirSync, realpathSync, rmSync } from 'node:fs'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  AutonomyLevel,
  connectToDaemon,
  DroidMessageType,
  type ConnectedDroid,
} from '@factory/droid-sdk'
import { GATEWAY_API_KEY_PLACEHOLDER, gatewayDaemonUrl } from '@droi/daemon-layer/gateway'
import { DaemonSupervisor } from '../../apps/desktop/src/main/daemon/daemon-supervisor'
import { daemonArgs } from '../../apps/desktop/src/main/daemon/daemon-args'
import { locateDroid } from '../../apps/desktop/src/main/daemon/locate-droid'
import { startGateway, type Gateway } from '../../apps/desktop/src/main/gateway/gateway'
import {
  buildRuntimeOverlay,
  writeRuntimeOverlay,
} from '../../apps/desktop/src/main/memory/runtime-overlay'
import { createMemorySessionRunner } from '../../apps/desktop/src/main/memory/memory-session'
import { consolidate } from '../../apps/desktop/src/main/memory/memory-work'
import { readPrompt } from '../../apps/desktop/src/main/memory/prompts'
import { openMemoryStore } from '../../apps/desktop/src/memory/store'

const apiKey = process.env['FACTORY_API_KEY']
const baseUrl = process.env['FACTORY_API_BASE_URL']
const model = process.env['DROI_LIVE_MODEL'] ?? 'glm-5.3-flash'
const TOKEN = 'live-memory-token'
const desktop = fileURLToPath(new URL('../../apps/desktop/', import.meta.url))
const built = join(desktop, 'out/main')

test.skip(!apiKey, 'FACTORY_API_KEY is not set; the live test only runs with a real key')
test.skip(!existsSync(join(built, 'memory-server.js')), 'Run `pnpm build` first')

let root: string
let workspace: string
let memoryDir: string
let daemon: DaemonSupervisor
let gateway: Gateway
let droid: ConnectedDroid

test.beforeAll(async () => {
  const droidPath = locateDroid()
  if (!droidPath) throw new Error('droid executable not found')
  root = realpathSync(mkdtempSync(join(tmpdir(), 'droi-live-memory-')))
  const home = join(root, 'home')
  workspace = join(root, 'workspace')
  memoryDir = join(root, 'memory')
  mkdirSync(home)
  mkdirSync(workspace)
  const overlay = join(root, 'runtime-overlay.json')
  writeRuntimeOverlay(
    overlay,
    buildRuntimeOverlay({
      nodeBinary: createRequire(join(desktop, 'package.json'))('electron') as string,
      serverEntry: join(built, 'memory-server.js'),
      hookEntry: join(built, 'memory-hook.js'),
      memoryDir,
      approvedAt: new Date().toISOString(),
    }),
  )
  daemon = new DaemonSupervisor({
    spawn: (port) =>
      spawn(
        droidPath,
        daemonArgs({
          port,
          liveness: ['--parent-pid', String(process.pid)],
          runtimeOverlay: overlay,
        }),
        {
          cwd: root,
          stdio: 'ignore',
          env: {
            ...process.env,
            HOME: home,
            FACTORY_API_KEY: apiKey!,
            ...(baseUrl ? { FACTORY_API_BASE_URL: baseUrl } : {}),
          },
        },
      ),
  })
  daemon.start()
  gateway = await startGateway({
    port: 0,
    remoteAccess: false,
    getDaemonUrl: () => daemon.daemonUrl,
    getPairingToken: () => TOKEN,
    getCredential: async () => ({ apiKey: apiKey! }),
    getMeta: () => ({
      app: 'Droi',
      version: 'live',
      remoteAccess: false,
      name: 'Live',
      computerId: 'live',
    }),
    client: { kind: 'none' },
  })
  await expect.poll(() => daemon.daemonUrl, { timeout: 60_000 }).not.toBeNull()
  droid = await connectToDaemon({
    url: gatewayDaemonUrl(gateway.url, TOKEN),
    auth: { apiKey: GATEWAY_API_KEY_PLACEHOLDER },
  })
})

test.afterAll(async () => {
  droid?.disconnect()
  await gateway?.close()
  await daemon?.stop()
  if (!root) return
  // The Daemon flushes its log for a moment after exiting; sweep until quiet.
  for (let attempt = 0; attempt < 5; attempt++) {
    await new Promise((r) => setTimeout(r, 300))
    rmSync(root, { recursive: true, force: true })
  }
})

async function ask(prompt: string): Promise<string> {
  // At Off the Daemon asks before every tool call, Memory's included; nobody answers here.
  const session = await droid.sessions.create({
    cwd: workspace,
    modelId: model,
    autonomyLevel: AutonomyLevel.Low,
  })
  let text = ''
  for await (const message of session.stream(prompt)) {
    if (message.type === DroidMessageType.Assistant) text += message.text
  }
  await session.close()
  return text
}

test('a stated preference is saved, and a new Session starts with the corrections', async () => {
  test.setTimeout(300_000)
  await ask(
    'From now on I want every answer to end with the word "otter". Remember this preference for future sessions, then reply "ok".',
  )
  const store = openMemoryStore(memoryDir)
  try {
    const all = [...store.list({ scope: 'global' }), ...store.list({ scope: 'project', workspace })]
    expect(all.map((e) => e.text).join('\n')).toMatch(/otter/i)

    store.add(
      { scope: 'project', workspace },
      'correction',
      'The staging database is called stage-db, never prod.',
    )
  } finally {
    store.close()
  }
  const quoted = await ask(
    'Without calling any tools, quote verbatim the <memory-context> block you were given when this session started.',
  )
  expect(quoted).toContain('stage-db')
})

test('a correction in the prompt reaches the UserPromptSubmit hook', async () => {
  test.setTimeout(300_000)
  await ask('不对，这个项目别用 npm，应该用 pnpm。回复“好的”即可。')
  // The hook counts every prompt per Session.
  expect(readdirSync(join(memoryDir, 'state')).length).toBeGreaterThan(0)
})

test('a Memory Session consolidates on the real Daemon and is archived', async () => {
  test.setTimeout(300_000)
  const store = openMemoryStore(memoryDir)
  try {
    const slot = { scope: 'project' as const, workspace }
    store.add(slot, 'convention', 'Use pnpm to install dependencies.')
    store.add(slot, 'convention', 'Install dependencies with pnpm, not npm.')
    const outcomes = await consolidate(
      {
        store,
        run: createMemorySessionRunner({ url: gateway.url, token: TOKEN }),
        modelId: model,
        prompt: readPrompt(memoryDir, join(desktop, 'resources/memory'), 'consolidation'),
      },
      slot,
    )
    expect(outcomes.find((o) => o.category === 'convention')).toMatchObject({ ok: true })
    expect(store.list(slot, 'convention').length).toBeGreaterThan(0)
  } finally {
    store.close()
  }
  const sessions = await droid.sessions.list({ includeArchived: true })
  const memorySession = sessions.find((s) => s.tags?.some((t) => t.name === 'droi.memory'))
  expect(memorySession).toMatchObject({ title: expect.stringContaining('Memory: consolidate') })
  expect(memorySession?.archivedTime).toBeDefined()
})
