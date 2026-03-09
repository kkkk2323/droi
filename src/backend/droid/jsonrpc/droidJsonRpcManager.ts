import { resolveDroidPath } from '../resolveDroidPath.ts'
import { randomUUID } from 'crypto'
import type {
  DroidAutonomyLevel,
  DroidInteractionMode,
  DroidPermissionOption,
  JsonRpcNotification,
  JsonRpcRequest,
} from './jsonRpcTypes.ts'
import { DroidJsonRpcSession, type DroidRpcSessionEvent } from './droidJsonRpcSession.ts'
import { formatNotificationTrace, isTraceChainEnabled } from './notificationFingerprint.ts'
import type { LocalDiagnostics } from '../../diagnostics/localDiagnostics.ts'
import {
  resolveSessionProtocolFields,
  type DecompSessionType,
  type SessionKind,
} from '../../../shared/sessionProtocol.ts'

export type DroidBackendEvent =
  | { type: 'stdout'; sessionId: string; data: string }
  | { type: 'stderr'; sessionId: string; data: string }
  | { type: 'rpc-notification'; sessionId: string; message: JsonRpcNotification }
  | { type: 'rpc-request'; sessionId: string; message: JsonRpcRequest }
  | { type: 'session-id-replaced'; oldSessionId: string; newSessionId: string; reason: string }
  | { type: 'turn-end'; sessionId: string; code: number }
  | { type: 'error'; sessionId: string; message: string }
  | { type: 'debug'; sessionId: string; message: string }

