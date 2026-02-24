import { spawn } from 'child_process'
import { dirname, isAbsolute } from 'path'
import type { ChildProcessWithoutNullStreams } from 'child_process'
import { JsonRpcLineParser } from './jsonRpcLineParser.ts'
import {
  FACTORY_API_VERSION,
  JSONRPC_VERSION,
  type DroidAutonomyLevel,
  type DroidInteractionMode,
  type JsonRpcMessage,
  type JsonRpcNotification,
  type JsonRpcRequest,
  type JsonRpcResponse,
} from './jsonRpcTypes.ts'
import { formatNotificationTrace, isTraceChainEnabled } from './notificationFingerprint.ts'
import type { LocalDiagnostics } from '../../diagnostics/localDiagnostics.ts'

export type DroidRpcSessionEvent =
  | { type: 'stdout'; data: string }
  | { type: 'stderr'; data: string }
  | { type: 'rpc-notification'; message: JsonRpcNotification }
  | { type: 'rpc-request'; message: JsonRpcRequest }
  | { type: 'turn-end'; code: number }
  | { type: 'error'; message: string }
  | { type: 'debug'; message: string }

export interface DroidRpcSessionOptions {
  droidPath: string
  sessionId: string
  cwd: string
  machineId: string
  env: Record<string, string | undefined>
  diagnostics?: LocalDiagnostics
  onEvent: (ev: DroidRpcSessionEvent) => void
}

type PendingRequest = {
  method: string
  createdAt: number
  resolve: (res: JsonRpcResponse) => void
  reject: (err: Error) => void
  timer: NodeJS.Timeout
}

const ENGINE_SESSION_ID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

function isEngineSessionId(value: string): boolean {
  return ENGINE_SESSION_ID_RE.test(String(value || '').trim())
}

function safeStringify(value: unknown): string {
  try {
    return JSON.stringify(value)
  } catch {
    return '"[unserializable]"'
  }
}

function clipString(value: unknown, maxLen = 512): string {
  const s = String(value ?? '')
  if (s.length <= maxLen) return s
  return s.slice(0, Math.max(0, maxLen - 1)) + '…'
}

function summarizeSessionNotification(notification: any): Record<string, unknown> | undefined {
  const type = typeof notification?.type === 'string' ? notification.type : ''
  if (!type) return undefined

  if (type === 'settings_updated') {
    const s =
      notification?.settings && typeof notification.settings === 'object'
        ? notification.settings
        : null
    if (!s) return { type }
    return {
      type,
      settings: {
        modelId: typeof s.modelId === 'string' ? s.modelId : undefined,
        reasoningEffort: typeof s.reasoningEffort === 'string' ? s.reasoningEffort : undefined,
        autonomyLevel: typeof s.autonomyLevel === 'string' ? s.autonomyLevel : undefined,
        specModeReasoningEffort:
          typeof s.specModeReasoningEffort === 'string' ? s.specModeReasoningEffort : undefined,
      },
    }
  }

  if (type === 'tool_use') {
    const input =
      notification?.input && typeof notification.input === 'object' ? notification.input : null
    const cmd = input ? (input as any).command : undefined
    return {
      type,
      id: typeof notification.id === 'string' ? notification.id : undefined,
      name: typeof notification.name === 'string' ? notification.name : undefined,
      inputKeys: input ? Object.keys(input as any).slice(0, 50) : undefined,
      commandPreview:
        typeof cmd === 'string' && cmd.trim() ? clipString(cmd.trim(), 256) : undefined,
    }
  }

  if (type === 'tool_result') {
    const content = notification?.content
    const toolUseId = typeof notification?.toolUseId === 'string' ? notification.toolUseId : ''
    return {
      type,
      toolUseId: toolUseId || undefined,
      isError: typeof notification?.isError === 'boolean' ? notification.isError : undefined,
      contentType: content === null ? 'null' : Array.isArray(content) ? 'array' : typeof content,
      contentPreview:
        typeof content === 'string' && content.trim() ? clipString(content.trim(), 256) : undefined,
    }
  }

  if (type === 'create_message') {
    const msg =
      notification?.message && typeof notification.message === 'object'
        ? notification.message
        : null
    const content = Array.isArray((msg as any)?.content) ? (msg as any).content : []
    const contentTypes: string[] = []
    const toolUses: Array<{ id: string; name: string }> = []
    let textLen = 0
    for (const item of content) {
      const t = typeof (item as any)?.type === 'string' ? String((item as any).type) : ''
      if (t) contentTypes.push(t)
      if (t === 'text' && typeof (item as any)?.text === 'string')
        textLen += String((item as any).text).length
      if (t === 'tool_use') {
        const id = typeof (item as any)?.id === 'string' ? String((item as any).id) : ''
        const name = typeof (item as any)?.name === 'string' ? String((item as any).name) : ''
        if (id || name) toolUses.push({ id, name })
      }
    }
    return {
      type,
      message: msg
        ? {
            id: typeof (msg as any).id === 'string' ? (msg as any).id : undefined,
            role: typeof (msg as any).role === 'string' ? (msg as any).role : undefined,
            contentTypes: contentTypes.length ? contentTypes.slice(0, 50) : undefined,
            toolUses: toolUses.length ? toolUses.slice(0, 50) : undefined,
            textLen: textLen || undefined,
          }
        : undefined,
    }
  }

  if (type === 'error') {
    return { type, message: clipString(notification?.message, 256) }
  }

  if (type === 'mcp_auth_required') {
    return {
      type,
      serverName:
        typeof notification?.serverName === 'string' ? notification.serverName : undefined,
      authUrl: typeof notification?.authUrl === 'string' ? notification.authUrl : undefined,
    }
  }

  return { type }
}

