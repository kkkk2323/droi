import { readFile, stat } from 'fs/promises'
import { homedir } from 'os'
import { basename, join, resolve } from 'path'
import type {
  MissionRuntimeRequest,
  MissionRuntimeSnapshot,
  RuntimeLogEntry,
} from './missionTypes.ts'
import { resolveMissionDirPath, readMissionDirSnapshot } from './missionDirReader.ts'

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function normalizeOptionalString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

function resolveFactoryHome(): string {
  const override = normalizeOptionalString(process.env['FACTORY_HOME_OVERRIDE'])
  return override ? resolve(override) : homedir()
}

function resolveWorkingDirectory(value: string): string {
  if (value === '~') return homedir()
  if (value.startsWith('~/')) return join(homedir(), value.slice(2))
  return resolve(value)
}

function buildCwdKey(workingDirectory: string): string {
  let resolvedDir = resolveWorkingDirectory(workingDirectory).replace(/[\\/]+$/, '')
  resolvedDir = resolvedDir.replace(/^[/\\]+/, '')
  return `-${resolvedDir.replace(/[\\/]+/g, '-')}`
}

export function resolveWorkerSessionPath(params: {
  workingDirectory: string
  workerSessionId: string
}): string {
  return join(
    resolveFactoryHome(),
    '.factory',
    'sessions',
    buildCwdKey(params.workingDirectory),
    `${params.workerSessionId}.jsonl`,
  )
}

async function pathExists(path: string): Promise<boolean> {
  try {
    return (await stat(path)).isFile()
  } catch {
    return false
  }
}

function clipText(value: string, max = 4_000): string {
  return value.length <= max ? value : `${value.slice(0, max - 1)}…`
}

function normalizeText(value: string): string {
  return value.replace(/\r\n/g, '\n').replace(/\r/g, '\n').trim()
}

