import { randomUUID } from 'node:crypto'
import { spawn } from 'node:child_process'

const JSONRPC_VERSION = '2.0'
const FACTORY_API_VERSION = '1.0.0'

const apiKey = process.env.FACTORY_API_KEY
if (!apiKey) {
  console.error('Missing FACTORY_API_KEY in env')
  process.exit(2)
}

const cwd = process.env.DROID_CWD || process.cwd()
const modelId = process.env.DROID_MODEL_ID || 'kimi-k2.5'
const sessionId = process.env.DROID_SESSION_ID || `exit-spec-timing-${Date.now()}`
const machineId = process.env.DROID_MACHINE_ID || `machine-${randomUUID()}`
const actionRequested = process.env.EXIT_SPEC_ACTION || 'proceed_auto_run_medium'
const nonExitAction = process.env.NON_EXIT_ACTION || 'cancel'
const updateStrategy = process.env.UPDATE_STRATEGY || 'none'
const targetAutoLevel = process.env.TARGET_AUTO_LEVEL || 'medium'
const maxRunMs = Number(process.env.MAX_RUN_MS || 180000)
const prompt = process.env.EXIT_SPEC_PROMPT || [
  'Please enter specification mode for this request.',
  'Design a plan to add a compact session search box to the sidebar with keyboard focus support.',
  'After presenting the spec plan, ask for approval before any code changes.',
].join(' ')

const args = [
  'exec',
  '--input-format', 'stream-jsonrpc',
  '--output-format', 'stream-jsonrpc',
  '--cwd', cwd,
  '--auto', 'high',
  '--model', modelId,
]

const proc = spawn('droid', args, {
  cwd,
  env: {
    ...process.env,
    FACTORY_API_KEY: apiKey,
  },
  stdio: ['pipe', 'pipe', 'pipe'],
})

let seq = 0
let stdoutBuffer = ''
let resolved = false
let sawWorkingNonIdle = false
let doneResolve = null
let exitSpecRespondedAt = -1
let eventSeq = 0
const pending = new Map()
const pendingOps = new Set()
const timeline = []
const permissionEvents = []
const updateAttempts = []
const stderrLines = []
const rawStdoutLines = []

const now = () => Date.now()
const mark = (type, extra = {}) => {
  timeline.push({ n: ++eventSeq, t: now(), type, ...extra })
}

const write = (obj) => {
  if (!proc.stdin.writable) return
  proc.stdin.write(`${JSON.stringify(obj)}\n`)
}

const sendResponse = (id, result) => {
  write({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'response',
    id,
    result,
  })
}

const sendRequest = (method, params) => {
  const id = `${sessionId}:${++seq}`
  write({
    jsonrpc: JSONRPC_VERSION,
    factoryApiVersion: FACTORY_API_VERSION,
    type: 'request',
    id,
    method,
    params,
  })
  mark('request', { id, method })

  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id)
      reject(new Error(`timeout waiting response: ${method}`))
    }, 45000)
    pending.set(id, { resolve, reject, timer })
  })
}

const parsePermissionOptions = (options) => {
  if (!Array.isArray(options)) return []
  return options
    .map((item) => {
      if (typeof item === 'string') return item
      if (!item || typeof item !== 'object') return ''
      const value = typeof item.value === 'string' ? item.value : ''
      const id = typeof item.id === 'string' ? item.id : ''
      return value || id || ''
    })
    .filter(Boolean)
}

const normalizeToolName = (value) => {
  const raw = typeof value === 'string' ? value.trim() : ''
  if (!raw) return ''
  const parts = raw.split('.')
  return parts[parts.length - 1] || raw
}

const extractToolUseItems = (toolUses) => {
  if (!Array.isArray(toolUses)) return []
  return toolUses
    .map((item) => {
      const raw = item && typeof item === 'object' ? (item.toolUse || item) : null
      if (!raw || typeof raw !== 'object') return null
      const name = normalizeToolName(raw.name || raw.toolName || raw.recipient_name)
      const input = raw.input && typeof raw.input === 'object' && !Array.isArray(raw.input)
        ? raw.input
        : (raw.parameters && typeof raw.parameters === 'object' && !Array.isArray(raw.parameters) ? raw.parameters : {})
      return { name, input }
    })
    .filter(Boolean)
}

