import {
  AutonomyLevel,
  DroidClient,
  DroidInteractionMode,
  DroidSession,
  ProcessTransport,
  ReasoningEffort,
  type AskUserCollectedAnswer,
  type AskUserRequestParams,
  type DroidMessage,
  type InitializeSessionResult,
  type LoadSessionResult,
  type ToolConfirmationOutcome,
  type UpdateSessionSettingsRequestParams,
} from '@factory/droid-sdk'
import type { DecompSessionType, SessionKind } from '../../shared/sessionProtocol.ts'
import { resolveDroidPath } from './resolveDroidPath.ts'
import {
  buildAskUserRequestPayload,
  buildPermissionRequestPayload,
  normalizeExecEnv,
  resolvePermissionOutcome,
  type DroidBackendEvent,
} from './sdkSessionBridge.ts'
import type { DroidPermissionOption } from '../../shared/protocol.ts'

type SessionSettingsSnapshot = {
  cwd: string
  machineId: string
  modelId?: string
  interactionMode?: string
  autonomyLevel?: string
  decompSessionType?: DecompSessionType
  isMission?: boolean
  sessionKind?: SessionKind
  reasoningEffort?: string
  env?: Record<string, string | undefined>
}

type PendingPermissionBridge = {
  requestKey: string
  resolve: (value: ToolConfirmationOutcome) => void
  reject: (error: Error) => void
}

type PendingAskUserBridge = {
  requestKey: string
  resolve: (value: { cancelled?: boolean; answers: AskUserCollectedAnswer[] }) => void
  reject: (error: Error) => void
}

type ManagedSession = {
  sessionId: string
  client: DroidClient
  sdkSession: DroidSession
  initResult: InitializeSessionResult | LoadSessionResult
  settings: SessionSettingsSnapshot
  pendingPermission?: PendingPermissionBridge
  pendingAskUser?: PendingAskUserBridge
  turnPromise?: Promise<void>
}

function toSdkInteractionMode(value?: string): DroidInteractionMode | undefined {
  if (value === 'auto') return DroidInteractionMode.Auto
  if (value === 'spec') return DroidInteractionMode.Spec
  if (value === 'agi') return DroidInteractionMode.AGI
  return undefined
}

function toSdkAutonomyLevel(value?: string): AutonomyLevel | undefined {
  if (value === 'off') return AutonomyLevel.Off
  if (value === 'low') return AutonomyLevel.Low
  if (value === 'medium') return AutonomyLevel.Medium
  if (value === 'high') return AutonomyLevel.High
  return undefined
}

function toSdkReasoningEffort(value?: string): ReasoningEffort | undefined {
  if (!value) return undefined
  return value as ReasoningEffort
}

function buildUpdateSettingsParams(
  settings: SessionSettingsSnapshot,
): Partial<UpdateSessionSettingsRequestParams> {
  return {
    ...(settings.modelId ? { modelId: settings.modelId } : {}),
    ...(settings.interactionMode
      ? { interactionMode: toSdkInteractionMode(settings.interactionMode) }
      : {}),
    ...(settings.autonomyLevel
      ? { autonomyLevel: toSdkAutonomyLevel(settings.autonomyLevel) }
      : {}),
    ...(settings.reasoningEffort
      ? { reasoningEffort: toSdkReasoningEffort(settings.reasoningEffort) }
      : {}),
  }
}

export class SessionManager {
  private readonly sessions = new Map<string, ManagedSession>()
  private readonly opts: {
    droidPath?: string
    emit: (ev: DroidBackendEvent) => void
  }

  constructor(opts: { droidPath?: string; emit: (ev: DroidBackendEvent) => void }) {
    this.opts = opts
  }

  hasSession(id: string): boolean {
    return this.sessions.has(id)
  }

  getFirstSessionId(): string | null {
    return this.sessions.keys().next().value ?? null
  }

  async createSession(settings: SessionSettingsSnapshot): Promise<string> {
    const managed = await this.createManagedSession(settings)
    this.sessions.set(managed.sessionId, managed)
    return managed.sessionId
  }

  async sendUserMessage(
    settings: SessionSettingsSnapshot & {
      sessionId: string
      resumeSessionId?: string
      prompt: string
    },
  ): Promise<void> {
    const managed = await this.getOrCreateSession(settings)
    managed.settings = {
      ...managed.settings,
      ...settings,
      env: settings.env ?? managed.settings.env,
    }
    const turnPromise = this.streamTurn(managed, settings.prompt, settings.sessionId)
    managed.turnPromise = turnPromise
    await turnPromise
  }

  async updateSessionSettings(
    settings: Partial<SessionSettingsSnapshot> & { sessionId: string },
  ): Promise<void> {
    const managed = this.sessions.get(settings.sessionId)
    if (!managed) throw new Error(`Session not found: ${settings.sessionId}`)
    managed.settings = {
      ...managed.settings,
      ...settings,
      env: settings.env ?? managed.settings.env,
    }
    await managed.sdkSession.updateSettings(buildUpdateSettingsParams(managed.settings))
  }