function readTimestamp(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim()) {
    const parsed = Date.parse(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

function summarizeUnknown(value: unknown, depth = 0): string {
  if (depth > 4 || value == null) return ''
  if (typeof value === 'string') return normalizeText(value)
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (Array.isArray(value)) {
    return value
      .map((entry) => summarizeUnknown(entry, depth + 1))
      .filter(Boolean)
      .join('\n')
      .trim()
  }
  if (!isObject(value)) return ''

  const objectValue = value as Record<string, unknown>
  const priorityKeys = [
    'command',
    'content',
    'text',
    'message',
    'stdout',
    'stderr',
    'output',
    'result',
    'error',
    'reason',
    'status',
  ]
  const parts = priorityKeys
    .map((key) => summarizeUnknown(objectValue[key], depth + 1))
    .filter(Boolean)
  if (parts.length > 0) return parts.join('\n').trim()

  try {
    return clipText(JSON.stringify(objectValue))
  } catch {
    return ''
  }
}

function blockType(block: unknown): string {
  if (!isObject(block)) return ''
  return String((block as any).type || (block as any).kind || '')
    .trim()
    .toLowerCase()
}

function readMessageBlocks(message: unknown): unknown[] {
  if (!isObject(message)) return []
  if (Array.isArray((message as any).content)) return (message as any).content as unknown[]
  if (Array.isArray((message as any).blocks)) return (message as any).blocks as unknown[]
  return []
}

function parseToolUseBlock(block: Record<string, unknown>, ts: number, workerSessionId: string) {
  const name = normalizeOptionalString((block as any).name) || 'tool'
  const input = isObject((block as any).input)
    ? ((block as any).input as Record<string, unknown>)
    : isObject((block as any).params)
      ? ((block as any).params as Record<string, unknown>)
      : {}
  const command = normalizeOptionalString(input.command)
  const text = command
    ? command
    : `${name}${Object.keys(input).length > 0 ? ` ${clipText(JSON.stringify(input), 600)}` : ''}`
  return {
    ts,
    stream: 'system',
    kind: 'command',
    workerSessionId,
    text,
  } as RuntimeLogEntry
}

function parseToolResultBlock(block: Record<string, unknown>, ts: number, workerSessionId: string) {
  const payload =
    (block as any).content ?? (block as any).result ?? (block as any).value ?? (block as any).text
  const summary = summarizeUnknown(payload)
  if (!summary) return null
  const isError = Boolean((block as any).isError)
  return {
    ts,
    stream: isError ? 'stderr' : 'system',
    kind: 'result',
    workerSessionId,
    text: clipText(summary),
  } as RuntimeLogEntry
}

function parseTextBlock(
  role: string,
  block: Record<string, unknown>,
  ts: number,
  workerSessionId: string,
) {
  const text = summarizeUnknown(
    (block as any).text ?? (block as any).content ?? (block as any).value ?? (block as any).body,
  )
  if (!text) return null
  const prefix = role === 'user' ? 'User' : role === 'assistant' ? 'Worker' : 'Message'
  return {
    ts,
    stream: 'system',
    kind: 'message',
    workerSessionId,
    text: clipText(`${prefix}: ${text}`),
  } as RuntimeLogEntry
}

function parseMessageEvent(
  entry: Record<string, unknown>,
  workerSessionId: string,
  fallbackTs: number,
): RuntimeLogEntry[] {
  const message = isObject(entry.message) ? (entry.message as Record<string, unknown>) : null
  if (!message) return []
  const role = String(message.role || '')
    .trim()
    .toLowerCase()
  const ts =
    readTimestamp(entry.timestamp) ?? readTimestamp((message as any).timestamp) ?? fallbackTs
  const blocks = readMessageBlocks(message)
  const logs: RuntimeLogEntry[] = []
  for (const rawBlock of blocks) {
    if (!isObject(rawBlock)) continue
    const type = blockType(rawBlock)
    if (type === 'tool_use') {
      logs.push(parseToolUseBlock(rawBlock, ts, workerSessionId))
      continue
    }
    if (type === 'tool_result') {
      const entryLog = parseToolResultBlock(rawBlock, ts, workerSessionId)
      if (entryLog) logs.push(entryLog)
      continue
    }
    if (type === 'text') {
      const entryLog = parseTextBlock(role, rawBlock, ts, workerSessionId)
      if (entryLog) logs.push(entryLog)
    }
  }
  return logs
}

function parseRuntimeEntries(raw: string, workerSessionId: string): RuntimeLogEntry[] {
  const entries: RuntimeLogEntry[] = []
  let fallbackTs = 0
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim()
    if (!trimmed) continue
    fallbackTs += 1
    try {
      const parsed = JSON.parse(trimmed)
      if (!isObject(parsed)) continue
      if (String(parsed.type || '').trim() === 'message') {
        entries.push(...parseMessageEvent(parsed, workerSessionId, fallbackTs))
      }
    } catch {
      // Ignore partially-written lines; the watcher will recover on the next refresh.
    }
  }
  return entries.slice(-400)
}

async function resolveEffectiveWorkingDirectory(
  params: Pick<MissionRuntimeRequest, 'missionDir' | 'missionBaseSessionId' | 'workingDirectory'>,
): Promise<string | undefined> {
  const explicit = normalizeOptionalString(params.workingDirectory)
  if (explicit) return resolveWorkingDirectory(explicit)

  const missionDir = normalizeOptionalString(params.missionDir)
  const missionBaseSessionId = normalizeOptionalString(params.missionBaseSessionId)
  if (!missionDir && !missionBaseSessionId) return undefined

  try {
    const snapshot = await readMissionDirSnapshot(
      resolveMissionDirPath({
        sessionId: '',
        missionDir,
        missionBaseSessionId,
      }),
    )
    return normalizeOptionalString(snapshot.workingDirectory)
  } catch {
    return undefined
  }
}

export async function readMissionRuntimeSnapshot(
  params: MissionRuntimeRequest,
): Promise<MissionRuntimeSnapshot> {
  const sessionId = normalizeOptionalString(params.sessionId) || ''
  const workerSessionId = normalizeOptionalString(params.workerSessionId)
  if (!workerSessionId) {
    return {
      sessionId,
      workerSessionId: undefined,
      exists: false,
      status: 'idle',
      source: 'none',
      message: 'No active worker session.',
      entries: [],
    }
  }

  const workingDirectory = await resolveEffectiveWorkingDirectory(params)
  if (!workingDirectory) {
    return {
      sessionId,
      workerSessionId,
      exists: false,
      status: 'unavailable',
      source: 'none',
      message: 'Missing Mission working directory for worker session logs.',
      entries: [],
    }
  }

  const sessionFile = resolveWorkerSessionPath({ workingDirectory, workerSessionId })
  if (!(await pathExists(sessionFile))) {
    return {
      sessionId,
      workerSessionId,
      workingDirectory,
      sessionFile,
      exists: false,
      status: 'waiting',
      source: 'worker_session',
      message: 'Waiting for worker session transcript…',
      entries: [],
    }
  }

  try {
    const raw = await readFile(sessionFile, 'utf8')
    const entries = parseRuntimeEntries(raw, workerSessionId)
    return {
      sessionId,
      workerSessionId,
      workingDirectory,
      sessionFile,
      exists: true,
      status: entries.length > 0 ? 'ready' : 'waiting',
      source: 'worker_session',
      message:
        entries.length > 0
          ? undefined
          : 'Worker session is active but has no command or tool-result entries yet.',
      entries,
    }
  } catch {
    return {
      sessionId,
      workerSessionId,
      workingDirectory,
      sessionFile,
      exists: true,
      status: 'unavailable',
      source: 'worker_session',
      message: `Failed to read ${basename(sessionFile)}.`,
      entries: [],
    }
  }
}