const isExitSpecRequest = (toolUses) => {
  return extractToolUseItems(toolUses).some((item) => /exit\s?spec/i.test(item.name))
}

const pickPermissionOption = (available, isExitSpec) => {
  if (isExitSpec) {
    if (available.includes(actionRequested)) return actionRequested
    if (available.includes('proceed_auto_run')) return 'proceed_auto_run'
    if (available.includes('proceed_once')) return 'proceed_once'
  }
  if (available.includes(nonExitAction)) return nonExitAction
  if (available.includes('cancel')) return 'cancel'
  return available[0] || 'cancel'
}

const runUpdateSettings = async (phase, permissionRequestId) => {
  if (!['before', 'after'].includes(updateStrategy)) return null
  if (!['low', 'medium', 'high'].includes(targetAutoLevel)) return null
  const startedAt = now()
  mark('update-settings-request', { phase, permissionRequestId, targetAutoLevel })
  try {
    const res = await sendRequest('droid.update_session_settings', { autonomyLevel: targetAutoLevel })
    const isError = Boolean(res && res.error)
    const attempt = {
      phase,
      permissionRequestId,
      targetAutoLevel,
      ok: !isError,
      error: isError ? String(res.error?.message || 'unknown') : null,
      durationMs: now() - startedAt,
    }
    updateAttempts.push(attempt)
    mark('update-settings-response', { phase, permissionRequestId, ok: attempt.ok, error: attempt.error })
    return attempt
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    const attempt = {
      phase,
      permissionRequestId,
      targetAutoLevel,
      ok: false,
      error: message,
      durationMs: now() - startedAt,
    }
    updateAttempts.push(attempt)
    mark('update-settings-response', { phase, permissionRequestId, ok: false, error: message })
    return attempt
  }
}

const createPermissionHandlerTask = async (msg) => {
  const params = msg.params && typeof msg.params === 'object' ? msg.params : {}
  const options = parsePermissionOptions(params.options)
  const toolItems = extractToolUseItems(params.toolUses)
  const toolNames = toolItems.map((item) => item.name).filter(Boolean)
  const isExitSpec = isExitSpecRequest(params.toolUses)
  const selectedOption = pickPermissionOption(options, isExitSpec)
  const requestId = String(msg.id || '')

  const permissionEvent = {
    idx: permissionEvents.length,
    requestId,
    isExitSpec,
    toolNames,
    options,
    selectedOption,
    updateStrategy,
  }
  permissionEvents.push(permissionEvent)

  if (isExitSpec && updateStrategy === 'before') {
    await runUpdateSettings('before', requestId)
  }

  sendResponse(requestId, { selectedOption })
  mark('permission-response', { requestId, selectedOption, isExitSpec })

  if (isExitSpec) {
    exitSpecRespondedAt = permissionEvent.idx
  }

  if (isExitSpec && updateStrategy === 'after') {
    await runUpdateSettings('after', requestId)
  }
}

const done = new Promise((resolve) => {
  doneResolve = resolve
})

const finish = (reason) => {
  if (resolved) return
  resolved = true
  if (doneResolve) doneResolve(reason)
}

const trackTask = (promise) => {
  pendingOps.add(promise)
  promise.finally(() => pendingOps.delete(promise))
}

