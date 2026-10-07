// Entry the Go tests bundle with esbuild: starts droi's Fake Daemon with a
// scenario from the DROID_FAKE_SCENARIO environment variable, prints its
// WebSocket URL as one JSON line, then obeys JSON commands on stdin and
// answers each with one JSON line on stdout.
import { createInterface } from 'node:readline'
import { FakeDaemon } from 'DROI/tests/fake-daemon/fake-daemon'
import { session, userMessage, assistantMessage } from 'DROI/tests/fake-daemon/scenario'
import {
  streamedReply,
  permissionTurn,
  askUserTurn,
  todoTurn,
  structuredTurn,
  interruptHandler,
} from 'DROI/tests/fake-daemon/turns'

type MessageSpec = {
  role: 'user' | 'assistant' | 'tool'
  text?: string
  content?: Record<string, unknown>[]
}
type SessionSpec = {
  title: string
  cwd: string
  messages?: MessageSpec[]
  extra?: Record<string, unknown>
}
type TurnSpec =
  | { kind: 'reply'; deltas: string[]; delayMs?: number }
  | { kind: 'permission'; command: string; reply: string; toolOutput?: string }
  | { kind: 'askUser'; question: string; options: string[]; multiSelect?: boolean }
  | {
      kind: 'todo'
      todos: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' }>
      reply: string
    }
  | { kind: 'structured'; structured: Record<string, unknown> }
type Spec = {
  sessions?: SessionSpec[]
  turn?: TurnSpec
  validDirectories?: string[]
  input?: Record<string, unknown>
}

const spec: Spec = JSON.parse(process.env['DROID_FAKE_SCENARIO'] ?? '{}')

function turnHandler(turn: TurnSpec | undefined) {
  switch (turn?.kind) {
    case undefined:
      return undefined
    case 'reply':
      return streamedReply({ deltas: turn.deltas, delayMs: turn.delayMs })
    case 'permission':
      return permissionTurn(turn)
    case 'askUser':
      return askUserTurn(turn)
    case 'todo':
      return todoTurn(turn)
    case 'structured':
      return structuredTurn(() => turn.structured)
  }
}

const handler = turnHandler(spec.turn)
const daemon = await FakeDaemon.start({
  ...(spec.input ?? {}),
  sessions: (spec.sessions ?? []).map((s) =>
    session(
      s.title,
      s.cwd,
      (s.messages ?? []).map((m) => {
        const base = m.role === 'user' ? userMessage(m.text ?? '') : assistantMessage(m.text ?? '')
        return m.content ? { ...base, role: m.role, content: m.content } : base
      }),
      s.extra ?? {},
    ),
  ),
  handlers: {
    ...(handler ? { 'daemon.add_user_message': handler } : {}),
    'daemon.interrupt_session': interruptHandler,
  },
  validDirectories: spec.validDirectories,
})

const wsUrl =
  daemon.url.replace(/^http/, 'ws') + '/daemon?token=' + encodeURIComponent(daemon.token)
const out = (v: unknown) => process.stdout.write(JSON.stringify(v) + '\n')
out({
  url: wsUrl,
  sessions: daemon.scenario.sessions.map((s) => ({
    sessionId: s.sessionId,
    title: s.title,
    cwd: s.cwd,
  })),
})

const rl = createInterface({ input: process.stdin })
rl.on('line', async (line) => {
  let cmd: { id: number; op: string; [k: string]: unknown }
  try {
    cmd = JSON.parse(line)
  } catch {
    return
  }
  try {
    switch (cmd.op) {
      case 'goDown':
        daemon.goDown()
        return out({ id: cmd.id })
      case 'comeBack':
        daemon.comeBack()
        return out({ id: cmd.id })
      case 'notify':
        daemon.notify(String(cmd['sessionId']), cmd['notification'] as Record<string, unknown>)
        return out({ id: cmd.id })
      case 'requests':
        return out({ id: cmd.id, requests: daemon.requests })
      case 'waitForRequest': {
        const r = await daemon.waitForRequest(
          String(cmd['method']),
          Number(cmd['nth'] ?? 1),
          Number(cmd['timeoutMs'] ?? 5000),
        )
        return out({ id: cmd.id, request: r })
      }
      case 'stop':
        await daemon.stop()
        out({ id: cmd.id })
        process.exit(0)
    }
    out({ id: cmd.id, error: 'unknown op ' + cmd.op })
  } catch (error) {
    out({ id: cmd.id, error: String(error) })
  }
})
rl.on('close', () => {
  void daemon.stop().then(() => process.exit(0))
})
