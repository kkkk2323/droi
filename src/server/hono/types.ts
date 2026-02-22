import type { HttpBindings } from '@hono/node-server'
import type { DroidExecManager } from '../../backend/droid/droidExecRunner.ts'
import type { LocalDiagnostics } from '../../backend/diagnostics/localDiagnostics.ts'
import type { SetupScriptRunner } from '../../backend/session/setupScriptRunner.ts'
import type { KeyStoreAPI } from '../../backend/keys/keyStore.ts'
import type { AppStateStore } from '../../backend/storage/appStateStore.ts'
import type { SessionStore } from '../../backend/storage/sessionStore.ts'
import type { PersistedAppState } from '../../shared/protocol'
import type { StartApiServerOpts } from '../apiServer.ts'

export interface HonoAppDeps {
  opts: StartApiServerOpts
  runtimePortRef: { value: number }
  appStateStore: AppStateStore
  sessionStore: SessionStore
  execManager: DroidExecManager
  setupScriptRunner: SetupScriptRunner
  cachedStateRef: { value: PersistedAppState }
  diagnostics: LocalDiagnostics
  keyStore: KeyStoreAPI
}

export interface ServerEnv {
  Bindings: HttpBindings
  Variables: {
    deps: HonoAppDeps
    appVersion: string
  }
}
