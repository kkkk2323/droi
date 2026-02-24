import { execFile } from 'child_process'
import { DroidJsonRpcManager, type DroidBackendEvent } from './jsonrpc/droidJsonRpcManager.ts'
import type {
  DroidAutonomyLevel,
  DroidInteractionMode,
  DroidPermissionOption,
} from './jsonrpc/jsonRpcTypes.ts'
import { resolveDroidPath } from './resolveDroidPath.ts'
import type { LocalDiagnostics } from '../diagnostics/localDiagnostics.ts'

export type { DroidBackendEvent }

export interface DroidExecSendOptions {
  sessionId: string
  resumeSessionId?: string
  machineId: string
  prompt: string
  cwd: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export interface DroidExecUpdateSettingsOptions {
  sessionId: string
  modelId?: string
  autonomyLevel?: DroidAutonomyLevel
  reasoningEffort?: string
}

export interface DroidExecCreateSessionOptions {
  machineId: string
  cwd: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export function getDroidVersion(droidPath = resolveDroidPath()): Promise<string> {
  return new Promise((resolve) => {
    execFile(droidPath, ['--version'], { env: { ...process.env } }, (err, stdout, stderr) => {
      if (err) resolve(`Error: ${String(err.message || err)}`)
      else resolve(String(stdout || stderr).trim())
    })
  })
}

export class DroidExecManager {
  private readonly listeners = new Set<(ev: DroidBackendEvent) => void>()
  private readonly manager: DroidJsonRpcManager

  constructor(opts?: { droidPath?: string; diagnostics?: LocalDiagnostics }) {
    this.manager = new DroidJsonRpcManager({
      droidPath: opts?.droidPath,
      diagnostics: opts?.diagnostics,
      emit: (ev) => this.listeners.forEach((cb) => cb(ev)),
    })
  }

  onEvent(cb: (ev: DroidBackendEvent) => void): () => void {
    this.listeners.add(cb)
    return () => this.listeners.delete(cb)
  }

  hasSession(sessionId: string): boolean {
    return this.manager.hasSession(sessionId)
  }

  send(options: DroidExecSendOptions): Promise<void> {
    return this.manager.sendUserMessage({
      sessionId: options.sessionId,
      resumeSessionId: options.resumeSessionId,
      machineId: options.machineId,
      prompt: options.prompt,
      cwd: options.cwd,
      modelId: options.modelId,
      interactionMode: options.interactionMode,
      autonomyLevel: options.autonomyLevel,
      reasoningEffort: options.reasoningEffort,
      env: options.env,
    })
  }

  async createSession(options: DroidExecCreateSessionOptions): Promise<{ sessionId: string }> {
    const sessionId = await this.manager.createSession({
      machineId: options.machineId,
      cwd: options.cwd,
      modelId: options.modelId,
      interactionMode: options.interactionMode,
      autonomyLevel: options.autonomyLevel,
      reasoningEffort: options.reasoningEffort,
      env: options.env,
    })
    return { sessionId }
  }

  updateSessionSettings(options: DroidExecUpdateSettingsOptions): Promise<void> {
    return this.manager.updateSessionSettings({
      sessionId: options.sessionId,
      modelId: options.modelId,
      autonomyLevel: options.autonomyLevel,
      reasoningEffort: options.reasoningEffort,
    })
  }

  respondPermission(params: {
    sessionId: string
    requestId: string
    selectedOption: DroidPermissionOption
  }): void {
    this.manager.respondPermission(params)
  }

  respondAskUser(params: {
    sessionId: string
    requestId: string
    cancelled?: boolean
    answers: Array<{ index: number; question: string; answer: string }>
  }): void {
    this.manager.respondAskUser(params)
  }

  cancel(sessionId: string): void {
    this.manager.cancel(sessionId)
  }

  disposeSession(sessionId: string): void {
    this.manager.disposeSession(sessionId)
  }

  disposeAllSessions(): boolean {
    return this.manager.disposeAllSessions() > 0
  }
}
