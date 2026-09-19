#!/usr/bin/env node
/**
 * Mid-turn injection probe for `droid exec --input-format stream-jsonrpc`.
 *
 * Goal:
 * - Send a long-running turn (forces a tool wait via `sleep`).
 * - While that turn is still running, send another user message (inject).
 * - Determine whether the injected token affects the *current* turn, and if not,
 *   whether it is handled in a *subsequent* turn (queued-for-later).
 *
 * Run:
 *   FACTORY_API_KEY='fk-...' node scripts/droid_mid_turn_injection_test.mjs
 *
 * Notes:
 * - We create an isolated HOME for the child process so droid can write sessions.
 * - The API key is NEVER printed.
 * - If your droid version doesn't accept `stream-jsonrpc`, set `DROID_STREAM_FORMAT=stream-json`.
 */

import { spawn } from 'node:child_process'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { randomUUID } from 'node:crypto'

const JSONRPC_VERSION = '2.0'
const FACTORY_API_VERSION = '1.0.0'

const apiKey = process.env.FACTORY_API_KEY
if (!apiKey) {
  console.error('Missing FACTORY_API_KEY in env')
  process.exit(2)
}

const cwd = process.env.DROID_CWD || process.cwd()
const modelId = process.env.DROID_MODEL_ID || undefined
const autonomyLevel = process.env.DROID_AUTONOMY_LEVEL || 'auto-high'
const reasoningEffort = process.env.DROID_REASONING_EFFORT || undefined
const streamFormat = process.env.DROID_STREAM_FORMAT || 'stream-jsonrpc'

const sleepSeconds = Number(process.env.SLEEP_SECONDS || 12)
const injectDelayMs = Number(process.env.INJECT_DELAY_MS || 2000)
const maxRunMs = Number(process.env.MAX_RUN_MS || 120000)
const postIdleObserveMs = Number(process.env.POST_IDLE_OBSERVE_MS || 20000)
const quietWindowMs = Number(process.env.QUIET_WINDOW_MS || 2000)

const sessionId = process.env.DROID_SESSION_ID || `mid-inject-${Date.now()}`
const machineId = process.env.DROID_MACHINE_ID || `machine-${randomUUID()}`
const token = process.env.INJECT_TOKEN || `TOK_${randomUUID().slice(0, 8).toUpperCase()}`

const homeDir = mkdtempSync(join(tmpdir(), 'droid-mid-inject-home-'))

const execArgs = [
  'exec',
  '--input-format', streamFormat,
  '--output-format', streamFormat,
  '--cwd', cwd,
  '--auto', 'high',
  "--model", "kimi-k2.5"
]

// Optional CLI model flag (separate from JSON-RPC settings).
if (process.env.DROID_EXEC_MODEL) {
  execArgs.push('--model', String(process.env.DROID_EXEC_MODEL))
}

const proc = spawn('droid', execArgs, {
  cwd,
  env: {
    ...process.env,
    HOME: homeDir,
    FACTORY_API_KEY: apiKey,
  },
  stdio: ['pipe', 'pipe', 'pipe'],
})

let seq = 0
const pending = new Map()

const timeline = []
const workingStates = []
const errors = []
const rawStdout = []
const assistantTextById = new Map()
const assistantOrder = []
const assistantFirstSeenAtById = new Map()
const createMessageSamples = []
const assistantHasDelta = new Set()

let lastEventAt = Date.now()
let injectedAt = 0
let firstUserMessageSentAt = 0

let turn1NonIdleAt = 0
let turn1IdleAt = 0
let turn2NonIdleAt = 0
let turn2IdleAt = 0

const toolProgressTimes = []
const toolResultTimes = []

const now = () => Date.now()
const mark = (type, extra = {}) => {
  const item = { t: now(), type, ...extra }
  timeline.push(item)
  lastEventAt = item.t
}

const send = (obj) => {
  if (!proc.stdin.writable) return
  proc.stdin.write(`${JSON.stringify(obj)}\n`)
}

