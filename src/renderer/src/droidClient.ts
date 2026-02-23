import type {
  DroidClientAPI,
  GitToolsInfo,
  GenerateCommitMetaRequest,
  GenerateCommitMetaResult,
  CommitWorkflowRequest,
  CommitWorkflowResult,
  PersistedAppState,
  ProjectSettings,
  RemoveWorktreeResult,
  PushBranchResult,
  SaveSessionRequest,
  LoadSessionResponse,
  SessionMeta,
  JsonRpcNotification,
  JsonRpcRequest,
  SlashCommandDef,
  SlashResolveResult,
  SkillDef,
  SetupScriptEvent,
  WorkspaceInfo,
  WorkspaceCreateParams,
} from '@/types'

type RpcNotificationCallback = (payload: {
  message: JsonRpcNotification
  sessionId: string | null
}) => void
type RpcRequestCallback = (payload: { message: JsonRpcRequest; sessionId: string | null }) => void
type TurnEndCallback = (payload: { code: number; sessionId: string | null }) => void
type DebugCallback = (payload: { message: string; sessionId: string | null }) => void
type StdoutCallback = (payload: { data: string; sessionId: string | null }) => void
type StderrCallback = (payload: { data: string; sessionId: string | null }) => void
type ErrorCallback = (payload: { message: string; sessionId: string | null }) => void
type SetupScriptEventCallback = (payload: {
  event: SetupScriptEvent
  sessionId: string | null
}) => void
type SessionIdReplacedCallback = (payload: {
  oldSessionId: string
  newSessionId: string
  reason: string
}) => void

const rpcNotificationListeners: Set<RpcNotificationCallback> = new Set()
const rpcRequestListeners: Set<RpcRequestCallback> = new Set()
const turnEndListeners: Set<TurnEndCallback> = new Set()
const debugListeners: Set<DebugCallback> = new Set()
const stdoutListeners: Set<StdoutCallback> = new Set()
const stderrListeners: Set<StderrCallback> = new Set()
const errorListeners: Set<ErrorCallback> = new Set()
const setupScriptEventListeners: Set<SetupScriptEventCallback> = new Set()
const sessionIdReplacedListeners: Set<SessionIdReplacedCallback> = new Set()

export function getApiBase(): string {
  const envBase = (import.meta as any)?.env?.VITE_DROID_API_BASE as string | undefined
  if (envBase && typeof envBase === 'string') return envBase.replace(/\/+$/, '')

  try {
    const isElectron = Boolean((window as any).droid)
    if (!isElectron) {
      return `${window.location.origin}/api`
    }
  } catch {
    // ignore
  }

  return 'http://localhost:3001/api'
}

export function getServerOrigin(): string {
  const base = getApiBase()
  return base.endsWith('/api') ? base.slice(0, -4) : base
}

function emitRpcNotification(message: JsonRpcNotification, sessionId: string | null) {
  rpcNotificationListeners.forEach((cb) => cb({ message, sessionId }))
}
function emitRpcRequest(message: JsonRpcRequest, sessionId: string | null) {
  rpcRequestListeners.forEach((cb) => cb({ message, sessionId }))
}
function emitTurnEnd(code: number, sessionId: string | null) {
  turnEndListeners.forEach((cb) => cb({ code, sessionId }))
}
function emitDebug(message: string, sessionId: string | null) {
  debugListeners.forEach((cb) => cb({ message, sessionId }))
}
function emitStdout(data: string, sessionId: string | null) {
  stdoutListeners.forEach((cb) => cb({ data, sessionId }))
}
function emitStderr(data: string, sessionId: string | null) {
  stderrListeners.forEach((cb) => cb({ data, sessionId }))
}
function emitError(message: string, sessionId: string | null) {
  errorListeners.forEach((cb) => cb({ message, sessionId }))
}
function emitSetupScriptEvent(event: SetupScriptEvent, sessionId: string | null) {
  setupScriptEventListeners.forEach((cb) => cb({ event, sessionId }))
}

function emitSessionIdReplaced(payload: {
  oldSessionId: string
  newSessionId: string
  reason: string
}) {
  sessionIdReplacedListeners.forEach((cb) => cb(payload))
}

