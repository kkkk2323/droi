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
const sessionId = process.env.DROID_SESSION_ID || `exit-spec-${Date.now()}`
const machineId = process.env.DROID_MACHINE_ID || `machine-${randomUUID()}`
const actionRequested = process.env.EXIT_SPEC_ACTION || 'proceed_auto_run_low'
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
let lastActionSent = ''
let exitSpecActionSent = ''
let sawWorkingNonIdle = false
const pending = new Map()
const timeline = []
const permissionEvents = []
const stderrLines = []
const rawStdoutLines = []

const now = () => Date.now()
const mark = (type, extra = {}) => {
  timeline.push({ t: now(), type, ...extra })
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

const pickPermissionOption = (available, isExitSpec) => {
  if (isExitSpec) {
    if (available.includes(actionRequested)) return actionRequested
    if (available.includes('proceed_once')) return 'proceed_once'
  }
  if (available.includes('cancel')) return 'cancel'
  return available[0] || 'cancel'
}

const extractExitSpecPlanInfo = (toolUses) => {
  if (!Array.isArray(toolUses)) return null
  for (const item of toolUses) {
    const raw = item && typeof item === 'object' ? (item.toolUse || item) : null
    if (!raw || typeof raw !== 'object') continue
    const name = typeof raw.name === 'string' ? raw.name : ''
    if (!/exit\s?spec/i.test(name)) continue
    const input = raw.input
    if (!input || typeof input !== 'object' || Array.isArray(input)) continue
    const plan = typeof input.plan === 'string' ? input.plan : ''
    const title = typeof input.title === 'string' ? input.title : ''
    const optionNames = Array.isArray(input.optionNames)
      ? input.optionNames.map((v) => String(v))
      : []
    return {
      toolName: name,
      planLength: plan.length,
      planPreview: plan.slice(0, 200),
      title,
      optionNames,
    }
  }
  return null
}

let doneResolve = null
const done = new Promise((resolve) => {
  doneResolve = resolve
})

const finish = (reason) => {
  if (resolved) return
  resolved = true
  if (doneResolve) doneResolve(reason)
}

const handleMessage = (msg) => {
  if (!msg || typeof msg !== 'object') return
  mark('json', { type: msg.type || '' })

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
      const params = msg.params && typeof msg.params === 'object' ? msg.params : {}
      const options = parsePermissionOptions(params.options)
      const exitSpec = extractExitSpecPlanInfo(params.toolUses)
      const selectedOption = pickPermissionOption(options, Boolean(exitSpec))
      lastActionSent = selectedOption
      if (exitSpec && !exitSpecActionSent) {
        exitSpecActionSent = selectedOption
      }

      permissionEvents.push({
        requestId: String(msg.id || ''),
        options,
        selectedOption,
        exitSpec,
      })

      sendResponse(String(msg.id), { selectedOption })
      mark('permission-response', { requestId: msg.id || '', selectedOption, exitSpec: Boolean(exitSpec) })
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
    mark('notification', { nType })

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
  const text = chunk.toString()
  stderrLines.push(text)
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
    cleanup()

    const summary = {
      sessionId,
      cwd,
      modelId,
      actionRequested,
      actionSent: exitSpecActionSent || lastActionSent || null,
      exitSpecActionSent: exitSpecActionSent || null,
      lastActionSent: lastActionSent || null,
      doneReason: reason,
      permissionEventCount: permissionEvents.length,
      sawExitSpecPermission: permissionEvents.some((e) => Boolean(e.exitSpec)),
      permissionEvents,
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
