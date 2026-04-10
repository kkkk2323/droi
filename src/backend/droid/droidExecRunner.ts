import { execFile } from 'child_process'
import { DroidJsonRpcManager, type DroidBackendEvent } from './jsonrpc/droidJsonRpcManager.ts'
import type {
  AvailableModelConfig,
  DroidAutonomyLevel,
  DroidInteractionMode,
  DroidPermissionOption,
} from '../../shared/protocol.ts'
import { resolveDroidPath } from './resolveDroidPath.ts'
import type { LocalDiagnostics } from '../diagnostics/localDiagnostics.ts'
import type { DecompSessionType, SessionKind } from '../../shared/sessionProtocol.ts'

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
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export interface DroidExecUpdateSettingsOptions {
  sessionId: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
}

export interface DroidExecCreateSessionOptions {
  machineId: string
  cwd: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export interface DroidExecLoadSessionSnapshotOptions {
  sessionId: string
  machineId: string
  cwd: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export interface DroidExecSendLoadedSessionMessageOptions {
  sessionId: string
  loadSessionId: string
  machineId: string
  prompt: string
  cwd: string
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

  getFirstSessionId(): string | null {
    return this.manager.getFirstSessionId()
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
      decompSessionType: options.decompSessionType,
      isMission: options.isMission,
      sessionKind: options.sessionKind,
      reasoningEffort: options.reasoningEffort,
      env: options.env,
    })
  }

  async createSession(
    options: DroidExecCreateSessionOptions,
  ): Promise<{ sessionId: string; availableModels?: AvailableModelConfig[] }> {
    const result = await this.manager.createSession({
      machineId: options.machineId,
      cwd: options.cwd,
      modelId: options.modelId,
      interactionMode: options.interactionMode,
      autonomyLevel: options.autonomyLevel,
      decompSessionType: options.decompSessionType,
      isMission: options.isMission,
      sessionKind: options.sessionKind,
      reasoningEffort: options.reasoningEffort,
      env: options.env,
    })
    return result
  }

  updateSessionSettings(options: DroidExecUpdateSettingsOptions): Promise<void> {
    return this.manager.updateSessionSettings({
      sessionId: options.sessionId,
      modelId: options.modelId,
      interactionMode: options.interactionMode,
      autonomyLevel: options.autonomyLevel,
      decompSessionType: options.decompSessionType,
      isMission: options.isMission,
      sessionKind: options.sessionKind,
      reasoningEffort: options.reasoningEffort,
    })
  }

  async killWorkerSession(options: { sessionId: string; workerSessionId: string }): Promise<void> {
    return this.manager.killWorkerSession(options)
  }

  sendLoadedSessionMessage(options: DroidExecSendLoadedSessionMessageOptions): Promise<void> {
    return this.manager.sendLoadedSessionMessage(options)
  }

  loadSessionSnapshot(
    options: DroidExecLoadSessionSnapshotOptions,
  ): Promise<Record<string, unknown> | null> {
    return this.manager.loadSessionSnapshot({
      sessionId: options.sessionId,
      machineId: options.machineId,
      cwd: options.cwd,
      modelId: options.modelId,
      interactionMode: options.interactionMode,
      autonomyLevel: options.autonomyLevel,
      decompSessionType: options.decompSessionType,
      isMission: options.isMission,
      sessionKind: options.sessionKind,
      reasoningEffort: options.reasoningEffort,
      env: options.env,
    })
  }

  async listSkills(sessionId: string): Promise<unknown[]> {
    return this.manager.listSkills(sessionId)
  }

  async addUserMessage(sessionId: string, text: string): Promise<void> {
    return this.manager.addUserMessage(sessionId, text)
  }

  respondPermission(params: {
    sessionId: string
    requestId: string
    selectedOption: DroidPermissionOption
    selectedExitSpecModeOptionIndex?: number
    exitSpecModeComment?: string
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