function summarizeInboundRequest(message: JsonRpcRequest): Record<string, unknown> {
  const method = typeof (message as any)?.method === 'string' ? String((message as any).method) : ''
  const id = typeof (message as any)?.id === 'string' ? String((message as any).id) : ''
  const params = (message as any)?.params

  if (method === 'droid.request_permission') {
    const toolUsesRaw = (params as any)?.toolUses
    const toolUses = Array.isArray(toolUsesRaw) ? toolUsesRaw : []
    const tools: Array<{ id: string; name: string }> = []
    for (const item of toolUses) {
      const tu = (item as any)?.toolUse || item
      if (!tu || typeof tu !== 'object') continue
      const tid = typeof (tu as any)?.id === 'string' ? String((tu as any).id) : ''
      const name = typeof (tu as any)?.name === 'string' ? String((tu as any).name) : ''
      if (tid || name) tools.push({ id: tid, name })
    }
    return {
      method,
      id: id || undefined,
      toolCount: tools.length,
      tools: tools.length ? tools.slice(0, 50) : undefined,
    }
  }

  const keys =
    params && typeof params === 'object' ? Object.keys(params as any).slice(0, 50) : undefined
  return {
    method,
    id: id || undefined,
    paramsKeys: keys && keys.length ? keys : undefined,
  }
}

export class DroidJsonRpcSession {
  private readonly opts: DroidRpcSessionOptions
  private proc: ChildProcessWithoutNullStreams | null = null
  private readonly parser = new JsonRpcLineParser()
  private requestSeq = 0
  private pending = new Map<string, PendingRequest>()
  private initialized = false
  private turnActive = false
  private engineSessionId: string | null = null
  private initInteractionMode: DroidInteractionMode | undefined = undefined

  constructor(opts: DroidRpcSessionOptions) {
    this.opts = opts
  }

  start(): void {
    if (this.proc) return

    const args = [
      'exec',
      '--input-format',
      'stream-jsonrpc',
      '--output-format',
      'stream-jsonrpc',
      '--cwd',
      this.opts.cwd,
    ]

    this.opts.onEvent({
      type: 'debug',
      message: `spawn: ${this.opts.droidPath} ${args.map((a) => JSON.stringify(a)).join(' ')}`,
    })

    const env = { ...process.env, ...(this.opts.env || {}) }
    // Ensure sub-processes (e.g. Task subagents) can resolve `droid` on PATH even when
    // the parent was launched via an absolute path (common for GUI apps).
    try {
      const droidPath = this.opts.droidPath
      const looksLikePath =
        isAbsolute(droidPath) || droidPath.includes('/') || droidPath.includes('\\')
      if (looksLikePath) {
        const droidDir = dirname(droidPath)
        const pathKey =
          Object.keys(env).find((k) => k.toUpperCase() === 'PATH') ||
          (process.platform === 'win32' ? 'Path' : 'PATH')
        const sep = process.platform === 'win32' ? ';' : ':'
        const existing = String((env as any)[pathKey] || '')
        const parts = existing.split(sep).filter(Boolean)
        if (!parts.includes(droidDir)) (env as any)[pathKey] = [droidDir, ...parts].join(sep)
      }
    } catch {
      /* ignore */
    }

    const proc = spawn(this.opts.droidPath, args, {
      env,
      cwd: this.opts.cwd,
      stdio: ['pipe', 'pipe', 'pipe'],
    })
    this.proc = proc

    proc.stdout.on('data', (chunk: Buffer) => {
      const parsed = this.parser.push(chunk)
      for (const item of parsed) {
        if (item.kind === 'stdout') {
          this.opts.onEvent({ type: 'stdout', data: item.data })
          continue
        }
        this.handleMessage(item.message)
      }
    })

    proc.stderr.on('data', (chunk: Buffer) => {
      const data = chunk.toString()
      this.opts.onEvent({ type: 'stderr', data })
      const trimmed = data.trim()
      if (trimmed) this.opts.onEvent({ type: 'debug', message: `stderr: ${trimmed}` })
    })

    proc.on('close', (code) => {
      for (const item of this.parser.flush()) {
        if (item.kind === 'stdout') this.opts.onEvent({ type: 'stdout', data: item.data })
        else this.handleMessage(item.message)
      }
      const exitCode = typeof code === 'number' ? code : 0
      this.opts.onEvent({ type: 'debug', message: `close: code=${exitCode}` })

      const err = new Error(`droid process exited (code ${exitCode})`)
      for (const pending of this.pending.values()) {
        clearTimeout(pending.timer)
        pending.reject(err)
      }
      this.pending.clear()
      this.proc = null
      this.initialized = false
      this.engineSessionId = null
      this.initInteractionMode = undefined
      if (this.turnActive) {
        this.turnActive = false
        this.opts.onEvent({ type: 'turn-end', code: typeof code === 'number' ? code : 1 })
      }
      if (exitCode !== 0) this.opts.onEvent({ type: 'error', message: err.message })
    })

    proc.on('error', (err) => {
      this.opts.onEvent({ type: 'error', message: (err as Error).message })
    })
  }