function handleStreamPayload(payload: string, sessionId: string | null) {
  const sid = sessionId && typeof sessionId === 'string' ? sessionId : null
  if (sid) touchStream(sid)

  try {
    const msg = JSON.parse(payload)
    if (sid) {
      if (msg.type === 'rpc-notification') {
        const n = msg.message
        const notif =
          n && typeof n === 'object' && n.method === 'droid.session_notification'
            ? (n.params as any)?.notification
            : null
        const t = (notif as any)?.type
        if (t === 'droid_working_state_changed' || t === 'working_state_changed') {
          const newState = String((notif as any)?.newState || '')
            .trim()
            .toLowerCase()
          if (newState && newState !== 'idle') markStreamRunning(sid)
          else if (newState === 'idle') markStreamIdle(sid)
        }
      } else if (msg.type === 'turn-end') {
        markStreamIdle(sid)
      }
    }

    if (msg.type === 'stdout') {
      emitStdout(String(msg.data || ''), sessionId)
    } else if (msg.type === 'stderr') {
      emitStderr(String(msg.data || ''), sessionId)
    } else if (msg.type === 'rpc-notification') {
      emitRpcNotification(msg.message as JsonRpcNotification, sessionId)
    } else if (msg.type === 'rpc-request') {
      emitRpcRequest(msg.message as JsonRpcRequest, sessionId)
    } else if (msg.type === 'turn-end') {
      emitTurnEnd(Number(msg.code) || 0, sessionId)
    } else if (msg.type === 'debug') {
      emitDebug(String(msg.message || ''), sessionId)
    } else if (msg.type === 'error') {
      emitError(String(msg.message || ''), sessionId)
    } else if (msg.type === 'setup-script-event') {
      const event = msg.event as SetupScriptEvent | undefined
      if (!event || typeof event !== 'object') return
      emitSetupScriptEvent(event, sessionId)
    } else if (msg.type === 'session-id-replaced') {
      const oldSessionId = String(msg.oldSessionId || '').trim()
      const newSessionId = String(msg.newSessionId || '').trim()
      const reason = String(msg.reason || '').trim() || 'unknown'
      if (oldSessionId && newSessionId)
        emitSessionIdReplaced({ oldSessionId, newSessionId, reason })
    }
  } catch {
    // ignore
  }
}

function parseSseLines(text: string, onData: (dataLine: string) => void): string {
  const lines = text.split('\n')
  // Keep the last partial line in the buffer
  const remainder = lines.pop() || ''
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed.startsWith('data:')) continue
    const payload = trimmed.slice(5).trim()
    if (!payload) continue
    onData(payload)
  }
  return remainder
}

async function apiFetch(input: string, init?: RequestInit): Promise<Response> {
  return await fetch(input, init)
}

async function readApiError(res: Response): Promise<string> {
  try {
    const data = await res.json()
    if (data && typeof data === 'object' && typeof (data as any)?.error === 'string') {
      return String((data as any).error || '').trim() || `HTTP ${res.status}: ${res.statusText}`
    }
  } catch {
    // ignore
  }
  return `HTTP ${res.status}: ${res.statusText}`
}

const STREAM_RECONNECT_DELAYS_MS = [500, 1000, 2000, 5000, 10000]
const STREAM_READY_TIMEOUT_MS = 2000
const STREAM_IDLE_CLOSE_DELAY_MS = 5_000
const STREAM_INACTIVE_GC_MS = 2 * 60_000
const STREAM_GC_INTERVAL_MS = 30_000

type TimeoutHandle = number | ReturnType<typeof setTimeout>
type IntervalHandle = number | ReturnType<typeof setInterval>

type StreamState = {
  sessionId: string
  abort: AbortController
  ready: Promise<void>
  resolveReady: (() => void) | null
  reconnectAttempt: number
  isRunning: boolean
  lastActivityAt: number
  closeTimer: TimeoutHandle | null
}

let activeBrowserSessionId: string | null = null
const streamBySessionId = new Map<string, StreamState>()
let streamGcInterval: IntervalHandle | null = null

