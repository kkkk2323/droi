import { join, resolve, sep } from 'path'
import { open, readdir, readFile, unlink } from 'fs/promises'
import type {
  ChatMessage,
  LoadSessionResponse,
  RuntimeLogEntry,
  SaveSessionRequest,
  SessionMeta,
} from '../../shared/protocol'
import { atomicWriteFile, ensureDir } from './fsUtils.ts'

const SESSION_ID_RE = /^[A-Za-z0-9_-]{1,128}$/

function getTitleFromMessages(messages: ChatMessage[], fallback?: string): string {
  const firstUser = messages.find((m) => m.role === 'user')
  const cmd = firstUser?.blocks?.find((b: any) => b?.kind === 'command')
  const cmdName = cmd && typeof (cmd as any).name === 'string' ? String((cmd as any).name) : ''
  const skill = firstUser?.blocks?.find((b: any) => b?.kind === 'skill')
  const skillName =
    skill && typeof (skill as any).name === 'string' ? String((skill as any).name) : ''
  const firstTextBlock = firstUser?.blocks?.find((b: any) => b?.kind === 'text')
  const text =
    firstTextBlock && typeof (firstTextBlock as any).content === 'string'
      ? String((firstTextBlock as any).content)
      : ''

  const branchFallback = String(fallback || '').trim()
  const branchTail = branchFallback ? branchFallback.split('/').pop() || branchFallback : ''

  const titleSource = cmdName
    ? `/${cmdName}${text.trim() ? ` ${text.trim()}` : ''}`
    : skillName
      ? `/${skillName}${text.trim() ? ` ${text.trim()}` : ''}`
      : text || branchTail || 'Untitled'

  const trimmed = String(titleSource || 'Untitled')
  return trimmed.slice(0, 40) + (trimmed.length > 40 ? '...' : '')
}

function safeSessionFilePath(sessionsDir: string, id: string): string | null {
  if (!SESSION_ID_RE.test(id)) return null
  const resolvedDir = resolve(sessionsDir)
  const filePath = resolve(sessionsDir, `${id}.json`)
  if (!filePath.startsWith(`${resolvedDir}${sep}`)) return null
  return filePath
}

function normalizeOptionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function normalizeRuntimeLogs(value: unknown): RuntimeLogEntry[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value
    .map((entry) => {
      if (!entry || typeof entry !== 'object') return null
      const ts = Number((entry as any).ts)
      const stream = String((entry as any).stream || '').trim()
      const text = typeof (entry as any).text === 'string' ? (entry as any).text : ''
      if (!Number.isFinite(ts) || !text.trim()) return null
      if (stream !== 'stdout' && stream !== 'stderr' && stream !== 'system') return null
      const kind =
        (entry as any).kind === 'command' ||
        (entry as any).kind === 'result' ||
        (entry as any).kind === 'message' ||
        (entry as any).kind === 'status'
          ? (entry as any).kind
          : undefined
      const workerSessionId =
        typeof (entry as any).workerSessionId === 'string'
          ? String((entry as any).workerSessionId).trim() || undefined
          : undefined
      return { ts, stream, text, kind, workerSessionId } as RuntimeLogEntry
    })
    .filter(Boolean) as RuntimeLogEntry[]
}

const SESSION_META_HEADER_BYTES = 32 * 1024

function decodeJsonStringLiteral(value: string): string | undefined {
  try {
    return JSON.parse(`"${value}"`) as string
  } catch {
    return undefined
  }
}

function extractStringField(raw: string, key: string): string | undefined {
  const match = raw.match(new RegExp(`"${key}"\\s*:\\s*"((?:\\\\.|[^"\\\\])*)"`))
  if (!match) return undefined
  return decodeJsonStringLiteral(match[1])
}

function extractNumberField(raw: string, key: string): number | undefined {
  const match = raw.match(new RegExp(`"${key}"\\s*:\\s*(-?\\d+(?:\\.\\d+)?)`))
  if (!match) return undefined
  const value = Number(match[1])
  return Number.isFinite(value) ? value : undefined
}

