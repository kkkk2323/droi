import { join, resolve, sep } from 'path'
import { readdir, readFile, unlink } from 'fs/promises'
import type { ChatMessage, LoadSessionResponse, SaveSessionRequest, SessionMeta } from '../../shared/protocol'
import { atomicWriteFile, ensureDir } from './fsUtils.ts'

const SESSION_ID_RE = /^[A-Za-z0-9_-]{1,128}$/

function getTitleFromMessages(messages: ChatMessage[], fallback?: string): string {
  const firstUser = messages.find((m) => m.role === 'user')
  const cmd = firstUser?.blocks?.find((b: any) => b?.kind === 'command')
  const cmdName = cmd && typeof (cmd as any).name === 'string' ? String((cmd as any).name) : ''
  const skill = firstUser?.blocks?.find((b: any) => b?.kind === 'skill')
  const skillName = skill && typeof (skill as any).name === 'string' ? String((skill as any).name) : ''
  const firstTextBlock = firstUser?.blocks?.find((b: any) => b?.kind === 'text')
  const text = firstTextBlock && typeof (firstTextBlock as any).content === 'string'
    ? String((firstTextBlock as any).content)
    : ''

  const branchFallback = String(fallback || '').trim()
  const branchTail = branchFallback ? (branchFallback.split('/').pop() || branchFallback) : ''

  const titleSource = cmdName
    ? `/${cmdName}${text.trim() ? ` ${text.trim()}` : ''}`
    : skillName
      ? `/${skillName}${text.trim() ? ` ${text.trim()}` : ''}`
      : (text || branchTail || 'Untitled')

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
    const lastMessageAt = req.messages.length > 0
      ? req.messages[req.messages.length - 1].timestamp
      : savedAt

    const record = {
      version: 1,
      id: req.id,
      projectDir: req.projectDir,
      repoRoot: req.repoRoot,
      branch: req.branch,
      workspaceType: req.workspaceType,
      baseBranch: req.baseBranch,
      model: req.model,
      autoLevel: req.autoLevel,
      reasoningEffort: req.reasoningEffort,
      apiKeyFingerprint: req.apiKeyFingerprint,
      pinned: req.pinned,
      title,
      savedAt,
      lastMessageAt,
      messages: req.messages,
    }

    await atomicWriteFile(filePath, JSON.stringify(record, null, 2))
    return {
      id: req.id,
      projectDir: req.projectDir,
      repoRoot: req.repoRoot,
      branch: req.branch,
      workspaceType: req.workspaceType,
      baseBranch: req.baseBranch,
      title,
      savedAt,
      messageCount,
      model: req.model,
      autoLevel: req.autoLevel,
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
        const lastMessageAt = raw.lastMessageAt ?? (messages.length > 0 ? messages[messages.length - 1].timestamp : Number(raw.savedAt || 0))
        return {
          id: String(raw.id || id),
          projectDir: String(raw.projectDir || ''),
          repoRoot: typeof raw.repoRoot === 'string' ? raw.repoRoot : undefined,
          branch: typeof raw.branch === 'string' ? raw.branch : undefined,
          workspaceType: raw.workspaceType === 'worktree' ? 'worktree' : (raw.workspaceType === 'branch' ? 'branch' : undefined),
          baseBranch: typeof raw.baseBranch === 'string' ? raw.baseBranch : undefined,
          model: String(raw.model || ''),
          autoLevel: String(raw.autoLevel || 'default'),
          reasoningEffort: typeof raw.reasoningEffort === 'string' ? raw.reasoningEffort : undefined,
          apiKeyFingerprint: typeof raw.apiKeyFingerprint === 'string' ? raw.apiKeyFingerprint : undefined,
          pinned: typeof raw.pinned === 'boolean' ? raw.pinned : undefined,
          title: String(raw.title || getTitleFromMessages(messages)),
          savedAt: Number(raw.savedAt || 0),
          lastMessageAt,
          messages,
        }
      }

      // v0 fallback: {id,messages,savedAt}
      const messages = (raw?.messages || []) as ChatMessage[]
      const lastMessageAt = messages.length > 0 ? messages[messages.length - 1].timestamp : Number(raw?.savedAt || 0)
      return {
        id: String(raw?.id || id),
        projectDir: '',
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
      }
    } catch {
      return null
    }
  }

  const list = async (): Promise<SessionMeta[]> => {
    try {
      await ensureDir(sessionsDir)
      const files = (await readdir(sessionsDir)).filter((f) => f.endsWith('.json'))
      const metas: SessionMeta[] = []
      for (const file of files) {
        const id = file.slice(0, -5)
        const data = await load(id)
        if (!data) continue
        metas.push({
          id: data.id,
          projectDir: data.projectDir,
          repoRoot: data.repoRoot,
          branch: data.branch,
          workspaceType: data.workspaceType,
          baseBranch: data.baseBranch,
          title: data.title,
          savedAt: data.savedAt,
          messageCount: data.messages.length,
          model: data.model,
          autoLevel: data.autoLevel,
          reasoningEffort: data.reasoningEffort,
          apiKeyFingerprint: data.apiKeyFingerprint,
          pinned: data.pinned,
          lastMessageAt: data.lastMessageAt,
        })
      }
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
      const prevMessages = Array.isArray((raw as any).messages) ? ((raw as any).messages as ChatMessage[]) : []
      const titleRaw = typeof (raw as any).title === 'string' ? (raw as any).title : ''
      const title = titleRaw.trim() || getTitleFromMessages(prevMessages)

      const record = {
        ...(raw as any),
        version: 1,
        id: String((raw as any).id || id),
        title,
        savedAt: now,
        lastMessageAt: now,
        messages: [],
      }

      await atomicWriteFile(filePath, JSON.stringify(record, null, 2))

      return {
        id: String((raw as any).id || id),
        projectDir: String((raw as any).projectDir || ''),
        repoRoot: typeof (raw as any).repoRoot === 'string' ? (raw as any).repoRoot : undefined,
        branch: typeof (raw as any).branch === 'string' ? (raw as any).branch : undefined,
        workspaceType: (raw as any).workspaceType === 'worktree'
          ? 'worktree'
          : ((raw as any).workspaceType === 'branch' ? 'branch' : undefined),
        baseBranch: typeof (raw as any).baseBranch === 'string' ? (raw as any).baseBranch : undefined,
        title,
        savedAt: now,
        messageCount: 0,
        model: String((raw as any).model || ''),
        autoLevel: String((raw as any).autoLevel || 'default'),
        reasoningEffort: typeof (raw as any).reasoningEffort === 'string' ? (raw as any).reasoningEffort : undefined,
        apiKeyFingerprint: typeof (raw as any).apiKeyFingerprint === 'string' ? (raw as any).apiKeyFingerprint : undefined,
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
      const prevMessages = Array.isArray((raw as any).messages) ? ((raw as any).messages as ChatMessage[]) : []
      const titleRaw = typeof (raw as any).title === 'string' ? (raw as any).title : ''
      const title = titleRaw.trim() || getTitleFromMessages(prevMessages)

      const record = {
        ...(raw as any),
        version: 1,
        id: newId,
        title,
        savedAt: now,
        lastMessageAt: now,
        messages: [],
      }

      await ensureDir(sessionsDir)
      await atomicWriteFile(newPath, JSON.stringify(record, null, 2))
      if (oldId !== newId) {
        try { await unlink(oldPath) } catch { /* ignore */ }
      }

      return {
        id: newId,
        projectDir: String((raw as any).projectDir || ''),
        repoRoot: typeof (raw as any).repoRoot === 'string' ? (raw as any).repoRoot : undefined,
        branch: typeof (raw as any).branch === 'string' ? (raw as any).branch : undefined,
        workspaceType: (raw as any).workspaceType === 'worktree'
          ? 'worktree'
          : ((raw as any).workspaceType === 'branch' ? 'branch' : undefined),
        baseBranch: typeof (raw as any).baseBranch === 'string' ? (raw as any).baseBranch : undefined,
        title,
        savedAt: now,
        messageCount: 0,
        model: String((raw as any).model || ''),
        autoLevel: String((raw as any).autoLevel || 'default'),
        reasoningEffort: typeof (raw as any).reasoningEffort === 'string' ? (raw as any).reasoningEffort : undefined,
        apiKeyFingerprint: typeof (raw as any).apiKeyFingerprint === 'string' ? (raw as any).apiKeyFingerprint : undefined,
        lastMessageAt: now,
      }
    } catch {
      return null
    }
  }

  return { save, load, list, delete: del, clearContext, replaceSessionId, sessionsDir }
}