  async ensureInitialized(
    params: {
      modelId?: string
      interactionMode?: DroidInteractionMode
      autonomyLevel?: DroidAutonomyLevel
      reasoningEffort?: string
    },
    resumeSessionId?: string,
  ): Promise<{
    engineSessionId: string
    source: 'init' | 'resume' | 'resume_failed' | 'resume_invalid'
  }> {
    this.start()
    if (this.initialized) return { engineSessionId: this.engineSessionId || '', source: 'resume' }

    const initRes = await this.sendRequest('droid.initialize_session', {
      machineId: this.opts.machineId,
      cwd: this.opts.cwd,
      modelId: params.modelId,
      interactionMode: params.interactionMode,
      autonomyLevel: params.autonomyLevel,
      reasoningEffort: params.reasoningEffort || undefined,
    })
    if (initRes.error) {
      const msg = initRes.error.message || 'initialize_session failed'
      throw new Error(msg)
    }

    const initEngineSessionId = String((initRes as any)?.result?.sessionId || '').trim()
    if (!initEngineSessionId) throw new Error('initialize_session did not return sessionId')

    const resume = String(resumeSessionId || '').trim()
    let effectiveEngineSessionId = initEngineSessionId
    let source: 'init' | 'resume' | 'resume_failed' | 'resume_invalid' = 'init'

    if (resume && resume !== initEngineSessionId) {
      if (!isEngineSessionId(resume)) {
        this.opts.onEvent({
          type: 'debug',
          message: `ensureInitialized: skip load_session (invalid engine session id) resume=${resume}`,
        })
        source = 'resume_invalid'
      } else {
        try {
          this.opts.onEvent({
            type: 'debug',
            message: `ensureInitialized: load_session start resume=${resume}`,
          })
          const loadRes = await this.sendRequest('droid.load_session', { sessionId: resume })
          if (loadRes.error) {
            this.opts.onEvent({
              type: 'debug',
              message: `ensureInitialized: load_session failed error=${loadRes.error.message || 'unknown'}`,
            })
            source = 'resume_failed'
          } else {
            this.opts.onEvent({
              type: 'debug',
              message: `ensureInitialized: load_session ok resume=${resume}`,
            })
            effectiveEngineSessionId = resume
            source = 'resume'
          }
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err)
          this.opts.onEvent({
            type: 'debug',
            message: `ensureInitialized: load_session threw error=${msg}`,
          })
          source = 'resume_failed'
        }
      }
    }
    this.initialized = true
    this.engineSessionId = effectiveEngineSessionId
    this.initInteractionMode = params.interactionMode
    return { engineSessionId: effectiveEngineSessionId, source }
  }

  isInitialized(): boolean {
    return this.initialized
  }

  isTurnActive(): boolean {
    return this.turnActive
  }

  getEngineSessionId(): string | null {
    return this.engineSessionId
  }

  getInitInteractionMode(): DroidInteractionMode | undefined {
    return this.initInteractionMode
  }

  async updateSettings(params: {
    modelId?: string
    autonomyLevel?: DroidAutonomyLevel
    reasoningEffort?: string
  }): Promise<void> {
    const res = await this.sendRequest('droid.update_session_settings', {
      modelId: params.modelId,
      autonomyLevel: params.autonomyLevel,
      reasoningEffort: params.reasoningEffort || undefined,
    })
    if (res.error) throw new Error(res.error.message || 'update_session_settings failed')
  }

  async addUserMessage(params: { text: string; messageId?: string }): Promise<void> {
    this.turnActive = true
    const res = await this.sendRequest('droid.add_user_message', {
      text: params.text,
      messageId: params.messageId,
    })
    if (res.error) throw new Error(res.error.message || 'add_user_message failed')
  }

  async interrupt(): Promise<void> {
    try {
      const res = await this.sendRequest('droid.interrupt_session', {})
      if (res.error) throw new Error(res.error.message || 'interrupt_session failed')
    } catch {
      // ignore
    }
  }

  sendResponse(requestId: string, result: unknown): void {
    const proc = this.proc
    if (!proc) return
    const msg = {
      jsonrpc: JSONRPC_VERSION,
      factoryApiVersion: FACTORY_API_VERSION,
      type: 'response',
      id: requestId,
      result,
    }
    proc.stdin.write(`${safeStringify(msg)}\n`)
  }

  dispose(): void {
    if (!this.proc) return
    try {
      this.proc.kill('SIGTERM')
    } catch {
      // ignore
    }
    this.proc = null
    this.initialized = false
    this.turnActive = false
    this.engineSessionId = null
    this.initInteractionMode = undefined
  }

  private nextId(): string {
    this.requestSeq += 1
    return `${this.opts.sessionId}:${this.requestSeq}`
  }

  private sendRequest(method: string, params?: unknown): Promise<JsonRpcResponse> {
    const proc = this.proc
    if (!proc) return Promise.reject(new Error('droid process not started'))

    const id = this.nextId()
    const req: JsonRpcRequest = {
      jsonrpc: JSONRPC_VERSION,
      factoryApiVersion: FACTORY_API_VERSION,
      type: 'request',
      id,
      method,
      params,
    }

    // Structured RPC request logging (redacted).
    if (this.opts.diagnostics?.isEnabled()) {
      const diag = this.opts.diagnostics
      const payload: Record<string, unknown> = { method, id }
      const p = params as any
      if (p && typeof p === 'object' && typeof p.text === 'string') {
        const sig = diag.computePromptSig(p.text)
        payload.textSig = sig
        const last = diag.getLastInputPromptSig(this.opts.sessionId)
        if (last && last.promptSha256 !== sig.promptSha256) {
          void diag.append({
            ts: new Date().toISOString(),
            level: 'warn',
            scope: 'backend',
            event: 'anomaly.prompt_tail_missing',
            sessionId: this.opts.sessionId,
            correlation: { rpcId: id, method },
            data: { inputPromptSig: last, rpcTextSig: sig },
          })
        }
      }
      void diag.append({
        ts: new Date().toISOString(),
        level: 'debug',
        scope: 'droid-rpc',
        event: 'rpc.request',
        sessionId: this.opts.sessionId,
        correlation: { rpcId: id },
        data: payload,
      })
    }

    const timeoutMs = 30_000
    return new Promise<JsonRpcResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        this.opts.onEvent({ type: 'debug', message: `timeout: ${method} id=${id}` })
        reject(new Error(`JSON-RPC request timeout: ${method}`))
      }, timeoutMs)
      this.pending.set(id, { method, createdAt: Date.now(), resolve, reject, timer })
      this.opts.onEvent({
        type: 'debug',
        message: `request: ${method} id=${id} params=${safeStringify(params)}`,
      })
      proc.stdin.write(`${safeStringify(req)}\n`)
    })
  }

  private handleMessage(message: JsonRpcMessage): void {
    if (message.type === 'response') {
      const id = message.id
      const resolvePending = (
        pendingId: string,
        pending: PendingRequest,
        originalIdWasNull: boolean,
      ) => {
        clearTimeout(pending.timer)
        this.pending.delete(pendingId)
        const hasError = Boolean((message as any)?.error)
        this.opts.onEvent({
          type: 'debug',
          message: `response: id=${pendingId} error=${hasError ? safeStringify((message as any)?.error) : 'null'}`,
        })
        if (this.opts.diagnostics?.isEnabled()) {
          void this.opts.diagnostics.append({
            ts: new Date().toISOString(),
            level: hasError ? 'warn' : 'debug',
            scope: 'droid-rpc',
            event: 'rpc.response',
            sessionId: this.opts.sessionId,
            correlation: { rpcId: pendingId },
            data: {
              hasError,
              error: hasError ? (message as any)?.error : undefined,
              method: pending.method,
              originalIdWasNull: originalIdWasNull || undefined,
            },
          })
        }
        pending.resolve({ ...(message as any), id: pendingId } as JsonRpcResponse)
      }

      if (typeof id === 'string') {
        const pending = this.pending.get(id)
        if (pending) resolvePending(id, pending, false)
      } else if (id === null) {
        const last = Array.from(this.pending.entries()).at(-1) || null
        if (last) {
          const [pendingId, pending] = last
          this.opts.onEvent({
            type: 'debug',
            message: `response: id=null; associating with pending id=${pendingId} method=${pending.method}`,
          })
          resolvePending(pendingId, pending, true)
        }
      }
      return
    }

    if (message.type === 'request') {
      this.opts.onEvent({
        type: 'debug',
        message: `inbound-request: ${(message as any)?.method || ''} id=${(message as any)?.id || ''}`,
      })
      if (this.opts.diagnostics?.isEnabled()) {
        void this.opts.diagnostics.append({
          ts: new Date().toISOString(),
          level: 'debug',
          scope: 'droid-rpc',
          event: 'rpc.inbound_request',
          sessionId: this.opts.sessionId,
          data: summarizeInboundRequest(message as JsonRpcRequest),
        })
      }
      this.opts.onEvent({ type: 'rpc-request', message: message as JsonRpcRequest })
      return
    }

    if (message.type === 'notification') {
      const n = message as JsonRpcNotification
      if (isTraceChainEnabled()) {
        this.opts.onEvent({ type: 'debug', message: formatNotificationTrace('session-in', n) })
      }
      if (this.opts.diagnostics?.isEnabled()) {
        const notif =
          n.method === 'droid.session_notification' ? (n.params as any)?.notification || null : null
        const t = notif && typeof notif === 'object' ? String((notif as any)?.type || '') : ''
        const summary =
          notif && typeof notif === 'object' ? summarizeSessionNotification(notif) : undefined
        void this.opts.diagnostics.append({
          ts: new Date().toISOString(),
          level: 'debug',
          scope: 'droid-rpc',
          event: 'rpc.notification',
          sessionId: this.opts.sessionId,
          data: { method: n.method, notifType: t || undefined, summary: summary || undefined },
        })
        if (t === 'tool_use') {
          const name = String((notif as any)?.name || '')
          const input = (notif as any)?.input
          const cmd =
            input && typeof input === 'object' ? String((input as any)?.command || '') : ''
          if (cmd) {
            this.opts.diagnostics.logPipelineExitcodeRisk({
              sessionId: this.opts.sessionId,
              command: cmd,
              toolName: name,
              correlation: { toolUseId: String((notif as any)?.id || '') },
            })
          }
        }
        if (t === 'droid_working_state_changed' || t === 'working_state_changed') {
          void this.opts.diagnostics.append({
            ts: new Date().toISOString(),
            level: 'debug',
            scope: 'droid-rpc',
            event: 'working_state',
            sessionId: this.opts.sessionId,
            data: { newState: String((notif as any)?.newState || '') },
          })
        }
      }
      if (n.method === 'droid.session_notification') {
        const t = (n.params as any)?.notification?.type
        this.opts.onEvent({
          type: 'debug',
          message: `notification: ${n.method} type=${typeof t === 'string' ? t : ''}`,
        })
      } else {
        this.opts.onEvent({ type: 'debug', message: `notification: ${n.method}` })
      }
      if (isTraceChainEnabled()) {
        this.opts.onEvent({ type: 'debug', message: formatNotificationTrace('session-out', n) })
      }
      this.opts.onEvent({ type: 'rpc-notification', message: n })

      if (n.method === 'droid.session_notification') {
        const notif = (n.params as any)?.notification
        const t = (notif as any)?.type
        if (t === 'droid_working_state_changed' || t === 'working_state_changed') {
          const newState = String((notif as any)?.newState || '')
            .trim()
            .toLowerCase()
          if (newState && newState !== 'idle') this.turnActive = true
          if (newState === 'idle' && this.turnActive) {
            this.turnActive = false
            this.opts.onEvent({ type: 'turn-end', code: 0 })
          }
        } else if (this.turnActive && t === 'error') {
          const msg = String((notif as any)?.message || 'Unknown error')
          this.turnActive = false
          this.opts.onEvent({ type: 'error', message: msg })
          this.opts.onEvent({ type: 'turn-end', code: 1 })
        }
      }
    }
  }
}