const sendResponse = (id, result) => {
  send({ jsonrpc: JSONRPC_VERSION, factoryApiVersion: FACTORY_API_VERSION, type: 'response', id, result })
}

const request = (method, params) => {
  const id = `${sessionId}:${++seq}`
  send({ jsonrpc: JSONRPC_VERSION, factoryApiVersion: FACTORY_API_VERSION, type: 'request', id, method, params })
  mark('request', { id, method })
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id)
      reject(new Error(`timeout waiting response: ${method}`))
    }, 45000)
    pending.set(id, { resolve, reject, timer, method })
  })
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function extractText(content) {
  if (!content) return ''
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    let out = ''
    for (const part of content) {
      if (!part || typeof part !== 'object') continue
      const text = typeof part.text === 'string'
        ? part.text
        : typeof part.content === 'string'
          ? part.content
          : ''
      out += text
    }
    return out
  }
  if (typeof content === 'object') {
    if (typeof content.text === 'string') return content.text
    if (typeof content.content === 'string') return content.content
  }
  return ''
}

function addAssistantText(messageId, delta) {
  if (!assistantTextById.has(messageId)) {
    assistantTextById.set(messageId, '')
    assistantOrder.push(messageId)
    assistantFirstSeenAtById.set(messageId, now())
  }
  assistantTextById.set(messageId, assistantTextById.get(messageId) + delta)
}

function handleJson(msg) {
  mark('json', { msgType: msg?.type, method: msg?.method || '' })

  if (msg?.type === 'response' && typeof msg.id === 'string') {
    const p = pending.get(msg.id)
    if (p) {
      clearTimeout(p.timer)
      pending.delete(msg.id)
      p.resolve(msg)
    }
    return
  }

  if (msg?.type === 'request') {
    const method = msg.method
    mark('inbound-request', { method })

    if (method === 'droid.request_permission') {
      // Auto-approve to keep the run moving.
      sendResponse(msg.id, { selectedOption: 'proceed_auto_run_high' })
      mark('auto-response', { for: method, id: msg.id })
      return
    }
    if (method === 'droid.ask_user') {
      sendResponse(msg.id, { cancelled: true, answers: [] })
      mark('auto-response', { for: method, id: msg.id })
      return
    }
    return
  }

  if (msg?.type === 'notification' && msg.method === 'droid.session_notification') {
    const n = msg?.params?.notification || {}
    const nType = typeof n.type === 'string' ? n.type : ''
    mark('notification', { nType })

    if (nType === 'assistant_text_delta') {
      const messageId = String(n.messageId || '')
      const textDelta = String(n.textDelta || '')
      if (messageId && textDelta) {
        assistantHasDelta.add(messageId)
        addAssistantText(messageId, textDelta)
      }
    }

    if (nType === 'create_message') {
      const m = n.message
      const role = m?.role
      const id = m?.id
      if (createMessageSamples.length < 3) {
        const preview = extractText(m?.content).slice(0, 240)
        createMessageSamples.push({
          role: typeof role === 'string' ? role : null,
          id: typeof id === 'string' ? id : null,
          contentShape: Array.isArray(m?.content) ? 'array' : typeof m?.content,
          contentPreview: preview || null,
        })
      }

      if (role === 'assistant' && typeof id === 'string') {
        const text = extractText(m?.content)
        // Avoid duplicating output when create_message contains a full snapshot and we also receive deltas.
        if (text && !assistantHasDelta.has(id)) {
          if (!assistantTextById.has(id)) assistantOrder.push(id)
          if (!assistantFirstSeenAtById.has(id)) assistantFirstSeenAtById.set(id, now())
          assistantTextById.set(id, text)
        }
      }
    }

    if (nType === 'droid_working_state_changed' || nType === 'working_state_changed') {
      const s = String(n.newState || '')
      const t = now()
      workingStates.push({ t, state: s })

      const normalized = s.trim().toLowerCase()
      if (firstUserMessageSentAt && !turn1NonIdleAt && normalized !== 'idle' && t >= firstUserMessageSentAt) {
        turn1NonIdleAt = t
        mark('turn1-start', { state: s })
      }
      if (turn1NonIdleAt && !turn1IdleAt && normalized === 'idle' && t >= turn1NonIdleAt) {
        turn1IdleAt = t
        mark('turn1-idle')
      } else if (turn1IdleAt && !turn2NonIdleAt && normalized !== 'idle' && t >= turn1IdleAt) {
        turn2NonIdleAt = t
        mark('turn2-start', { state: s })
      } else if (turn2NonIdleAt && !turn2IdleAt && normalized === 'idle' && t >= turn2NonIdleAt) {
        turn2IdleAt = t
        mark('turn2-idle')
      }
    }

    if (nType === 'tool_progress_update') toolProgressTimes.push(now())
    if (nType === 'tool_result') toolResultTimes.push(now())

    if (nType === 'error') {
      errors.push(String(n.message || 'Unknown error'))
    }
  }
}