function makeReadyPromise(): { promise: Promise<void>; resolve: () => void } {
  let resolve!: () => void
  const promise = new Promise<void>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

function markStreamNotReady(state: StreamState) {
  const { promise, resolve } = makeReadyPromise()
  state.ready = promise
  state.resolveReady = resolve
}

async function waitForStreamReady(
  sessionId: string,
  timeoutMs = STREAM_READY_TIMEOUT_MS,
): Promise<void> {
  const current = streamBySessionId.get(sessionId)
  if (!current) return

  const timeout = new Promise<void>((resolve) => {
    window.setTimeout(resolve, timeoutMs)
  })

  try {
    await Promise.race([current.ready, timeout])
  } catch {
    // ignore
  }
}

async function runStreamLoop(state: StreamState): Promise<void> {
  const sid = state.sessionId
  const url = `${getApiBase()}/stream?sessionId=${encodeURIComponent(sid)}`
  const decoder = new TextDecoder()

  while (streamBySessionId.get(sid) === state && !state.abort.signal.aborted) {
    try {
      const res = await apiFetch(url, {
        method: 'GET',
        headers: { Accept: 'text/event-stream' },
        signal: state.abort.signal,
      })

      if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}: ${res.statusText}`)

      state.reconnectAttempt = 0
      if (state.resolveReady) {
        state.resolveReady()
        state.resolveReady = null
      }

      const reader = res.body.getReader()
      let buffer = ''

      while (streamBySessionId.get(sid) === state && !state.abort.signal.aborted) {
        const { done, value } = await reader.read()
        if (done) break
        state.lastActivityAt = Date.now()
        buffer += decoder.decode(value, { stream: true })
        buffer = parseSseLines(buffer, (payload) => handleStreamPayload(payload, sid))
      }

      buffer += decoder.decode()
      if (buffer) parseSseLines(buffer, (payload) => handleStreamPayload(payload, sid))
    } catch (err) {
      if (state.abort.signal.aborted || streamBySessionId.get(sid) !== state) break
      emitDebug(
        `stream: disconnected sessionId=${sid} err=${String((err as Error)?.message || err || 'unknown')}`,
        sid,
      )
    }

    if (state.abort.signal.aborted || streamBySessionId.get(sid) !== state) break

    emitDebug(`stream: reconnecting sessionId=${sid}`, sid)
    markStreamNotReady(state)

    const delay =
      STREAM_RECONNECT_DELAYS_MS[
        Math.min(state.reconnectAttempt, STREAM_RECONNECT_DELAYS_MS.length - 1)
      ]
    state.reconnectAttempt += 1
    await new Promise<void>((resolve) => window.setTimeout(resolve, delay))
  }
}

function ensureStreamGcLoop() {
  if (streamGcInterval) return
  streamGcInterval = window.setInterval(() => {
    if (streamBySessionId.size === 0) return
    const now = Date.now()
    for (const [sid, state] of streamBySessionId) {
      if (sid === activeBrowserSessionId) continue
      if (state.isRunning) continue
      if (now - state.lastActivityAt < STREAM_INACTIVE_GC_MS) continue
      disposeBrowserStream(sid)
    }
    if (streamBySessionId.size === 0 && streamGcInterval) {
      window.clearInterval(streamGcInterval)
      streamGcInterval = null
    }
  }, STREAM_GC_INTERVAL_MS)
}

function disposeBrowserStream(sessionId: string) {
  const state = streamBySessionId.get(sessionId)
  if (!state) return
  streamBySessionId.delete(sessionId)
  if (state.closeTimer) window.clearTimeout(state.closeTimer)
  state.closeTimer = null
  try {
    state.abort.abort()
  } catch {
    // ignore
  }
}

function touchStream(sessionId: string) {
  const state = streamBySessionId.get(sessionId)
  if (!state) return
  state.lastActivityAt = Date.now()
}

function markStreamRunning(sessionId: string) {
  const state = streamBySessionId.get(sessionId)
  if (!state) return
  state.isRunning = true
  state.lastActivityAt = Date.now()
  if (state.closeTimer) window.clearTimeout(state.closeTimer)
  state.closeTimer = null
}

function markStreamIdle(sessionId: string) {
  const state = streamBySessionId.get(sessionId)
  if (!state) return
  state.isRunning = false
  state.lastActivityAt = Date.now()

  if (state.closeTimer) window.clearTimeout(state.closeTimer)
  state.closeTimer = null

  if (sessionId === activeBrowserSessionId) return

  state.closeTimer = window.setTimeout(() => {
    const cur = streamBySessionId.get(sessionId)
    if (!cur || cur !== state) return
    if (state.isRunning) return
    if (sessionId === activeBrowserSessionId) return
    disposeBrowserStream(sessionId)
  }, STREAM_IDLE_CLOSE_DELAY_MS)
}

function ensureBrowserStream(sessionId: string): StreamState {
  const existing = streamBySessionId.get(sessionId)
  if (existing) return existing

  const state: StreamState = {
    sessionId,
    abort: new AbortController(),
    ready: Promise.resolve(),
    resolveReady: null,
    reconnectAttempt: 0,
    isRunning: false,
    lastActivityAt: Date.now(),
    closeTimer: null,
  }
  markStreamNotReady(state)
  streamBySessionId.set(sessionId, state)
  ensureStreamGcLoop()
  void runStreamLoop(state)
  return state
}

function setBrowserActiveSession(params: { sessionId: string | null }) {
  const nextSid = params.sessionId || null
  activeBrowserSessionId = nextSid

  if (!nextSid) return

  const state = ensureBrowserStream(nextSid)
  if (state.closeTimer) window.clearTimeout(state.closeTimer)
  state.closeTimer = null
  state.lastActivityAt = Date.now()
}

const browserClient: DroidClientAPI = {
  getVersion: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/version`)
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`)
      const data = await res.json()
      return data.version || 'N/A'
    } catch {
      return 'N/A (browser mode)'
    }
  },

  getAppVersion: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/app-version`)
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`)
      const data = await res.json()
      return data.version || 'N/A'
    } catch {
      return 'N/A (browser mode)'
    }
  },

  exec: async (params) => {
    const sid = params.sessionId || null
    if (!sid) {
      throw new Error('Missing sessionId')
    }

    let wasRunning = false
    try {
      const stream = ensureBrowserStream(sid)
      wasRunning = stream.isRunning
      markStreamRunning(sid)
      await waitForStreamReady(sid)

      const res = await apiFetch(`${getApiBase()}/message`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt: params.prompt,
          sessionId: sid,
          modelId: params.modelId,
          autoLevel: params.autoLevel,
          reasoningEffort: params.reasoningEffort,
        }),
      })

      if (!res.ok) {
        if (!wasRunning) markStreamIdle(sid)
        throw new Error(`HTTP ${res.status}: ${res.statusText}`)
      }
    } catch (err) {
      if (!wasRunning) {
        markStreamIdle(sid)
      }
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to dispatch exec', { cause: err })
    }
  },

  cancel: ({ sessionId }) => {
    apiFetch(`${getApiBase()}/cancel`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId }),
    }).catch(() => {})
  },
  setActiveSession: (params) => {
    setBrowserActiveSession(params)
  },
  updateSessionSettings: async (params) => {
    const res = await apiFetch(`${getApiBase()}/rpc/session-settings`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params),
    })
    if (!res.ok) throw new Error(await readApiError(res))
    return (await res.json()) as { ok: true }
  },

  createSession: async (params) => {
    const cwd = String(params?.cwd || '').trim()
    if (!cwd) throw new Error('Missing cwd')
    const res = await apiFetch(`${getApiBase()}/session/create`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        cwd,
        modelId: params.modelId,
        autoLevel: params.autoLevel,
        reasoningEffort: params.reasoningEffort,
      }),
    })
    if (!res.ok) throw new Error(await readApiError(res))
    const data = await res.json()
    const sessionId = String(data?.sessionId || '').trim()
    if (!sessionId) throw new Error('Missing sessionId in response')
    return { sessionId }
  },

  restartSessionWithActiveKey: async ({ sessionId }) => {
    const sid = String(sessionId || '').trim()
    if (!sid) throw new Error('Missing sessionId')
    const res = await apiFetch(`${getApiBase()}/session/restart`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId: sid }),
    })
    if (!res.ok) throw new Error(await readApiError(res))
    const data = await res.json()
    return {
      ok: true as const,
      apiKeyFingerprint: String(data?.apiKeyFingerprint || ''),
    }
  },

  runSetupScript: async (params) => {
    const sid = typeof params?.sessionId === 'string' ? params.sessionId : ''
    if (sid) {
      ensureBrowserStream(sid)
      await waitForStreamReady(sid)
    }
    const res = await apiFetch(`${getApiBase()}/session/setup/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params),
    })
    if (!res.ok) throw new Error(await readApiError(res))
    return (await res.json()) as { ok: true }
  },

  cancelSetupScript: ({ sessionId }) => {
    apiFetch(`${getApiBase()}/session/setup/cancel`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId }),
    }).catch(() => {})
  },

  onSetupScriptEvent: (callback) => {
    setupScriptEventListeners.add(callback)
    return () => {
      setupScriptEventListeners.delete(callback)
    }
  },

  listSlashCommands: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/slash/commands`)
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`)
      return (await res.json()) as SlashCommandDef[]
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to fetch slash commands', { cause: err })
    }
  },

  listSkills: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/skills`)
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`)
      return (await res.json()) as SkillDef[]
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to fetch skills', { cause: err })
    }
  },

  resolveSlashCommand: async ({ text }: { text: string }) => {
    try {
      const res = await apiFetch(`${getApiBase()}/slash/resolve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      })
      if (!res.ok) return { matched: false, expandedText: text } as SlashResolveResult
      return (await res.json()) as SlashResolveResult
    } catch {
      return { matched: false, expandedText: text } as SlashResolveResult
    }
  },

  onRpcNotification: (callback) => {
    rpcNotificationListeners.add(callback)
    return () => {
      rpcNotificationListeners.delete(callback)
    }
  },
  onRpcRequest: (callback) => {
    rpcRequestListeners.add(callback)
    return () => {
      rpcRequestListeners.delete(callback)
    }
  },
  onTurnEnd: (callback) => {
    turnEndListeners.add(callback)
    return () => {
      turnEndListeners.delete(callback)
    }
  },
  onDebug: (callback) => {
    debugListeners.add(callback)
    return () => {
      debugListeners.delete(callback)
    }
  },

  onSessionIdReplaced: (callback) => {
    sessionIdReplacedListeners.add(callback)
    return () => {
      sessionIdReplacedListeners.delete(callback)
    }
  },

  respondPermission: ({ sessionId, requestId, selectedOption }) => {
    apiFetch(`${getApiBase()}/rpc/permission-response`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId, requestId, selectedOption }),
    }).catch(() => {})
  },
  respondAskUser: ({ sessionId, requestId, cancelled, answers }) => {
    apiFetch(`${getApiBase()}/rpc/askuser-response`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId, requestId, cancelled, answers }),
    }).catch(() => {})
  },
  onStdout: (callback) => {
    stdoutListeners.add(callback)
    return () => {
      stdoutListeners.delete(callback)
    }
  },
  onStderr: (callback) => {
    stderrListeners.add(callback)
    return () => {
      stderrListeners.delete(callback)
    }
  },
  onError: (callback) => {
    errorListeners.add(callback)
    return () => {
      errorListeners.delete(callback)
    }
  },

  setApiKey: (apiKey) => {
    apiFetch(`${getApiBase()}/apikey`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ apiKey }),
    }).catch(() => {})
  },
  getApiKey: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/apikey`)
      if (!res.ok) return ''
      const data = await res.json()
      return data.apiKey || ''
    } catch {
      return ''
    }
  },
  listKeys: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/keys`)
      if (!res.ok) return []
      const data = await res.json()
      return data.keys || []
    } catch {
      return []
    }
  },
  addKeys: async (keys) => {
    try {
      const res = await apiFetch(`${getApiBase()}/keys/add`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ keys }),
      })
      if (!res.ok) return { added: 0, duplicates: 0 }
      return await res.json()
    } catch {
      return { added: 0, duplicates: 0 }
    }
  },
  removeKeyByIndex: async (index) => {
    await apiFetch(`${getApiBase()}/keys/remove`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ index }),
    }).catch(() => {})
  },
  updateKeyNote: async (index, note) => {
    await apiFetch(`${getApiBase()}/keys/note`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ index, note }),
    }).catch(() => {})
  },
  refreshKeys: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/keys/refresh`, { method: 'POST' })
      if (!res.ok) return []
      const data = await res.json()
      return data.keys || []
    } catch {
      return []
    }
  },
  getActiveKeyInfo: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/keys/active`)
      if (!res.ok) return { key: '', apiKeyFingerprint: '' }
      return await res.json()
    } catch {
      return { key: '', apiKeyFingerprint: '' }
    }
  },
  setTraceChainEnabled: (enabled) => {
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ traceChainEnabled: Boolean(enabled) }),
    }).catch(() => {})
  },
  setShowDebugTrace: (enabled) => {
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ showDebugTrace: Boolean(enabled) }),
    }).catch(() => {})
  },
  setDebugTraceMaxLines: (maxLines) => {
    const v =
      maxLines === null
        ? null
        : typeof maxLines === 'number' && Number.isFinite(maxLines)
          ? Math.min(10_000, Math.max(1, Math.floor(maxLines)))
          : null
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ debugTraceMaxLines: v }),
    }).catch(() => {})
  },
  setLocalDiagnosticsEnabled: (enabled) => {
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ localDiagnosticsEnabled: Boolean(enabled) }),
    }).catch(() => {})
  },
  setLanAccessEnabled: (enabled) => {
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ lanAccessEnabled: Boolean(enabled) }),
    }).catch(() => {})
  },
  setLocalDiagnosticsRetention: ({ retentionDays, maxTotalMb }) => {
    const days =
      typeof retentionDays === 'number' && Number.isFinite(retentionDays)
        ? Math.max(1, Math.floor(retentionDays))
        : null
    const mb =
      typeof maxTotalMb === 'number' && Number.isFinite(maxTotalMb)
        ? Math.max(1, Math.floor(maxTotalMb))
        : null
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        localDiagnosticsRetentionDays: days,
        localDiagnosticsMaxTotalMb: mb,
      }),
    }).catch(() => {})
  },
  appendDiagnosticsEvent: ({ sessionId, event, level, data, correlation }) => {
    apiFetch(`${getApiBase()}/diagnostics/event`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId, event, level, data, correlation }),
    }).catch(() => {})
  },
  getDiagnosticsDir: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/diagnostics/dir`)
      if (!res.ok) return ''
      const data = await res.json()
      return String(data.dir || '')
    } catch {
      return ''
    }
  },
  exportDiagnostics: async ({ sessionId, debugTraceText }) => {
    try {
      const sid = typeof sessionId === 'string' ? sessionId : ''
      const res = await apiFetch(`${getApiBase()}/diagnostics/export`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sessionId: sid || null,
          debugTraceText: typeof debugTraceText === 'string' ? debugTraceText : '',
        }),
      })
      if (!res.ok) throw new Error(await readApiError(res))
      const blob = await res.blob()
      const name = `droi-diagnostics${sid ? `-${sid}` : ''}.zip`
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = name
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(a.href), 1000)
      return { path: '(downloaded)' }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to export diagnostics', { cause: err })
    }
  },
  openPath: async () => ({ ok: true as const }),

  openInEditor: async () => {},
  openWithEditor: async () => {},
  detectEditors: async () => [],
  openDirectory: async () => null,
  openFile: async () => {
    return new Promise<string[] | null>((resolve) => {
      const input = document.createElement('input')
      input.type = 'file'
      input.accept = 'image/*'
      input.multiple = true
      input.style.display = 'none'
      input.onchange = async () => {
        const files = input.files
        if (!files || files.length === 0) {
          resolve(null)
          return
        }
        try {
          const projectDir = await browserClient.getProjectDir()
          if (!projectDir) {
            resolve(null)
            return
          }
          const formData = new FormData()
          for (let i = 0; i < files.length; i++) formData.append('file', files[i])
          const res = await apiFetch(
            `${getApiBase()}/upload?projectDir=${encodeURIComponent(projectDir)}`,
            {
              method: 'POST',
              body: formData,
            },
          )
          if (!res.ok) {
            resolve(null)
            return
          }
          const results: Array<{ name: string; path: string }> = await res.json()
          resolve(results.map((r) => r.path))
        } catch {
          resolve(null)
        }
        input.remove()
      }
      input.oncancel = () => {
        resolve(null)
        input.remove()
      }
      document.body.appendChild(input)
      input.click()
    })
  },
  saveAttachments: async (params) => {
    try {
      const formData = new FormData()
      for (const p of params.sourcePaths) {
        const res = await apiFetch(`${getApiBase()}/file?path=${encodeURIComponent(p)}`)
        if (!res.ok) continue
        const blob = await res.blob()
        const name = p.split('/').pop() || 'file'
        formData.append('file', blob, name)
      }
      const res = await apiFetch(
        `${getApiBase()}/upload?projectDir=${encodeURIComponent(params.projectDir)}`,
        {
          method: 'POST',
          body: formData,
        },
      )
      if (!res.ok) return []
      return await res.json()
    } catch {
      return []
    }
  },
  saveClipboardImage: async (params) => {
    try {
      const blob = new Blob([new Uint8Array(params.data)], { type: params.mimeType })
      const rawName = typeof params.fileName === 'string' ? params.fileName.trim() : ''
      const fallbackExt = params.mimeType.split('/')[1]?.replace('jpeg', 'jpg') || 'png'
      const fileName = rawName || `clipboard-${Date.now()}.${fallbackExt}`
      const formData = new FormData()
      formData.append('file', blob, fileName)
      const res = await apiFetch(
        `${getApiBase()}/upload?projectDir=${encodeURIComponent(params.projectDir)}`,
        {
          method: 'POST',
          body: formData,
        },
      )
      if (!res.ok) return null
      const results: Array<{ name: string; path: string }> = await res.json()
      return results[0] || null
    } catch {
      return null
    }
  },

  setProjectDir: (dir) => {
    apiFetch(`${getApiBase()}/project-dir`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ dir }),
    }).catch(() => {})
  },
  getProjectDir: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/project-dir`)
      if (!res.ok) return ''
      const data = await res.json()
      return data.dir || ''
    } catch {
      return ''
    }
  },

  saveSession: async (req: SaveSessionRequest) => {
    try {
      const res = await apiFetch(`${getApiBase()}/session/save`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
      })
      if (!res.ok) return null
      return (await res.json()) as SessionMeta
    } catch {
      return null
    }
  },
  loadSession: async (id: string) => {
    try {
      const res = await apiFetch(`${getApiBase()}/session/load?id=${encodeURIComponent(id)}`)
      return (await res.json()) as LoadSessionResponse | null
    } catch {
      return null
    }
  },
  clearSession: async (params: { id: string }) => {
    try {
      const res = await apiFetch(`${getApiBase()}/session/clear`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) return null
      return (await res.json()) as SessionMeta
    } catch {
      return null
    }
  },
  listSessions: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/session/list`)
      if (!res.ok) return []
      return (await res.json()) as SessionMeta[]
    } catch {
      return []
    }
  },
  deleteSession: async (id: string) => {
    try {
      const res = await apiFetch(`${getApiBase()}/session/delete`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id }),
      })
      const data = await res.json()
      return Boolean(data.ok)
    } catch {
      return false
    }
  },

  loadAppState: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/app-state`)
      if (!res.ok) return { version: 2, machineId: '' }
      return (await res.json()) as PersistedAppState
    } catch {
      return { version: 2, machineId: '' }
    }
  },
  saveProjects: (projects) => {
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ projects }),
    }).catch(() => {})
  },
  updateProjectSettings: async (params: { repoRoot: string; settings: ProjectSettings }) => {
    try {
      const res = await apiFetch(`${getApiBase()}/project-settings`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) throw new Error(await readApiError(res))
      return (await res.json()) as PersistedAppState
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to update project settings', { cause: err })
    }
  },

  setCommitMessageModelId: (modelId: string) => {
    apiFetch(`${getApiBase()}/app-state`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commitMessageModelId: modelId }),
    }).catch(() => {})
  },
  getGitStatus: async (params) => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-status`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) return []
      return await res.json()
    } catch {
      return []
    }
  },
  getGitBranch: async (params) => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-branch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) return ''
      const data = await res.json()
      return data.branch || ''
    } catch {
      return ''
    }
  },
  listGitBranches: async (params) => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-branches`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) return []
      const data = await res.json()
      return Array.isArray(data.branches) ? data.branches : []
    } catch {
      return []
    }
  },
  listGitWorktreeBranchesInUse: async (params) => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-worktree-branches-in-use`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) return []
      const data = await res.json()
      return Array.isArray(data) ? data : []
    } catch {
      return []
    }
  },
  getWorkspaceInfo: async (params) => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-workspace-info`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) throw new Error(await readApiError(res))
      return (await res.json()) as WorkspaceInfo | null
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to fetch workspace info', { cause: err })
    }
  },
  switchWorkspace: async (params) => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-switch-workspace`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) throw new Error(await readApiError(res))
      return (await res.json()) as WorkspaceInfo | null
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to switch workspace', { cause: err })
    }
  },
  createWorkspace: async (params: WorkspaceCreateParams) => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-create-workspace`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) throw new Error(await readApiError(res))
      return (await res.json()) as WorkspaceInfo | null
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to create workspace', { cause: err })
    }
  },
  removeWorktree: async (params): Promise<RemoveWorktreeResult> => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-remove-worktree`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) throw new Error(await readApiError(res))
      return (await res.json()) as RemoveWorktreeResult
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to remove worktree', { cause: err })
    }
  },
  pushBranch: async (params): Promise<PushBranchResult> => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-push-branch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) throw new Error(await readApiError(res))
      return (await res.json()) as PushBranchResult
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(msg || 'Failed to push branch', { cause: err })
    }
  },

  detectGitTools: async (params: { projectDir: string }): Promise<GitToolsInfo> => {
    try {
      const res = await apiFetch(`${getApiBase()}/git-detect-tools`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params),
      })
      if (!res.ok) return { hasGh: false, hasFlow: false, prTool: null }
      return (await res.json()) as GitToolsInfo
    } catch {
      return { hasGh: false, hasFlow: false, prTool: null }
    }
  },

  generateCommitMeta: async (
    params: GenerateCommitMetaRequest,
  ): Promise<GenerateCommitMetaResult> => {
    const res = await apiFetch(`${getApiBase()}/git-generate-commit-meta`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params),
    })
    if (!res.ok) throw new Error(await readApiError(res))
    return (await res.json()) as GenerateCommitMetaResult
  },

  commitWorkflow: async (params: CommitWorkflowRequest): Promise<CommitWorkflowResult> => {
    const res = await apiFetch(`${getApiBase()}/git-commit-workflow`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params),
    })
    if (!res.ok) throw new Error(await readApiError(res))
    return (await res.json()) as CommitWorkflowResult
  },
  onCommitWorkflowProgress: () => {
    // Browser mode: progress is not streamed from the server API.
    // The final result is returned from commitWorkflow directly.
    return () => {}
  },
  getCustomModels: async () => {
    try {
      const res = await apiFetch(`${getApiBase()}/custom-models`)
      if (!res.ok) return []
      return (await res.json()) as Array<{
        id: string
        displayName: string
        model: string
        provider: string
      }>
    } catch {
      return []
    }
  },
}