function extractBooleanField(raw: string, key: string): boolean | undefined {
  const match = raw.match(new RegExp(`"${key}"\\s*:\\s*(true|false)`))
  if (!match) return undefined
  return match[1] === 'true'
}

function extractEnumField<T extends string>(
  raw: string,
  key: string,
  allowed: readonly T[],
): T | undefined {
  const value = extractStringField(raw, key)
  return value && allowed.includes(value as T) ? (value as T) : undefined
}

async function readSessionMetaFast(filePath: string, id: string): Promise<SessionMeta | null> {
  let handle = null
  try {
    handle = await open(filePath, 'r')
    const buffer = Buffer.alloc(SESSION_META_HEADER_BYTES)
    const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0)
    const head = buffer.toString('utf-8', 0, bytesRead)
    if (!/"version"\s*:\s*1/.test(head) || !/"messages"\s*:/.test(head)) return null

    const projectDir = extractStringField(head, 'projectDir') || ''
    if (!projectDir) return null

    const savedAt = extractNumberField(head, 'savedAt') ?? 0
    const lastMessageAt = extractNumberField(head, 'lastMessageAt') ?? savedAt

    return {
      id: extractStringField(head, 'id') || id,
      projectDir,
      workspaceDir: extractStringField(head, 'workspaceDir'),
      cwdSubpath: extractStringField(head, 'cwdSubpath'),
      repoRoot: extractStringField(head, 'repoRoot'),
      branch: extractStringField(head, 'branch'),
      workspaceType: extractEnumField(head, 'workspaceType', ['worktree', 'local', 'branch']),
      baseBranch: extractStringField(head, 'baseBranch'),
      title: extractStringField(head, 'title') || 'Untitled',
      savedAt,
      messageCount: extractNumberField(head, 'messageCount') ?? 0,
      model: extractStringField(head, 'model') || '',
      autoLevel: extractStringField(head, 'autoLevel') || 'default',
      missionDir: extractStringField(head, 'missionDir'),
      missionBaseSessionId: normalizeOptionalString(
        extractStringField(head, 'missionBaseSessionId'),
      ),
      isMission: extractBooleanField(head, 'isMission') === true ? true : undefined,
      sessionKind: extractEnumField(head, 'sessionKind', ['mission', 'normal']),
      interactionMode: extractEnumField(head, 'interactionMode', ['spec', 'auto', 'agi']),
      autonomyLevel: extractEnumField(head, 'autonomyLevel', ['off', 'low', 'medium', 'high']),
      decompSessionType: extractEnumField(head, 'decompSessionType', ['orchestrator']),
      reasoningEffort: extractStringField(head, 'reasoningEffort'),
      apiKeyFingerprint: extractStringField(head, 'apiKeyFingerprint'),
      pinned: extractBooleanField(head, 'pinned'),
      lastMessageAt,
    }
  } catch {
    return null
  } finally {
    await handle?.close().catch(() => {})
  }
}

export interface SessionStore {
  save: (req: SaveSessionRequest) => Promise<SessionMeta | null>
  load: (id: string) => Promise<LoadSessionResponse | null>
  list: () => Promise<SessionMeta[]>
  delete: (id: string) => Promise<boolean>
  clearContext: (id: string) => Promise<SessionMeta | null>
  replaceSessionId: (oldId: string, newId: string) => Promise<SessionMeta | null>
  sessionsDir: string
}

