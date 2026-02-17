import { createAdaptorServer } from '@hono/node-server'
import { DroidExecManager } from '../backend/droid/droidExecRunner.ts'
import { setTraceChainEnabledOverride } from '../backend/droid/jsonrpc/notificationFingerprint.ts'
import { createKeyStore } from '../backend/keys/keyStore.ts'
import { SetupScriptRunner } from '../backend/session/setupScriptRunner.ts'
import { LocalDiagnostics } from '../backend/diagnostics/localDiagnostics.ts'
import { createAppStateStore } from '../backend/storage/appStateStore.ts'
import { resolveServerDataDir } from '../backend/storage/dataDir.ts'
import { createSessionStore } from '../backend/storage/sessionStore.ts'
import type { PersistedAppState } from '../shared/protocol'
import { createHonoApp } from './hono/app.ts'
import type { HonoAppDeps } from './hono/types.ts'

function readTraceChainEnabled(state: PersistedAppState): boolean | undefined {
  const raw = (state as any)?.traceChainEnabled
  return typeof raw === 'boolean' ? raw : undefined
}

function readLocalDiagnosticsEnabled(state: PersistedAppState): boolean | undefined {
  const raw = (state as any)?.localDiagnosticsEnabled
  return typeof raw === 'boolean' ? raw : undefined
}

function readLocalDiagnosticsRetention(state: PersistedAppState): { retentionDays?: number; maxTotalMb?: number } {
  const daysRaw = (state as any)?.localDiagnosticsRetentionDays
  const mbRaw = (state as any)?.localDiagnosticsMaxTotalMb
  const retentionDays = (typeof daysRaw === 'number' && Number.isFinite(daysRaw)) ? Math.max(1, Math.floor(daysRaw)) : undefined
  const maxTotalMb = (typeof mbRaw === 'number' && Number.isFinite(mbRaw)) ? Math.max(1, Math.floor(mbRaw)) : undefined
  return { retentionDays, maxTotalMb }
}

export function shouldRequireAuth(_params: { remoteAddress: string | undefined | null; method: string | undefined; path: string }): boolean {
  return false
}

export function resolvePairingPort(apiPort: number, pairingWebPort: number | undefined): number {
  const p = Number(pairingWebPort)
  return Number.isFinite(p) && p > 0 ? p : apiPort
}

export interface StartApiServerOpts {
  host: string
  port: number
  baseDir?: string
  webRootDir?: string | null
  pairingTokenTtlSeconds?: number
  pairingWebPort?: number
  diagnostics?: LocalDiagnostics
}

export async function startApiServer(opts: StartApiServerOpts) {
  const baseDir = opts.baseDir || resolveServerDataDir()
  const appStateStore = createAppStateStore({ baseDir })
  const sessionStore = createSessionStore({ baseDir })
  const diagnostics = opts.diagnostics || new LocalDiagnostics({ baseDir })
  const execManager = new DroidExecManager({ diagnostics })
  const setupScriptRunner = new SetupScriptRunner()
  const unsubscribeSessionReplace = execManager.onEvent((ev) => {
    if (ev.type === 'session-id-replaced') void sessionStore.replaceSessionId(ev.oldSessionId, ev.newSessionId)
  })

  const cachedStateRef: { value: PersistedAppState } = {
    value: { version: 2, machineId: '' },
  }

  cachedStateRef.value = await appStateStore.load()
  setTraceChainEnabledOverride(readTraceChainEnabled(cachedStateRef.value))
  const diagEnabled = readLocalDiagnosticsEnabled(cachedStateRef.value)
  diagnostics.setEnabled(typeof diagEnabled === 'boolean' ? diagEnabled : true)
  const retention = readLocalDiagnosticsRetention(cachedStateRef.value)
  const bytes = typeof retention.maxTotalMb === 'number' ? retention.maxTotalMb * 1024 * 1024 : undefined
  diagnostics.setRetention({ maxAgeDays: retention.retentionDays, maxTotalBytes: bytes })
  await diagnostics.startMaintenance()

  const keyStore = createKeyStore(appStateStore)
  const runtimePortRef = { value: opts.port }
  const deps: HonoAppDeps = {
    opts,
    runtimePortRef,
    appStateStore,
    sessionStore,
    execManager,
    setupScriptRunner,
    cachedStateRef,
    diagnostics,
    keyStore,
  }

  const app = createHonoApp(deps)
  const server = createAdaptorServer({ fetch: app.fetch })

  await new Promise<void>((resolvePromise, rejectPromise) => {
    const onError = (err: unknown) => {
      ;(server as any).off('error', onError)
      rejectPromise(err)
    }
    ;(server as any).once('error', onError)
    ;(server as any).listen(opts.port, opts.host, () => {
      ;(server as any).off('error', onError)
      resolvePromise()
    })
  })

  const addr = (server as any).address()
  const actualPort = (addr && typeof addr === 'object' && typeof addr.port === 'number')
    ? Number(addr.port)
    : opts.port
  runtimePortRef.value = actualPort

  return {
    server,
    host: opts.host,
    port: actualPort,
    baseDir,
    close: async () => {
      unsubscribeSessionReplace()
      setupScriptRunner.disposeAll()
      return await new Promise<void>((resolvePromise) => (server as any).close(() => resolvePromise()))
    },
  }
}