  async loadSessionSnapshot(
    settings: SessionSettingsSnapshot & { sessionId: string },
  ): Promise<Record<string, unknown> | null> {
    const temp = await this.createLoadedSession(settings.sessionId, settings)
    try {
      return (temp.initResult as unknown as Record<string, unknown>) ?? null
    } finally {
      await temp.sdkSession.close()
    }
  }

  async listSkills(sessionId: string): Promise<unknown[]> {
    const managed = this.sessions.get(sessionId)
    if (managed)
      return (((await managed.sdkSession.listSkills()) as any)?.skills as unknown[]) ?? []
    const temp = await this.createLoadedSession(sessionId, {
      sessionId,
      machineId: 'default',
      cwd: process.cwd(),
    })
    try {
      return (((await temp.sdkSession.listSkills()) as any)?.skills as unknown[]) ?? []
    } finally {
      await temp.sdkSession.close()
    }
  }

  async addUserMessage(sessionId: string, text: string): Promise<void> {
    const managed = this.sessions.get(sessionId)
    if (!managed) throw new Error(`Session not found: ${sessionId}`)
    await managed.client.addUserMessage({ text })
  }

  respondPermission(params: { sessionId: string; selectedOption: DroidPermissionOption }): void {
    const managed = this.sessions.get(params.sessionId)
    if (!managed?.pendingPermission) return
    const bridge = managed.pendingPermission
    managed.pendingPermission = undefined
    bridge.resolve(resolvePermissionOutcome(params.selectedOption))
  }

  respondAskUser(params: {
    sessionId: string
    cancelled?: boolean
    answers: Array<{ index: number; question: string; answer: string }>
  }): void {
    const managed = this.sessions.get(params.sessionId)
    if (!managed?.pendingAskUser) return
    const bridge = managed.pendingAskUser
    managed.pendingAskUser = undefined
    bridge.resolve({
      cancelled: params.cancelled,
      answers: params.answers.map((answer) => ({
        index: answer.index,
        question: answer.question,
        answer: answer.answer,
      })),
    })
  }

  cancel(sessionId: string): void {
    const managed = this.sessions.get(sessionId)
    if (!managed) return
    void managed.sdkSession.interrupt()
  }

  async killWorkerSession(params: { sessionId: string; workerSessionId: string }): Promise<void> {
    const managed = await this.getOrCreateSession({
      sessionId: params.sessionId,
      machineId: 'default',
      cwd: process.cwd(),
    })
    await managed.client.killWorkerSession({ workerSessionId: params.workerSessionId })
  }

  async sendLoadedSessionMessage(options: {
    sessionId: string
    loadSessionId: string
    machineId: string
    prompt: string
    cwd: string
    env?: Record<string, string | undefined>
  }): Promise<void> {
    const temp = await this.createLoadedSession(options.loadSessionId, {
      sessionId: options.sessionId,
      machineId: options.machineId,
      cwd: options.cwd,
      env: options.env,
    })
    try {
      this.sessions.set(options.sessionId, temp)
      await this.streamTurn(temp, options.prompt, options.sessionId)
    } finally {
      this.sessions.delete(options.sessionId)
      await temp.sdkSession.close()
    }
  }

  disposeSession(sessionId: string): void {
    const managed = this.sessions.get(sessionId)
    if (!managed) return
    this.sessions.delete(sessionId)
    managed.pendingPermission?.reject(new Error('Session disposed'))
    managed.pendingAskUser?.reject(new Error('Session disposed'))
    void managed.sdkSession.close()
  }

  disposeAll(): number {
    const sessionIds = [...this.sessions.keys()]
    for (const sessionId of sessionIds) this.disposeSession(sessionId)
    return sessionIds.length
  }

  private async getOrCreateSession(
    settings: SessionSettingsSnapshot & { sessionId: string; resumeSessionId?: string },
  ): Promise<ManagedSession> {
    const existing = this.sessions.get(settings.sessionId)
    if (existing) return existing
    if (settings.resumeSessionId) {
      const loaded = await this.createLoadedSession(settings.resumeSessionId, settings)
      this.sessions.set(settings.sessionId, loaded)
      return loaded
    }
    const created = await this.createManagedSession(settings)
    this.sessions.set(created.sessionId, created)
    return created
  }