export function createSessionStore(opts: { baseDir: string }): SessionStore {
  const sessionsDir = join(opts.baseDir, 'sessions')

  const save = async (req: SaveSessionRequest): Promise<SessionMeta | null> => {
    const filePath = safeSessionFilePath(sessionsDir, req.id)
    if (!filePath) return null
    await ensureDir(sessionsDir)
    const savedAt = Date.now()
    const title = getTitleFromMessages(req.messages, req.branch || req.baseBranch)
    const messageCount = req.messages.length
    const lastMessageAt =
      req.messages.length > 0 ? req.messages[req.messages.length - 1].timestamp : savedAt

    const record = {
      version: 1,
      id: req.id,
      projectDir: req.projectDir,
      workspaceDir: req.workspaceDir,
      cwdSubpath: req.cwdSubpath,
      repoRoot: req.repoRoot,
      branch: req.branch,
      workspaceType: req.workspaceType,
      baseBranch: req.baseBranch,
      model: req.model,
      autoLevel: req.autoLevel,
      missionDir: req.missionDir,
      missionBaseSessionId: normalizeOptionalString(req.missionBaseSessionId),
      isMission: req.isMission,
      sessionKind: req.sessionKind,
      interactionMode: req.interactionMode,
      autonomyLevel: req.autonomyLevel,
      decompSessionType: req.decompSessionType,
      reasoningEffort: req.reasoningEffort,
      apiKeyFingerprint: req.apiKeyFingerprint,
      pinned: req.pinned,
      title,
      savedAt,
      lastMessageAt,
      messageCount,
      messages: req.messages,
      runtimeLogs: normalizeRuntimeLogs(req.runtimeLogs) || [],
    }

    await atomicWriteFile(filePath, JSON.stringify(record, null, 2))
    return {
      id: req.id,
      projectDir: req.projectDir,
      workspaceDir: req.workspaceDir,
      cwdSubpath: req.cwdSubpath,
      repoRoot: req.repoRoot,
      branch: req.branch,
      workspaceType: req.workspaceType,
      baseBranch: req.baseBranch,
      title,
      savedAt,
      messageCount,
      model: req.model,
      autoLevel: req.autoLevel,
      missionDir: req.missionDir,
      missionBaseSessionId: normalizeOptionalString(req.missionBaseSessionId),
      isMission: req.isMission,
      sessionKind: req.sessionKind,
      interactionMode: req.interactionMode,
      autonomyLevel: req.autonomyLevel,
      decompSessionType: req.decompSessionType,
      apiKeyFingerprint: req.apiKeyFingerprint,
      pinned: req.pinned,
      lastMessageAt,
    }
  }

  const load = async (id: string): Promise<LoadSessionResponse | null> => {
    const filePath = safeSessionFilePath(sessionsDir, id)
    if (!filePath) return null
    try {
      const raw = JSON.parse(await readFile(filePath, 'utf-8'))
      if (raw && typeof raw === 'object' && raw.version === 1) {
        const messages = (raw.messages || []) as ChatMessage[]
        const lastMessageAt =
          raw.lastMessageAt ??
          (messages.length > 0 ? messages[messages.length - 1].timestamp : Number(raw.savedAt || 0))
        return {
          id: String(raw.id || id),
          projectDir: String(raw.projectDir || ''),
          workspaceDir: typeof raw.workspaceDir === 'string' ? raw.workspaceDir : undefined,
          cwdSubpath: typeof raw.cwdSubpath === 'string' ? raw.cwdSubpath : undefined,
          repoRoot: typeof raw.repoRoot === 'string' ? raw.repoRoot : undefined,
          branch: typeof raw.branch === 'string' ? raw.branch : undefined,
          workspaceType:
            raw.workspaceType === 'worktree'
              ? 'worktree'
              : raw.workspaceType === 'local'
                ? 'local'
                : raw.workspaceType === 'branch'
                  ? 'branch'
                  : undefined,
          baseBranch: typeof raw.baseBranch === 'string' ? raw.baseBranch : undefined,
          model: String(raw.model || ''),
          autoLevel: String(raw.autoLevel || 'default'),
          missionDir: typeof raw.missionDir === 'string' ? raw.missionDir : undefined,
          missionBaseSessionId: normalizeOptionalString(raw.missionBaseSessionId),
          isMission: raw.isMission === true ? true : undefined,
          sessionKind:
            raw.sessionKind === 'mission'
              ? 'mission'
              : raw.sessionKind === 'normal'
                ? 'normal'
                : undefined,
          interactionMode:
            raw.interactionMode === 'spec' ||
            raw.interactionMode === 'auto' ||
            raw.interactionMode === 'agi'
              ? raw.interactionMode
              : undefined,
          autonomyLevel:
            raw.autonomyLevel === 'off' ||
            raw.autonomyLevel === 'low' ||
            raw.autonomyLevel === 'medium' ||
            raw.autonomyLevel === 'high'
              ? raw.autonomyLevel
              : undefined,
          decompSessionType:
            raw.decompSessionType === 'orchestrator' ? raw.decompSessionType : undefined,
          reasoningEffort:
            typeof raw.reasoningEffort === 'string' ? raw.reasoningEffort : undefined,
          apiKeyFingerprint:
            typeof raw.apiKeyFingerprint === 'string' ? raw.apiKeyFingerprint : undefined,
          pinned: typeof raw.pinned === 'boolean' ? raw.pinned : undefined,
          title: String(raw.title || getTitleFromMessages(messages)),
          savedAt: Number(raw.savedAt || 0),
          lastMessageAt,
          messages,
          runtimeLogs: normalizeRuntimeLogs(raw.runtimeLogs),
        }
      }

      // v0 fallback: {id,messages,savedAt}
      const messages = (raw?.messages || []) as ChatMessage[]
      const lastMessageAt =
        messages.length > 0 ? messages[messages.length - 1].timestamp : Number(raw?.savedAt || 0)
      return {
        id: String(raw?.id || id),
        projectDir: '',
        workspaceDir: undefined,
        cwdSubpath: undefined,
        repoRoot: undefined,
        branch: undefined,
        workspaceType: undefined,
        baseBranch: undefined,
        model: '',
        autoLevel: 'default',
        reasoningEffort: undefined,
        apiKeyFingerprint: undefined,
        title: getTitleFromMessages(messages),
        savedAt: Number(raw?.savedAt || 0),
        lastMessageAt,
        messages,
        runtimeLogs: normalizeRuntimeLogs(raw?.runtimeLogs),
      }
    } catch {
      return null
    }
  }

  const list = async (): Promise<SessionMeta[]> => {
    try {
      await ensureDir(sessionsDir)
      const files = (await readdir(sessionsDir)).filter((f) => f.endsWith('.json'))
      const metas = (
        await Promise.all(
          files.map(async (file) => {
            const id = file.slice(0, -5)
            const filePath = safeSessionFilePath(sessionsDir, id)
            const fastMeta = filePath ? await readSessionMetaFast(filePath, id) : null
            if (fastMeta) return fastMeta

            const data = await load(id)
            if (!data) return null
            return {
              id: data.id,
              projectDir: data.projectDir,
              workspaceDir: data.workspaceDir,
              cwdSubpath: data.cwdSubpath,
              repoRoot: data.repoRoot,
              branch: data.branch,
              workspaceType: data.workspaceType,
              baseBranch: data.baseBranch,
              title: data.title,
              savedAt: data.savedAt,
              messageCount: data.messages.length,
              model: data.model,
              autoLevel: data.autoLevel,
              missionDir: data.missionDir,
              missionBaseSessionId: data.missionBaseSessionId,
              isMission: data.isMission,
              sessionKind: data.sessionKind,
              interactionMode: data.interactionMode,
              autonomyLevel: data.autonomyLevel,
              decompSessionType: data.decompSessionType,
              reasoningEffort: data.reasoningEffort,
              apiKeyFingerprint: data.apiKeyFingerprint,
              pinned: data.pinned,
              lastMessageAt: data.lastMessageAt,
            } satisfies SessionMeta
          }),
        )
      ).filter(Boolean) as SessionMeta[]
      metas.sort((a, b) => (b.lastMessageAt ?? b.savedAt) - (a.lastMessageAt ?? a.savedAt))
      return metas
    } catch {
      return []
    }
  }

  const del = async (id: string): Promise<boolean> => {
    const filePath = safeSessionFilePath(sessionsDir, id)
    if (!filePath) return false
    try {
      await unlink(filePath)
      return true
    } catch {
      return false
    }
  }

  const clearContext = async (id: string): Promise<SessionMeta | null> => {
    const filePath = safeSessionFilePath(sessionsDir, id)
    if (!filePath) return null
    try {
      const raw = JSON.parse(await readFile(filePath, 'utf-8'))
      if (!raw || typeof raw !== 'object') return null

      const now = Date.now()
      const prevMessages = Array.isArray((raw as any).messages)
        ? ((raw as any).messages as ChatMessage[])
        : []
      const titleRaw = typeof (raw as any).title === 'string' ? (raw as any).title : ''
      const title = titleRaw.trim() || getTitleFromMessages(prevMessages)

      const record = {
        ...(raw as any),
        version: 1,
        id: String((raw as any).id || id),
        title,
        savedAt: now,
        lastMessageAt: now,
        messageCount: 0,
        messages: [],
        runtimeLogs: [],
      }

      await atomicWriteFile(filePath, JSON.stringify(record, null, 2))

      return {
        id: String((raw as any).id || id),
        projectDir: String((raw as any).projectDir || ''),
        workspaceDir:
          typeof (raw as any).workspaceDir === 'string' ? (raw as any).workspaceDir : undefined,
        cwdSubpath:
          typeof (raw as any).cwdSubpath === 'string' ? (raw as any).cwdSubpath : undefined,
        repoRoot: typeof (raw as any).repoRoot === 'string' ? (raw as any).repoRoot : undefined,
        branch: typeof (raw as any).branch === 'string' ? (raw as any).branch : undefined,
        workspaceType:
          (raw as any).workspaceType === 'worktree'
            ? 'worktree'
            : (raw as any).workspaceType === 'local'
              ? 'local'
              : (raw as any).workspaceType === 'branch'
                ? 'branch'
                : undefined,
        baseBranch:
          typeof (raw as any).baseBranch === 'string' ? (raw as any).baseBranch : undefined,
        title,
        savedAt: now,
        messageCount: 0,
        model: String((raw as any).model || ''),
        autoLevel: String((raw as any).autoLevel || 'default'),
        missionDir:
          typeof (raw as any).missionDir === 'string' ? (raw as any).missionDir : undefined,
        missionBaseSessionId: normalizeOptionalString((raw as any).missionBaseSessionId),
        isMission: (raw as any).isMission === true ? true : undefined,
        sessionKind:
          (raw as any).sessionKind === 'mission'
            ? 'mission'
            : (raw as any).sessionKind === 'normal'
              ? 'normal'
              : undefined,
        interactionMode:
          (raw as any).interactionMode === 'spec' ||
          (raw as any).interactionMode === 'auto' ||
          (raw as any).interactionMode === 'agi'
            ? (raw as any).interactionMode
            : undefined,
        autonomyLevel:
          (raw as any).autonomyLevel === 'off' ||
          (raw as any).autonomyLevel === 'low' ||
          (raw as any).autonomyLevel === 'medium' ||
          (raw as any).autonomyLevel === 'high'
            ? (raw as any).autonomyLevel
            : undefined,
        decompSessionType:
          (raw as any).decompSessionType === 'orchestrator'
            ? (raw as any).decompSessionType
            : undefined,
        reasoningEffort:
          typeof (raw as any).reasoningEffort === 'string'
            ? (raw as any).reasoningEffort
            : undefined,
        apiKeyFingerprint:
          typeof (raw as any).apiKeyFingerprint === 'string'
            ? (raw as any).apiKeyFingerprint
            : undefined,
        lastMessageAt: now,
      }
    } catch {
      return null
    }
  }

  const replaceSessionId = async (oldId: string, newId: string): Promise<SessionMeta | null> => {
    const oldPath = safeSessionFilePath(sessionsDir, oldId)
    const newPath = safeSessionFilePath(sessionsDir, newId)
    if (!oldPath || !newPath) return null
    try {
      const raw = JSON.parse(await readFile(oldPath, 'utf-8'))
      if (!raw || typeof raw !== 'object') return null

      const now = Date.now()
      const prevMessages = Array.isArray((raw as any).messages)
        ? ((raw as any).messages as ChatMessage[])
        : []
      const titleRaw = typeof (raw as any).title === 'string' ? (raw as any).title : ''
      const title = titleRaw.trim() || getTitleFromMessages(prevMessages)
      const missionBaseSessionId = normalizeOptionalString((raw as any).missionBaseSessionId)
      const isMission = (raw as any).isMission === true || (raw as any).sessionKind === 'mission'

      const record = {
        ...(raw as any),
        version: 1,
        id: newId,
        missionBaseSessionId:
          missionBaseSessionId || (isMission ? String((raw as any).id || oldId) : undefined),
        title,
        savedAt: now,
        lastMessageAt: now,
        messageCount: 0,
        messages: [],
        runtimeLogs: [],
      }

      await ensureDir(sessionsDir)
      await atomicWriteFile(newPath, JSON.stringify(record, null, 2))
      if (oldId !== newId) {
        try {
          await unlink(oldPath)
        } catch {
          /* ignore */
        }
      }

      return {
        id: newId,
        projectDir: String((raw as any).projectDir || ''),
        workspaceDir:
          typeof (raw as any).workspaceDir === 'string' ? (raw as any).workspaceDir : undefined,
        cwdSubpath:
          typeof (raw as any).cwdSubpath === 'string' ? (raw as any).cwdSubpath : undefined,
        repoRoot: typeof (raw as any).repoRoot === 'string' ? (raw as any).repoRoot : undefined,
        branch: typeof (raw as any).branch === 'string' ? (raw as any).branch : undefined,
        workspaceType:
          (raw as any).workspaceType === 'worktree'
            ? 'worktree'
            : (raw as any).workspaceType === 'local'
              ? 'local'
              : (raw as any).workspaceType === 'branch'
                ? 'branch'
                : undefined,
        baseBranch:
          typeof (raw as any).baseBranch === 'string' ? (raw as any).baseBranch : undefined,
        title,
        savedAt: now,
        messageCount: 0,
        model: String((raw as any).model || ''),
        autoLevel: String((raw as any).autoLevel || 'default'),
        missionDir:
          typeof (raw as any).missionDir === 'string' ? (raw as any).missionDir : undefined,
        missionBaseSessionId:
          missionBaseSessionId || (isMission ? String((raw as any).id || oldId) : undefined),
        isMission: (raw as any).isMission === true ? true : undefined,
        sessionKind:
          (raw as any).sessionKind === 'mission'
            ? 'mission'
            : (raw as any).sessionKind === 'normal'
              ? 'normal'
              : undefined,
        interactionMode:
          (raw as any).interactionMode === 'spec' ||
          (raw as any).interactionMode === 'auto' ||
          (raw as any).interactionMode === 'agi'
            ? (raw as any).interactionMode
            : undefined,
        autonomyLevel:
          (raw as any).autonomyLevel === 'off' ||
          (raw as any).autonomyLevel === 'low' ||
          (raw as any).autonomyLevel === 'medium' ||
          (raw as any).autonomyLevel === 'high'
            ? (raw as any).autonomyLevel
            : undefined,
        decompSessionType:
          (raw as any).decompSessionType === 'orchestrator'
            ? (raw as any).decompSessionType
            : undefined,
        reasoningEffort:
          typeof (raw as any).reasoningEffort === 'string'
            ? (raw as any).reasoningEffort
            : undefined,
        apiKeyFingerprint:
          typeof (raw as any).apiKeyFingerprint === 'string'
            ? (raw as any).apiKeyFingerprint
            : undefined,
        lastMessageAt: now,
      }
    } catch {
      return null
    }
  }

  return { save, load, list, delete: del, clearContext, replaceSessionId, sessionsDir }
}