export interface SendUserMessageParams {
  sessionId: string
  resumeSessionId?: string
  cwd: string
  machineId: string
  prompt: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export interface CreateSessionParams {
  cwd: string
  machineId: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export interface LoadSessionSnapshotParams {
  sessionId: string
  cwd: string
  machineId: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

export interface UpdateSessionSettingsParams {
  sessionId: string
  modelId?: string
  interactionMode?: DroidInteractionMode
  autonomyLevel?: DroidAutonomyLevel
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
}

type ManagedSession = {
  session: DroidJsonRpcSession
  ref: { id: string }
  protocol: ReturnType<typeof resolveSessionProtocolFields>
}

export class DroidJsonRpcManager {
  private readonly droidPath: string
  private readonly sessions = new Map<string, ManagedSession>()
  private readonly emit: (ev: DroidBackendEvent) => void
  private readonly diagnostics?: LocalDiagnostics

  constructor(opts: {
    emit: (ev: DroidBackendEvent) => void
    droidPath?: string
    diagnostics?: LocalDiagnostics
  }) {
    this.emit = opts.emit
    this.droidPath = opts.droidPath || resolveDroidPath()
    this.diagnostics = opts.diagnostics
  }

  async sendUserMessage(params: SendUserMessageParams): Promise<void> {
    let sid = params.sessionId
    const managed = this.getOrCreateSession(params)
    const session = managed.session
    const protocol = this.resolveProtocol(params, managed.protocol)
    managed.protocol = protocol
    let stage = 'ensureInitialized'
    try {
      this.emit({
        type: 'debug',
        sessionId: sid,
        message: 'sendUserMessage: ensureInitialized start',
      })
      const init = await session.ensureInitialized(
        {
          modelId: params.modelId,
          interactionMode: protocol.interactionMode,
          autonomyLevel: protocol.autonomyLevel,
          decompSessionType: protocol.decompSessionType,
          reasoningEffort: params.reasoningEffort,
        },
        params.resumeSessionId,
      )

      const effectiveEngineSessionId = init.engineSessionId || sid
      if (effectiveEngineSessionId && effectiveEngineSessionId !== sid) {
        const reason =
          init.source === 'resume_failed'
            ? 'resume_failed'
            : init.source === 'resume_invalid'
              ? 'resume_invalid'
              : 'session_id_mismatch'
        this.rekeySession(sid, effectiveEngineSessionId)
        this.emit({
          type: 'session-id-replaced',
          oldSessionId: sid,
          newSessionId: effectiveEngineSessionId,
          reason,
        })
        sid = effectiveEngineSessionId
      }

      this.emit({
        type: 'debug',
        sessionId: sid,
        message: 'sendUserMessage: ensureInitialized done',
      })

      stage = 'updateSettings'
      this.emit({ type: 'debug', sessionId: sid, message: 'sendUserMessage: updateSettings start' })
      await session.updateSettings({
        modelId: params.modelId,
        interactionMode: protocol.interactionMode,
        autonomyLevel: protocol.autonomyLevel,
        reasoningEffort: params.reasoningEffort,
      })
      this.emit({ type: 'debug', sessionId: sid, message: 'sendUserMessage: updateSettings done' })

      stage = 'addUserMessage'
      this.emit({ type: 'debug', sessionId: sid, message: 'sendUserMessage: addUserMessage start' })
      await session.addUserMessage({ text: params.prompt })
      this.emit({ type: 'debug', sessionId: sid, message: 'sendUserMessage: addUserMessage done' })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      this.emit({
        type: 'debug',
        sessionId: sid,
        message: `sendUserMessage: failed stage=${stage} error=${msg}`,
      })
      this.emit({ type: 'error', sessionId: sid, message: msg })
      this.emit({ type: 'turn-end', sessionId: sid, code: 1 })
    }
  }

  async createSession(params: CreateSessionParams): Promise<string> {
    const tempSessionId = `create-${randomUUID()}`
    const noop = () => {}
    const session = new DroidJsonRpcSession({
      droidPath: this.droidPath,
      sessionId: tempSessionId,
      cwd: params.cwd,
      machineId: params.machineId,
      env: params.env || {},
      diagnostics: this.diagnostics,
      onEvent: noop,
    })

    try {
      const protocol = this.resolveProtocol(params)
      const init = await session.ensureInitialized({
        modelId: params.modelId,
        interactionMode: protocol.interactionMode,
        autonomyLevel: protocol.autonomyLevel,
        decompSessionType: protocol.decompSessionType,
        reasoningEffort: params.reasoningEffort,
      })
      const id = String(init.engineSessionId || '').trim()
      if (!id) throw new Error('initialize_session did not return sessionId')
      return id
    } finally {
      session.dispose()
    }
  }

  async loadSessionSnapshot(
    params: LoadSessionSnapshotParams,
  ): Promise<Record<string, unknown> | null> {
    const tempSessionId = `load-${randomUUID()}`
    const session = new DroidJsonRpcSession({
      droidPath: this.droidPath,
      sessionId: tempSessionId,
      cwd: params.cwd,
      machineId: params.machineId,
      env: params.env || {},
      diagnostics: this.diagnostics,
      onEvent: () => {},
    })

    try {
      const protocol = this.resolveProtocol(params)
      await session.ensureInitialized({
        modelId: params.modelId,
        interactionMode: protocol.interactionMode,
        autonomyLevel: protocol.autonomyLevel,
        decompSessionType: protocol.decompSessionType,
        reasoningEffort: params.reasoningEffort,
      })
      return await session.loadSessionSnapshot(params.sessionId)
    } finally {
      session.dispose()
    }
  }

  hasSession(sessionId: string): boolean {
    return this.sessions.has(sessionId)
  }

  getFirstSessionId(): string | null {
    const first = this.sessions.keys().next()
    return first.done ? null : first.value
  }

  async updateSessionSettings(params: UpdateSessionSettingsParams): Promise<void> {
    const managed = this.sessions.get(params.sessionId)
    if (!managed) return
    managed.protocol = this.resolveProtocol(params, managed.protocol)
    await managed.session.updateSettings({
      modelId: params.modelId,
      interactionMode: managed.protocol.interactionMode,
      autonomyLevel: managed.protocol.autonomyLevel,
      reasoningEffort: params.reasoningEffort,
    })
  }

  async killWorkerSession(params: { sessionId: string; workerSessionId: string }): Promise<void> {
    const managed = this.sessions.get(params.sessionId)
    if (!managed) return
    await managed.session.killWorkerSession(params.workerSessionId)
  }

  async listSkills(sessionId: string): Promise<unknown[]> {
    const managed = this.sessions.get(sessionId)
    if (!managed) return []
    return managed.session.listSkills()
  }

  async addUserMessage(sessionId: string, text: string): Promise<void> {
    const managed = this.sessions.get(sessionId)
    if (!managed) throw new Error(`No session: ${sessionId}`)
    await managed.session.addUserMessage({ text })
  }

  respondPermission(params: {
    sessionId: string
    requestId: string
    selectedOption: DroidPermissionOption
    selectedExitSpecModeOptionIndex?: number
    exitSpecModeComment?: string
  }): void {
    const managed = this.sessions.get(params.sessionId)
    if (!managed) return
    const result: Record<string, unknown> = { selectedOption: params.selectedOption }
    if (params.selectedExitSpecModeOptionIndex !== undefined) {
      result.selectedExitSpecModeOptionIndex = params.selectedExitSpecModeOptionIndex
    }
    if (params.exitSpecModeComment !== undefined) {
      result.exitSpecModeComment = params.exitSpecModeComment
    }
    managed.session.sendResponse(params.requestId, result)
  }

  respondAskUser(params: {
    sessionId: string
    requestId: string
    cancelled?: boolean
    answers: Array<{ index: number; question: string; answer: string }>
  }): void {
    const managed = this.sessions.get(params.sessionId)
    if (!managed) return
    managed.session.sendResponse(params.requestId, {
      cancelled: params.cancelled,
      answers: params.answers,
    })
  }

  cancel(sessionId: string): void {
    const managed = this.sessions.get(sessionId)
    if (!managed) return
    void managed.session.interrupt()
  }

  disposeSession(sessionId: string): void {
    const managed = this.sessions.get(sessionId)
    if (!managed) return
    managed.session.dispose()
    this.sessions.delete(sessionId)
  }

  disposeAllSessions(): number {
    const ids = Array.from(this.sessions.keys())
    for (const id of ids) this.disposeSession(id)
    return ids.length
  }

  private getOrCreateSession(params: SendUserMessageParams): ManagedSession {
    const existing = this.sessions.get(params.sessionId)
    if (existing) {
      existing.protocol = this.resolveProtocol(params, existing.protocol)
      return existing
    }

    const ref = { id: params.sessionId }
    const session = new DroidJsonRpcSession({
      droidPath: this.droidPath,
      sessionId: params.sessionId,
      cwd: params.cwd,
      machineId: params.machineId,
      env: params.env || {},
      diagnostics: this.diagnostics,
      onEvent: (ev: DroidRpcSessionEvent) => this.handleSessionEvent(ref, ev),
    })
    const managed: ManagedSession = {
      session,
      ref,
      protocol: this.resolveProtocol(params),
    }
    this.sessions.set(params.sessionId, managed)
    return managed
  }

  private resolveProtocol(
    params: SendUserMessageParams | CreateSessionParams | UpdateSessionSettingsParams,
    existing?: ManagedSession['protocol'],
  ): ManagedSession['protocol'] {
    return resolveSessionProtocolFields({
      explicit: {
        isMission: params.isMission,
        sessionKind: params.sessionKind,
        interactionMode: params.interactionMode,
        autonomyLevel: params.autonomyLevel,
        decompSessionType: params.decompSessionType,
      },
      existing,
    })
  }

  private rekeySession(oldSessionId: string, newSessionId: string): void {
    if (oldSessionId === newSessionId) return
    const managed = this.sessions.get(oldSessionId)
    if (!managed) return

    const existing = this.sessions.get(newSessionId)
    if (existing && existing !== managed) {
      existing.session.dispose()
      this.sessions.delete(newSessionId)
    }

    this.sessions.delete(oldSessionId)
    managed.ref.id = newSessionId
    this.sessions.set(newSessionId, managed)
  }

  private handleSessionEvent(ref: { id: string }, ev: DroidRpcSessionEvent): void {
    const sessionId = ref.id
    if (ev.type === 'stdout') this.emit({ type: 'stdout', sessionId, data: ev.data })
    else if (ev.type === 'stderr') this.emit({ type: 'stderr', sessionId, data: ev.data })
    else if (ev.type === 'rpc-notification') {
      if (isTraceChainEnabled()) {
        this.emit({
          type: 'debug',
          sessionId,
          message: formatNotificationTrace('manager-out', ev.message),
        })
      }
      this.emit({ type: 'rpc-notification', sessionId, message: ev.message })
    } else if (ev.type === 'rpc-request')
      this.emit({ type: 'rpc-request', sessionId, message: ev.message })
    else if (ev.type === 'turn-end') this.emit({ type: 'turn-end', sessionId, code: ev.code })
    else if (ev.type === 'error') this.emit({ type: 'error', sessionId, message: ev.message })
    else if (ev.type === 'debug') this.emit({ type: 'debug', sessionId, message: ev.message })
  }
}