let stdoutBuf = ''
proc.stdout.on('data', (chunk) => {
  stdoutBuf += chunk.toString()
  let idx
  while ((idx = stdoutBuf.indexOf('\n')) >= 0) {
    const line = stdoutBuf.slice(0, idx).trim()
    stdoutBuf = stdoutBuf.slice(idx + 1)
    if (!line) continue
    try {
      handleJson(JSON.parse(line))
    } catch {
      rawStdout.push(line)
      mark('stdout-text', { line: line.slice(0, 200) })
    }
  }
})

proc.stderr.on('data', (chunk) => {
  const text = chunk.toString().trim()
  if (!text) return
  errors.push(text)
  mark('stderr', { text: text.slice(0, 300) })
})

proc.on('close', (code, signal) => {
  mark('close', { code: code ?? null, signal: signal ?? null })
  for (const [, p] of pending) {
    clearTimeout(p.timer)
    p.reject(new Error('process closed'))
  }
  pending.clear()
})

async function main() {
  const t0 = now()
  mark('start', { sessionId, cwd, sleepSeconds, injectDelayMs, homeDir })

  await request('droid.initialize_session', {
    machineId,
    cwd,
    sessionId,
    ...(modelId ? { modelId } : {}),
    autonomyLevel,
    reasoningEffort: reasoningEffort || undefined,
  })

  await request('droid.update_session_settings', {
    ...(modelId ? { modelId } : {}),
    autonomyLevel,
    reasoningEffort: reasoningEffort || undefined,
  })

  const firstPrompt = [
    `Run shell command: sleep ${sleepSeconds}.`,
    'In your response to THIS message only, after the sleep completes, output EXACTLY one line:',
    'INJECTION_RESULT=<value>',
    'Rule: if you received a later user message containing "INJECT_TOKEN=<value>" BEFORE you print the line, use that <value>.',
    'Otherwise output INJECTION_RESULT=NONE.',
    'Do not output anything else in this response.',
  ].join(' ')

  firstUserMessageSentAt = now()
  await request('droid.add_user_message', { text: firstPrompt })

  // Inject while the first turn is presumably still active.
  await sleep(injectDelayMs)
  await request('droid.add_user_message', {
    text: [
      `INJECT_TOKEN=${token}.`,
      'If you process THIS message after the previous task already completed (i.e. after the assistant has already output the INJECTION_RESULT line),',
      `then ignoring any prior output-format restrictions, reply with exactly ACK_LATE_${token} and nothing else.`,
    ].join(' ')
  })
  injectedAt = now()
  mark('inject-sent', { token })

  const deadline = t0 + maxRunMs
  let postIdleDeadline = 0
  while (now() < deadline) {
    await sleep(250)
    if (!turn1IdleAt) continue
    if (!postIdleDeadline) postIdleDeadline = turn1IdleAt + postIdleObserveMs

    const quiet = now() - lastEventAt > quietWindowMs
    if (turn2IdleAt && quiet) break
    if (now() > postIdleDeadline && quiet) break
  }

  try {
    await request('droid.interrupt_session', {})
  } catch {
    // ignore
  }
  try {
    proc.kill('SIGTERM')
  } catch {
    // ignore
  }

  const assistantMessages = assistantOrder.map((id) => ({
    id,
    firstSeenAt: assistantFirstSeenAtById.get(id) ?? null,
    text: String(assistantTextById.get(id) || '').trim(),
  }))
  const resultRegex = /INJECTION_RESULT=([A-Za-z0-9_-]+)/i
  const ackLateRegex = new RegExp(`\\\\bACK_LATE_${token}\\\\b`)
  const parsedResults = assistantMessages.map((m, index) => {
    const match = m.text.match(resultRegex)
    return {
      index,
      messageId: m.id,
      firstSeenAt: m.firstSeenAt,
      resultValue: match ? match[1] : null,
      hasAckLate: ackLateRegex.test(m.text),
      textPreview: m.text.slice(0, 240),
    }
  })

  const injectionResultCandidates = parsedResults
    .filter((r) => r.resultValue !== null)
    .sort((a, b) => (Number(a.firstSeenAt ?? 0) - Number(b.firstSeenAt ?? 0)))
  const firstResult = injectionResultCandidates[0] || null
  const firstValue = firstResult?.resultValue || null
  const sawTokenInFirstResult = typeof firstValue === 'string' && (firstValue === token || firstValue.startsWith(token) || firstValue.includes(token))
  const ackLateAfterTurn1 = parsedResults.some((r) => r.hasAckLate && typeof r.firstSeenAt === 'number' && r.firstSeenAt >= turn1IdleAt)
  const sawAckLate = parsedResults.some((r) => r.hasAckLate)

  // Heuristic verdict:
  // - token in first result => injection visible before completing first turn (best case).
  // - NONE + ACK_LATE_<token> after turn1 idle => injection processed later (next turn / queued).
  // - otherwise => inconclusive (manual inspection required).
  let verdict = 'inconclusive'
  if (sawTokenInFirstResult) verdict = 'mid_turn_injection_supported'
  else if (String(firstValue || '').toUpperCase() === 'NONE' && ackLateAfterTurn1) verdict = 'injection_queued_for_later'
  else if (String(firstValue || '').toUpperCase() === 'NONE') verdict = 'no_mid_turn_injection_observed'

  const toolStartAt = workingStates.find((w) => String(w.state || '').trim().toLowerCase() === 'executing_tool')?.t
    ?? toolProgressTimes[0]
    ?? null
  const toolEndAt = toolResultTimes[0] ?? null

  const summary = {
    sessionId,
    token,
    elapsedMs: now() - t0,
    verdict,
    timing: {
      firstUserMessageSentAt,
      injectedAt,
      toolStartAt,
      toolEndAt,
      turn1NonIdleAt: turn1NonIdleAt || null,
      turn1IdleAt: turn1IdleAt || null,
      turn2NonIdleAt: turn2NonIdleAt || null,
      turn2IdleAt: turn2IdleAt || null,
      injectRelativeToToolStartMs: toolStartAt ? injectedAt - toolStartAt : null,
      injectRelativeToTurn1StartMs: turn1NonIdleAt ? injectedAt - turn1NonIdleAt : null,
      postIdleObserveMs,
      quietWindowMs,
    },
    createMessageSamples,
    workingStates,
    parsedResults,
    sawAckLate,
    ackLateAfterTurn1,
    errorCount: errors.length,
    errors: errors.slice(0, 8),
    rawStdoutSample: rawStdout.slice(0, 20),
    timelineSample: timeline.slice(0, 50),
  }

  console.log(JSON.stringify(summary, null, 2))
}

main().catch((err) => {
  console.error(String(err?.stack || err))
  try { proc.kill('SIGTERM') } catch { }
  process.exitCode = 1
})