const handleMessage = (msg) => {
  if (!msg || typeof msg !== 'object') return

  if (msg.type === 'response' && typeof msg.id === 'string') {
    const waiter = pending.get(msg.id)
    if (waiter) {
      clearTimeout(waiter.timer)
      pending.delete(msg.id)
      waiter.resolve(msg)
    }
    return
  }

  if (msg.type === 'request') {
    const method = String(msg.method || '')
    mark('inbound-request', { method, id: msg.id || '' })

    if (method === 'droid.request_permission') {
      const task = createPermissionHandlerTask(msg).catch((err) => {
        const message = err instanceof Error ? err.message : String(err)
        mark('permission-handler-error', { requestId: msg.id || '', error: message })
        try {
          sendResponse(String(msg.id || ''), { selectedOption: 'cancel' })
        } catch {
          // ignore
        }
      })
      trackTask(task)
      return
    }

    if (method === 'droid.ask_user') {
      sendResponse(String(msg.id), { cancelled: true, answers: [] })
      mark('ask-user-response', { requestId: msg.id || '', cancelled: true })
      return
    }

    return
  }

  if (msg.type === 'notification' && msg.method === 'droid.session_notification') {
    const notification = msg.params && typeof msg.params === 'object'
      ? msg.params.notification
      : null
    const nType = notification && typeof notification === 'object'
      ? String(notification.type || '')
      : ''

    if (nType === 'droid_working_state_changed' || nType === 'working_state_changed') {
      const state = String(notification?.newState || '').trim().toLowerCase()
      mark('working-state', { state })
      if (state && state !== 'idle') sawWorkingNonIdle = true
      if (state === 'idle' && sawWorkingNonIdle) finish('turn-idle')
    }
  }
}

proc.stdout.on('data', (chunk) => {
  stdoutBuffer += chunk.toString()
  while (true) {
    const idx = stdoutBuffer.indexOf('\n')
    if (idx === -1) break
    const line = stdoutBuffer.slice(0, idx).trim()
    stdoutBuffer = stdoutBuffer.slice(idx + 1)
    if (!line) continue
    try {
      handleMessage(JSON.parse(line))
    } catch {
      rawStdoutLines.push(line)
    }
  }
})

proc.stderr.on('data', (chunk) => {
  stderrLines.push(chunk.toString())
})

proc.on('close', (code, signal) => {
  mark('close', { code: typeof code === 'number' ? code : null, signal: signal || null })
  finish('process-close')
})

proc.on('error', (err) => {
  mark('proc-error', { message: err.message || String(err) })
  finish('process-error')
})

const timeout = setTimeout(() => {
  mark('timeout', { maxRunMs })
  finish('timeout')
}, maxRunMs)

const cleanup = () => {
  clearTimeout(timeout)
  for (const waiter of pending.values()) {
    clearTimeout(waiter.timer)
    waiter.reject(new Error('probe finished'))
  }
  pending.clear()
  if (!proc.killed && proc.exitCode === null) {
    try {
      proc.kill('SIGTERM')
    } catch {
      // ignore
    }
  }
}

const run = async () => {
  try {
    await sendRequest('droid.initialize_session', {
      machineId,
      cwd,
      sessionId,
      modelId,
      autonomyLevel: 'spec',
    })

    await sendRequest('droid.add_user_message', {
      text: prompt,
    })

    const reason = await done
    await Promise.allSettled(Array.from(pendingOps))
    cleanup()

    const exitSpecEventIndex = permissionEvents.findIndex((event) => event.isExitSpec)
    const postExitPermissions = exitSpecEventIndex >= 0
      ? permissionEvents.slice(exitSpecEventIndex + 1)
      : []
    const postExitNeedsManual = postExitPermissions.some((event) => {
      const opts = event.options
      return opts.includes('proceed_once') || opts.includes('proceed_always')
    })

    const summary = {
      sessionId,
      cwd,
      modelId,
      updateStrategy,
      targetAutoLevel,
      actionRequested,
      nonExitAction,
      doneReason: reason,
      permissionEventCount: permissionEvents.length,
      exitSpecRespondedAt,
      permissionEvents,
      updateAttempts,
      postExitPermissionCount: postExitPermissions.length,
      postExitNeedsManualPermission: postExitNeedsManual,
      timeline,
      stderrPreview: stderrLines.join('').slice(0, 3000),
      rawStdoutPreview: rawStdoutLines.slice(0, 20),
    }

    console.log(JSON.stringify(summary, null, 2))
    process.exit(0)
  } catch (err) {
    cleanup()
    const message = err instanceof Error ? err.message : String(err)
    console.error(`Probe failed: ${message}`)
    process.exit(1)
  }
}

void run()