  private async createManagedSession(settings: SessionSettingsSnapshot): Promise<ManagedSession> {
    const transport = new ProcessTransport({
      execPath: this.opts.droidPath || resolveDroidPath(),
      cwd: settings.cwd,
      env: normalizeExecEnv(settings.env),
    })
    await transport.connect()
    const client = new DroidClient({ transport })
    const sessionPlaceholder = { current: '' }
    client.setPermissionHandler((params) =>
      this.handlePermissionRequest(sessionPlaceholder.current, params),
    )
    client.setAskUserHandler((params) =>
      this.handleAskUserRequest(sessionPlaceholder.current, params),
    )
    const initResult = await client.initializeSession({
      machineId: settings.machineId,
      cwd: settings.cwd,
      ...(settings.modelId ? { modelId: settings.modelId } : {}),
      ...(settings.interactionMode
        ? { interactionMode: toSdkInteractionMode(settings.interactionMode) }
        : {}),
      ...(settings.autonomyLevel
        ? { autonomyLevel: toSdkAutonomyLevel(settings.autonomyLevel) }
        : {}),
      ...(settings.reasoningEffort
        ? { reasoningEffort: toSdkReasoningEffort(settings.reasoningEffort) }
        : {}),
      ...(settings.decompSessionType
        ? { decompSessionType: settings.decompSessionType as any }
        : {}),
    } as any)
    const sessionId = String((initResult as any).sessionId || '').trim()
    if (!sessionId) {
      await client.close()
      throw new Error('SDK initializeSession returned no sessionId')
    }
    sessionPlaceholder.current = sessionId
    return {
      sessionId,
      client,
      sdkSession: new DroidSession(client, sessionId, initResult),
      initResult,
      settings,
    }
  }

  private async createLoadedSession(
    sessionId: string,
    settings: SessionSettingsSnapshot & { sessionId: string },
  ): Promise<ManagedSession> {
    const transport = new ProcessTransport({
      execPath: this.opts.droidPath || resolveDroidPath(),
      cwd: settings.cwd,
      env: normalizeExecEnv(settings.env),
    })
    await transport.connect()
    const client = new DroidClient({ transport })
    client.setPermissionHandler((params) =>
      this.handlePermissionRequest(settings.sessionId, params),
    )
    client.setAskUserHandler((params) => this.handleAskUserRequest(settings.sessionId, params))
    const loadResult = await client.loadSession({ sessionId })
    return {
      sessionId: settings.sessionId,
      client,
      sdkSession: new DroidSession(client, settings.sessionId, loadResult),
      initResult: loadResult,
      settings,
    }
  }

  private async streamTurn(
    managed: ManagedSession,
    prompt: string,
    emitSessionId: string,
  ): Promise<void> {
    let sawTurnComplete = false
    try {
      for await (const message of managed.sdkSession.stream(prompt)) {
        this.handleStreamMessage(emitSessionId, message)
        if (message.type === 'turn_complete') {
          sawTurnComplete = true
        }
      }
      if (!sawTurnComplete) this.opts.emit({ type: 'turn-end', sessionId: emitSessionId, code: 0 })
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      this.opts.emit({ type: 'error', sessionId: emitSessionId, message })
      this.opts.emit({ type: 'turn-end', sessionId: emitSessionId, code: 1 })
      throw error
    }
  }

  private handleStreamMessage(sessionId: string, message: DroidMessage): void {
    if (message.type === 'turn_complete') {
      this.opts.emit({ type: 'turn-end', sessionId, code: 0 })
      return
    }
    if (message.type === 'error') {
      this.opts.emit({ type: 'error', sessionId, message: message.message })
      return
    }
    this.opts.emit({ type: 'message', sessionId, message })
  }

  private handlePermissionRequest(
    sessionId: string,
    params: Record<string, unknown>,
  ): Promise<ToolConfirmationOutcome> {
    const managed = this.sessions.get(sessionId)
    if (!managed) return Promise.resolve('cancel' as ToolConfirmationOutcome)
    if (managed.pendingPermission) {
      managed.pendingPermission.resolve('cancel' as ToolConfirmationOutcome)
      managed.pendingPermission = undefined
    }

    const request = buildPermissionRequestPayload(params)
    this.opts.emit({ type: 'permission-request', sessionId, request })
    return new Promise<ToolConfirmationOutcome>((resolve, reject) => {
      managed.pendingPermission = { requestKey: request.requestKey, resolve, reject }
    })
  }

  private handleAskUserRequest(
    sessionId: string,
    params: AskUserRequestParams | Record<string, unknown>,
  ): Promise<{ cancelled?: boolean; answers: AskUserCollectedAnswer[] }> {
    const managed = this.sessions.get(sessionId)
    if (!managed) return Promise.resolve({ cancelled: true, answers: [] })
    if (managed.pendingAskUser) {
      managed.pendingAskUser.resolve({ cancelled: true, answers: [] })
      managed.pendingAskUser = undefined
    }

    const request = buildAskUserRequestPayload(params)
    this.opts.emit({ type: 'ask-user-request', sessionId, request })
    return new Promise<{ cancelled?: boolean; answers: AskUserCollectedAnswer[] }>(
      (resolve, reject) => {
        managed.pendingAskUser = { requestKey: request.requestKey, resolve, reject }
      },
    )
  }
}