export function getDroidClient(): DroidClientAPI {
  // In some dev setups, the preload may expose an older `window.droid` shape.
  // Merge with browserClient so newly-added methods (e.g. slash commands) still work.
  const w = (window as any).droid as Partial<DroidClientAPI> | undefined
  if (w) {
    const merged = Object.assign({}, browserClient, w) as Partial<DroidClientAPI>
    // If the preload defines keys but leaves them undefined (or not functions), keep browser fallbacks.
    if (typeof (merged as any).listSlashCommands !== 'function')
      merged.listSlashCommands = browserClient.listSlashCommands
    if (typeof (merged as any).resolveSlashCommand !== 'function')
      merged.resolveSlashCommand = browserClient.resolveSlashCommand
    if (typeof (merged as any).listSkills !== 'function')
      merged.listSkills = browserClient.listSkills
    if (typeof (merged as any).setActiveSession !== 'function')
      merged.setActiveSession = browserClient.setActiveSession
    if (typeof (merged as any).runSetupScript !== 'function')
      merged.runSetupScript = browserClient.runSetupScript
    if (typeof (merged as any).cancelSetupScript !== 'function')
      merged.cancelSetupScript = browserClient.cancelSetupScript
    if (typeof (merged as any).onSetupScriptEvent !== 'function')
      merged.onSetupScriptEvent = browserClient.onSetupScriptEvent
    if (typeof (merged as any).updateSessionSettings !== 'function')
      merged.updateSessionSettings = browserClient.updateSessionSettings
    if (typeof (merged as any).listGitBranches !== 'function')
      merged.listGitBranches = browserClient.listGitBranches
    if (typeof (merged as any).listGitWorktreeBranchesInUse !== 'function')
      merged.listGitWorktreeBranchesInUse = browserClient.listGitWorktreeBranchesInUse
    if (typeof (merged as any).getWorkspaceInfo !== 'function')
      merged.getWorkspaceInfo = browserClient.getWorkspaceInfo
    if (typeof (merged as any).switchWorkspace !== 'function')
      merged.switchWorkspace = browserClient.switchWorkspace
    if (typeof (merged as any).setCommitMessageModelId !== 'function')
      merged.setCommitMessageModelId = browserClient.setCommitMessageModelId
    if (typeof (merged as any).detectGitTools !== 'function')
      merged.detectGitTools = browserClient.detectGitTools
    if (typeof (merged as any).generateCommitMeta !== 'function')
      merged.generateCommitMeta = browserClient.generateCommitMeta
    if (typeof (merged as any).commitWorkflow !== 'function')
      merged.commitWorkflow = browserClient.commitWorkflow
    if (typeof (merged as any).onCommitWorkflowProgress !== 'function')
      merged.onCommitWorkflowProgress = browserClient.onCommitWorkflowProgress
    if (typeof (merged as any).createWorkspace !== 'function')
      merged.createWorkspace = browserClient.createWorkspace
    if (typeof (merged as any).updateProjectSettings !== 'function')
      merged.updateProjectSettings = browserClient.updateProjectSettings
    if (typeof (merged as any).removeWorktree !== 'function')
      merged.removeWorktree = browserClient.removeWorktree
    if (typeof (merged as any).pushBranch !== 'function')
      merged.pushBranch = browserClient.pushBranch
    if (typeof (merged as any).detectEditors !== 'function')
      merged.detectEditors = browserClient.detectEditors
    if (typeof (merged as any).openWithEditor !== 'function')
      merged.openWithEditor = browserClient.openWithEditor
    if (typeof (merged as any).setLocalDiagnosticsEnabled !== 'function')
      merged.setLocalDiagnosticsEnabled = browserClient.setLocalDiagnosticsEnabled
    if (typeof (merged as any).setLocalDiagnosticsRetention !== 'function')
      merged.setLocalDiagnosticsRetention = browserClient.setLocalDiagnosticsRetention
    if (typeof (merged as any).setLanAccessEnabled !== 'function')
      merged.setLanAccessEnabled = browserClient.setLanAccessEnabled
    if (typeof (merged as any).appendDiagnosticsEvent !== 'function')
      merged.appendDiagnosticsEvent = browserClient.appendDiagnosticsEvent
    if (typeof (merged as any).getDiagnosticsDir !== 'function')
      merged.getDiagnosticsDir = browserClient.getDiagnosticsDir
    if (typeof (merged as any).exportDiagnostics !== 'function')
      merged.exportDiagnostics = browserClient.exportDiagnostics
    if (typeof (merged as any).openPath !== 'function') merged.openPath = browserClient.openPath
    if (typeof (merged as any).clearSession !== 'function')
      merged.clearSession = browserClient.clearSession
    if (typeof (merged as any).createSession !== 'function')
      merged.createSession = browserClient.createSession
    if (typeof (merged as any).restartSessionWithActiveKey !== 'function')
      merged.restartSessionWithActiveKey = browserClient.restartSessionWithActiveKey
    if (typeof (merged as any).onSessionIdReplaced !== 'function')
      merged.onSessionIdReplaced = browserClient.onSessionIdReplaced
    return merged as DroidClientAPI
  }
  return browserClient
}

export function isBrowserMode(): boolean {
  return !(window as any).droid
}
