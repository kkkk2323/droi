import { create } from 'zustand'
import { getDroidClient } from '@/droidClient'
import type { ChatMessage, Project, SessionMeta, WorkspaceInfo, ProjectSettings } from '@/types'
import type { DroidPermissionOption, CustomModelDef } from '@/types'
import { buildHookMismatchMessage, getMissingDroidHooks } from '@/lib/droidHooks'
import { uuidv4 } from './lib/uuid.ts'
import {
  defaultSessionTitleFromBranch,
  generateWorktreeBranch,
  sanitizeWorktreePrefix,
} from '@/lib/sessionWorktree'
import { isTraceChainEnabled, setTraceChainEnabledOverride } from '@/lib/notificationFingerprint'
import { getModelDefaultReasoning } from '@/types'
import {
  DEFAULT_AUTO_LEVEL,
  DEFAULT_MODEL,
  makeBuffer,
  applySetupScriptEvent,
  appendDebugTrace,
  clearDebugTrace,
  setDebugTraceMaxLinesOverride,
  applyTurnEnd,
  applyError,
  markSetupScriptSkipped,
  type SessionBuffer,
  type SessionSetupState,
  type PendingPermissionRequest,
  type PendingAskUserRequest,
} from '@/state/appReducer'
import {
  getRepoKey,
  getTitleFromPrompt,
  upsertSessionMeta,
  updateSessionTitle,
  replaceSessionIdInProjects,
} from './store/projectHelpers'

const droid = getDroidClient()

export type SendInput = string | { text: string; tag?: { type: 'command' | 'skill'; name: string } }

export type PendingNewSession = {
  repoRoot: string
  branch: string
  isExistingBranch?: boolean
}

type PendingInitialSend = {
  sessionId: string
  input: SendInput
  attachments: Array<{ name: string; path: string }>
}

// --- Zustand Store ---

interface AppState {
  // Session buffers
  sessionBuffers: Map<string, SessionBuffer>
  activeSessionId: string
  activeProjectDir: string

  // New-session flow (deferred creation)
  pendingNewSession: PendingNewSession | null
  pendingInitialSend: PendingInitialSend | null

  // App-level state
  appVersion: string
  droidVersion: string
  apiKey: string
  traceChainEnabled: boolean
  showDebugTrace: boolean
  debugTraceMaxLines: number | null
  localDiagnosticsEnabled: boolean
  localDiagnosticsRetentionDays: number
  localDiagnosticsMaxTotalMb: number
  diagnosticsDir: string
  commitMessageModelId: string
  lanAccessEnabled: boolean
  customModels: CustomModelDef[]
  projects: Project[]
  projectSettingsByRepo: Record<string, ProjectSettings>
  workspaceError: string
  deletingSessionIds: Set<string>
  isCreatingSession: boolean

  // Generation tracking
  _sessionGenerations: Map<string, number>
  _hookMismatchReported: boolean
  _initialLoadDone: boolean
}

interface AppActions {
  // Internal setters
  _setSessionBuffers: (
    updater: (prev: Map<string, SessionBuffer>) => Map<string, SessionBuffer>,
  ) => void
  _setProjects: (updater: (prev: Project[]) => Project[]) => void

  // Derived getters (computed from state)
  getMessages: () => ChatMessage[]
  getIsRunning: () => boolean
  getIsAnyRunning: () => boolean
  getSessionRunning: (sessionId: string) => boolean
  getSessionDeleting: (sessionId: string) => boolean
  getDebugTrace: () => string[]
  getSetupScript: () => SessionSetupState | null
  getIsSetupBlocked: () => boolean
  getModel: () => string
  getAutoLevel: () => string
  getReasoningEffort: () => string
  getPendingPermissionRequest: () => PendingPermissionRequest | null
  getPendingAskUserRequest: () => PendingAskUserRequest | null
  getPendingSendMessageIds: () => Record<string, true>
  getActiveSessionTitle: () => string

  // Settings actions
  clearWorkspaceError: () => void
  clearDebugTrace: () => void
  appendUiDebugTrace: (message: string) => void
  setModel: (m: string) => void
  setAutoLevel: (l: string) => void
  setReasoningEffort: (r: string) => void
  setApiKey: (k: string) => void
  setTraceChainEnabled: (enabled: boolean) => void
  setShowDebugTrace: (enabled: boolean) => void
  setDebugTraceMaxLines: (maxLines: number | null) => void
  setLocalDiagnosticsEnabled: (enabled: boolean) => void
  setLocalDiagnosticsRetention: (params: { retentionDays: number; maxTotalMb: number }) => void
  setCommitMessageModelId: (modelId: string) => void
  setLanAccessEnabled: (enabled: boolean) => void
  refreshDiagnosticsDir: () => Promise<void>
  exportDiagnostics: (params?: { sessionId?: string }) => Promise<{ path: string }>
  openPath: (path: string) => Promise<void>
  updateProjectSettings: (repoRoot: string, patch: Partial<ProjectSettings>) => Promise<void>

  // New-session flow (deferred creation)
  updatePendingNewSession: (patch: Partial<PendingNewSession>) => void
  clearPendingNewSession: () => void

  // Session actions
  handleSend: (input: SendInput, attachments?: Array<{ name: string; path: string }>) => void
  handleClearSessionContext: (sessionId?: string) => Promise<void>
  handleCancel: () => void
  handleForceCancel: () => void
  handleRespondPermission: (params: {
    selectedOption: DroidPermissionOption
    autoLevel?: 'low' | 'medium' | 'high'
  }) => void
  handleRespondAskUser: (params: {
    cancelled?: boolean
    answers: Array<{ index: number; question: string; answer: string }>
  }) => void
  handleRetrySetupScript: (sessionId?: string) => Promise<void>
  handleSkipSetupScript: (sessionId?: string) => void
  handleNewSession: (repoRoot?: string) => void
  handleCreateSessionWithWorkspace: (params: {
    repoRoot?: string
    projectDir?: string
    mode: 'plain' | 'switch-branch' | 'new-branch' | 'new-worktree'
    branch?: string
    baseBranch?: string
  }) => Promise<string | null>
  handleSwitchWorkspaceForSession: (params: {
    branch: string
    sessionId?: string
  }) => Promise<boolean>
  handleAddProject: () => void
  handleSetProjectDir: (dir: string) => void
  handleSelectSession: (sessionId: string) => void
  handleTogglePin: (sessionId: string) => void
  handleDeleteSession: (sessionId: string) => void
  handleDeleteProject: (repoRoot: string) => void

  // Internal helpers
  _bumpSessionGeneration: (sid: string) => number
  _isSessionGenerationCurrent: (sid: string, generation: number) => boolean
  _clearSessionGeneration: (sid: string) => void
  _getSessionGeneration: (sid: string) => number
  _resolveWorkspace: (projectDir: string) => Promise<WorkspaceInfo | null>
  _pickProjectDirForRepo: (repoRoot?: string) => string
  _saveSessionToDisk: (sid: string, buf: SessionBuffer) => Promise<void>
  _runSetupScriptForSession: (params: {
    sessionId: string
    projectDir: string
    script: string
  }) => Promise<void>
  _ensureWorkspaceForMeta: (meta: SessionMeta) => Promise<WorkspaceInfo>
  _switchToSessionWithAligned: (selectedMeta: SessionMeta, aligned: WorkspaceInfo) => Promise<void>
  _reportHookMismatch: (sid: string, projectDir: string, missingHooks: string[]) => void

  _ensureProjectSettingsInitialized: (params: {
    repoRoot: string
    hintBranch?: string
  }) => Promise<void>

  // New-session helpers
  _confirmPendingNewSessionAndSend: (params: {
    input: SendInput
    attachments?: Array<{ name: string; path: string }>
  }) => Promise<void>
  _maybeFlushPendingInitialSend: () => void
}

type AppStore = AppState & AppActions

export const useAppStore = create<AppStore>((set, get) => ({
  // --- Initial State ---
  sessionBuffers: new Map(),
  activeSessionId: '',
  activeProjectDir: '',
  pendingNewSession: null,
  pendingInitialSend: null,
  appVersion: 'loading...',
  droidVersion: 'loading...',
  apiKey: '',
  traceChainEnabled: isTraceChainEnabled(),
  showDebugTrace: false,
  debugTraceMaxLines: null,
  localDiagnosticsEnabled: true,
  localDiagnosticsRetentionDays: 7,
  localDiagnosticsMaxTotalMb: 50,
  diagnosticsDir: '',
  commitMessageModelId: 'minimax-m2.5',
  lanAccessEnabled: false,
  customModels: [],
  projects: [],
  projectSettingsByRepo: {},
  workspaceError: '',
  deletingSessionIds: new Set(),
  isCreatingSession: false,
  _sessionGenerations: new Map(),
  _hookMismatchReported: false,
  _initialLoadDone: false,

  // --- Internal setters ---
  _setSessionBuffers: (updater) => set((s) => ({ sessionBuffers: updater(s.sessionBuffers) })),
  _setProjects: (updater) => set((s) => ({ projects: updater(s.projects) })),

  // --- Derived getters ---
  getMessages: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.messages ?? []
  },
  getIsRunning: () => {
    const s = get()
    const buf = s.sessionBuffers.get(s.activeSessionId)
    return Boolean(buf?.isRunning)
  },
  getIsAnyRunning: () => {
    const s = get()
    return Array.from(s.sessionBuffers.values()).some((b) => Boolean(b?.isRunning))
  },
  getSessionRunning: (sessionId) => {
    const buf = get().sessionBuffers.get(sessionId)
    return Boolean(buf?.isRunning)
  },
  getSessionDeleting: (sessionId) => {
    return get().deletingSessionIds.has(sessionId)
  },
  getDebugTrace: () => {
    const s = get()
    return (s.sessionBuffers.get(s.activeSessionId)?.debugTrace ?? []) as string[]
  },
  getSetupScript: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.setupScript ?? null
  },
  getIsSetupBlocked: () => {
    const s = get()
    const buf = s.sessionBuffers.get(s.activeSessionId)
    return Boolean(buf && (buf.isSetupRunning || buf.setupScript.status === 'failed'))
  },
  getModel: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.model ?? DEFAULT_MODEL
  },
  getAutoLevel: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.autoLevel ?? DEFAULT_AUTO_LEVEL
  },
  getReasoningEffort: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.reasoningEffort ?? ''
  },
  getPendingPermissionRequest: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.pendingPermissionRequests?.[0] ?? null
  },
  getPendingAskUserRequest: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.pendingAskUserRequests?.[0] ?? null
  },
  getPendingSendMessageIds: () => {
    const s = get()
    return s.sessionBuffers.get(s.activeSessionId)?.pendingSendMessageIds || {}
  },
  getActiveSessionTitle: () => {
    const s = get()
    for (const p of s.projects) {
      const sess = p.sessions.find((x) => x.id === s.activeSessionId)
      if (sess) return sess.title
    }
    return ''
  },

  // --- Generation tracking ---
  _bumpSessionGeneration: (sid) => {
    const gens = get()._sessionGenerations
    const next = (gens.get(sid) ?? 0) + 1
    const newGens = new Map(gens)
    newGens.set(sid, next)
    set({ _sessionGenerations: newGens })
    return next
  },
  _isSessionGenerationCurrent: (sid, generation) => {
    return (get()._sessionGenerations.get(sid) ?? 0) === generation
  },
  _clearSessionGeneration: (sid) => {
    const gens = new Map(get()._sessionGenerations)
    gens.delete(sid)
    set({ _sessionGenerations: gens })
  },
  _getSessionGeneration: (sid) => {
    return get()._sessionGenerations.get(sid) ?? 0
  },

  // --- Workspace helpers ---
  _resolveWorkspace: async (projectDir) => {
    if (!projectDir) return null
    return droid.getWorkspaceInfo({ projectDir })
  },

  _pickProjectDirForRepo: (repoRoot) => {
    const s = get()
    if (!repoRoot) return s.activeProjectDir
    const activeBuf = s.activeSessionId ? s.sessionBuffers.get(s.activeSessionId) : null
    if (activeBuf && (activeBuf.repoRoot || activeBuf.projectDir) === repoRoot)
      return activeBuf.projectDir

    const p = s.projects.find((x) => x.dir === repoRoot)
    if (!p || p.sessions.length === 0) return repoRoot
    const latest = [...p.sessions].sort(
      (a, b) => (b.lastMessageAt ?? b.savedAt) - (a.lastMessageAt ?? a.savedAt),
    )[0]
    return latest?.projectDir || repoRoot
  },

  _saveSessionToDisk: async (sid, buf) => {
    if (!buf.projectDir || !sid) return
    const existingMeta = get()
      .projects.flatMap((p) => p.sessions)
      .find((x) => x.id === sid)
    const meta = await droid.saveSession({
      id: sid,
      projectDir: buf.projectDir,
      repoRoot: buf.repoRoot,
      branch: buf.branch,
      workspaceType: buf.workspaceType,
      baseBranch: buf.baseBranch,
      model: buf.model || DEFAULT_MODEL,
      autoLevel: buf.autoLevel || DEFAULT_AUTO_LEVEL,
      reasoningEffort: buf.reasoningEffort || undefined,
      apiKeyFingerprint: buf.apiKeyFingerprint || undefined,
      pinned: existingMeta?.pinned || undefined,
      messages: buf.messages,
    })
    if (!meta) return

    const normalizedMeta: SessionMeta = {
      ...meta,
      projectDir: meta.projectDir || buf.projectDir,
      repoRoot: meta.repoRoot || buf.repoRoot || meta.projectDir || buf.projectDir,
      branch: meta.branch || buf.branch,
      workspaceType: meta.workspaceType || buf.workspaceType,
      baseBranch: meta.baseBranch || buf.baseBranch,
    }
    get()._setProjects((prev) => upsertSessionMeta(prev, normalizedMeta))
  },

  _reportHookMismatch: (sid, projectDir, missingHooks) => {
    if (get()._hookMismatchReported) return
    set({ _hookMismatchReported: true })
    const message = buildHookMismatchMessage(missingHooks)
    get()._setSessionBuffers((prev) => {
      const session = prev.get(sid) || makeBuffer(projectDir)
      let next = new Map(prev)
      next.set(sid, session)
      next = appendDebugTrace(
        next,
        sid,
        `hook-check-failed: missing-hooks=${missingHooks.join(',')}`,
      )
      next = applyError(next, sid, message)
      return next
    })
  },

  // --- Settings actions ---
  clearWorkspaceError: () => set({ workspaceError: '' }),

  clearDebugTrace: () => {
    const sid = get().activeSessionId
    if (!sid) return
    get()._setSessionBuffers((prev) => clearDebugTrace(prev, sid))
  },

  appendUiDebugTrace: (message) => {
    const sid = get().activeSessionId
    if (!sid) return
    get()._setSessionBuffers((prev) => appendDebugTrace(prev, sid, String(message || '')))
    if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
      ;(droid as any).appendDiagnosticsEvent({
        sessionId: sid,
        event: 'ui.debug',
        level: 'debug',
        data: { message: String(message || '').slice(0, 2000) },
      })
    }
  },

  setModel: (m) => {
    const sid = get().activeSessionId
    get()._setSessionBuffers((prev) => {
      const buf = prev.get(sid)
      if (!buf) return prev
      const next = new Map(prev)
      next.set(sid, { ...buf, model: m, reasoningEffort: getModelDefaultReasoning(m) })
      return next
    })
  },

  setAutoLevel: (l) => {
    const sid = get().activeSessionId
    get()._setSessionBuffers((prev) => {
      const buf = prev.get(sid)
      if (!buf) return prev
      const next = new Map(prev)
      next.set(sid, { ...buf, autoLevel: l })
      return next
    })
  },

  setReasoningEffort: (r) => {
    const sid = get().activeSessionId
    get()._setSessionBuffers((prev) => {
      const buf = prev.get(sid)
      if (!buf) return prev
      const next = new Map(prev)
      next.set(sid, { ...buf, reasoningEffort: r })
      return next
    })
  },

  setApiKey: (k) => {
    set({ apiKey: k })
    droid.setApiKey(k)
  },

  setTraceChainEnabled: (enabled) => {
    const next = Boolean(enabled)
    setTraceChainEnabledOverride(next)
    set({ traceChainEnabled: next })
    if (typeof (droid as any)?.setTraceChainEnabled === 'function') {
      ;(droid as any).setTraceChainEnabled(next)
    }
  },

  setShowDebugTrace: (enabled) => {
    const next = Boolean(enabled)
    set({ showDebugTrace: next })
    droid.setShowDebugTrace(next)
  },

  setDebugTraceMaxLines: (maxLines) => {
    const next =
      typeof maxLines === 'number' && Number.isFinite(maxLines)
        ? Math.min(10_000, Math.max(1, Math.floor(maxLines)))
        : null
    setDebugTraceMaxLinesOverride(next)
    set({ debugTraceMaxLines: next })
    ;(droid as any).setDebugTraceMaxLines?.(next)
  },

  setLocalDiagnosticsEnabled: (enabled) => {
    const next = Boolean(enabled)
    set({ localDiagnosticsEnabled: next })
    if (typeof (droid as any)?.setLocalDiagnosticsEnabled === 'function') {
      ;(droid as any).setLocalDiagnosticsEnabled(next)
    }
    if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
      ;(droid as any).appendDiagnosticsEvent({
        sessionId: get().activeSessionId || null,
        event: 'ui.diagnostics.toggle',
        level: 'info',
        data: { enabled: next },
      })
    }
  },

  setLocalDiagnosticsRetention: (params) => {
    const s = get()
    const days =
      typeof params?.retentionDays === 'number' && Number.isFinite(params.retentionDays)
        ? Math.max(1, Math.floor(params.retentionDays))
        : s.localDiagnosticsRetentionDays
    const mb =
      typeof params?.maxTotalMb === 'number' && Number.isFinite(params.maxTotalMb)
        ? Math.max(1, Math.floor(params.maxTotalMb))
        : s.localDiagnosticsMaxTotalMb

    set({ localDiagnosticsRetentionDays: days, localDiagnosticsMaxTotalMb: mb })
    if (typeof (droid as any)?.setLocalDiagnosticsRetention === 'function') {
      ;(droid as any).setLocalDiagnosticsRetention({ retentionDays: days, maxTotalMb: mb })
    }
    if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
      ;(droid as any).appendDiagnosticsEvent({
        sessionId: get().activeSessionId || null,
        event: 'ui.diagnostics.retention',
        level: 'info',
        data: { retentionDays: days, maxTotalMb: mb },
      })
    }
  },

  setCommitMessageModelId: (modelId) => {
    const next = String(modelId || '').trim() || 'minimax-m2.5'
    set({ commitMessageModelId: next })
    if (typeof (droid as any)?.setCommitMessageModelId === 'function') {
      ;(droid as any).setCommitMessageModelId(next)
    }
  },

  setLanAccessEnabled: (enabled) => {
    const next = Boolean(enabled)
    set({ lanAccessEnabled: next })
    if (typeof (droid as any)?.setLanAccessEnabled === 'function') {
      ;(droid as any).setLanAccessEnabled(next)
    }
  },

  refreshDiagnosticsDir: async () => {
    if (typeof (droid as any)?.getDiagnosticsDir !== 'function') return
    const dir = await (droid as any).getDiagnosticsDir().catch(() => '')
    set({ diagnosticsDir: typeof dir === 'string' ? dir : '' })
  },

  openPath: async (path) => {
    const p = String(path || '').trim()
    if (!p) return
    if (typeof (droid as any)?.openPath === 'function') {
      await (droid as any).openPath(p)
    }
  },

  exportDiagnostics: async (params) => {
    if (typeof (droid as any)?.exportDiagnostics !== 'function') return { path: '' }
    const s = get()
    const sid = String(params?.sessionId || s.activeSessionId || '').trim()
    const debugTraceText = s.getDebugTrace().join('\n')
    return await (droid as any).exportDiagnostics({ sessionId: sid || null, debugTraceText })
  },

  updateProjectSettings: async (repoRoot, patch) => {
    const key = String(repoRoot || '').trim()
    if (!key) return

    const prev = get().projectSettingsByRepo[key] || {}
    const next: ProjectSettings = {}
    if (typeof patch.baseBranch === 'string') next.baseBranch = patch.baseBranch.trim()
    if (typeof patch.worktreePrefix === 'string') {
      const trimmed = patch.worktreePrefix.trim()
      next.worktreePrefix = trimmed ? sanitizeWorktreePrefix(trimmed) : ''
    }
    if (typeof patch.setupScript === 'string') next.setupScript = patch.setupScript.trim()

    const merged: ProjectSettings = { ...prev, ...next }
    const state = await droid.updateProjectSettings({ repoRoot: key, settings: merged })
    const map = (state as any)?.projectSettings
    if (map && typeof map === 'object')
      set({ projectSettingsByRepo: map as Record<string, ProjectSettings> })
    else set((s) => ({ projectSettingsByRepo: { ...s.projectSettingsByRepo, [key]: merged } }))
  },

  _ensureProjectSettingsInitialized: async ({ repoRoot, hintBranch }) => {
    const key = String(repoRoot || '').trim()
    if (!key) return

    const existing = get().projectSettingsByRepo[key] || {}
    const nextPatch: Partial<ProjectSettings> = {}

    if (typeof existing.worktreePrefix !== 'string' || !existing.worktreePrefix.trim()) {
      nextPatch.worktreePrefix = 'droi'
    }

    if (typeof existing.baseBranch !== 'string' || !existing.baseBranch.trim()) {
      const branches = await droid.listGitBranches({ projectDir: key }).catch(() => [] as string[])
      const cleaned = Array.from(
        new Set(
          (branches || [])
            .filter(Boolean)
            .map((b) => String(b).trim())
            .filter(Boolean),
        ),
      )
      const hint = typeof hintBranch === 'string' ? hintBranch.trim() : ''
      const base = cleaned.includes('main')
        ? 'main'
        : cleaned.includes('master')
          ? 'master'
          : hint || cleaned[0] || ''
      if (base) nextPatch.baseBranch = base
    }

    if (Object.keys(nextPatch).length === 0) return
    await get().updateProjectSettings(key, nextPatch)
  },

  // --- New-session flow (deferred creation) ---
  updatePendingNewSession: (patch) => {
    const nextPatch = patch || {}
    set((prev) => {
      const cur = prev.pendingNewSession
      if (!cur) return {}
      return {
        pendingNewSession: {
          ...cur,
          ...(nextPatch as Partial<PendingNewSession>),
          repoRoot:
            typeof (nextPatch as any).repoRoot === 'string'
              ? String((nextPatch as any).repoRoot).trim()
              : cur.repoRoot,
          branch:
            typeof (nextPatch as any).branch === 'string'
              ? String((nextPatch as any).branch).trim()
              : cur.branch,
        },
      }
    })
  },
  clearPendingNewSession: () => set({ pendingNewSession: null, workspaceError: '' }),

  // --- Setup script ---
  _runSetupScriptForSession: async (params) => {
    const sessionId = String(params.sessionId || '').trim()
    const projectDir = String(params.projectDir || '').trim()
    const script = String(params.script || '').trim()
    if (!sessionId || !projectDir || !script) return

    get()._setSessionBuffers((prev) => {
      let next = appendDebugTrace(prev, sessionId, `setup-script-start: cwd=${projectDir}`)
      next = applySetupScriptEvent(next, sessionId, {
        type: 'started',
        sessionId,
        projectDir,
        script,
      })
      return next
    })

    try {
      await droid.runSetupScript({ sessionId, projectDir, script })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      get()._setSessionBuffers((prev) => {
        const session = prev.get(sessionId)
        if (!session) return prev
        let next = appendDebugTrace(prev, sessionId, `setup-script-start-failed: ${msg}`)
        const failed = new Map(next)
        failed.set(sessionId, {
          ...session,
          isSetupRunning: false,
          setupScript: {
            ...session.setupScript,
            script,
            status: 'failed',
            error: msg || 'Failed to start setup script',
            exitCode: null,
          },
        })
        return failed
      })
    }
  },

  handleRetrySetupScript: async (sessionId) => {
    const s = get()
    const sid = sessionId || s.activeSessionId
    if (!sid) return

    const session = s.sessionBuffers.get(sid)
    if (!session) return
    const script = String(session.setupScript?.script || '').trim()
    if (!script) return
    const projectDir = String(session.projectDir || '').trim()
    if (!projectDir) return

    await s._runSetupScriptForSession({ sessionId: sid, projectDir, script })
  },

  handleSkipSetupScript: (sessionId) => {
    const s = get()
    const sid = sessionId || s.activeSessionId
    if (!sid) return
    s._setSessionBuffers((prev) =>
      markSetupScriptSkipped(appendDebugTrace(prev, sid, 'setup-script-skipped'), sid),
    )
    s._maybeFlushPendingInitialSend()
  },

  // --- Session workspace ---
  handleCreateSessionWithWorkspace: async (params) => {
    const s = get()
    if (!s._initialLoadDone) return null
    if (s.isCreatingSession) return null
    set({ isCreatingSession: true })
    try {
      const sourceDir = params.projectDir || s._pickProjectDirForRepo(params.repoRoot)
      if (!sourceDir) throw new Error('No project directory available')

      const currentSid = s.activeSessionId
      const currentBuf = s.sessionBuffers.get(currentSid)
      if (currentBuf && currentBuf.messages.length > 0 && !currentBuf.isRunning) {
        void s._saveSessionToDisk(currentSid, currentBuf)
      }

      const inheritModel = currentBuf?.model ?? DEFAULT_MODEL
      const inheritAutoLevel = currentBuf?.autoLevel ?? DEFAULT_AUTO_LEVEL
      const inheritReasoningEffort = currentBuf?.reasoningEffort ?? ''

      let workspaceInfo: WorkspaceInfo | null = null
      if (params.mode === 'switch-branch') {
        if (!params.branch?.trim()) throw new Error('Missing branch')
        workspaceInfo = await droid.switchWorkspace({
          projectDir: sourceDir,
          branch: params.branch.trim(),
        })
        if (!workspaceInfo) throw new Error('Failed to switch branch')
      } else if (params.mode === 'new-branch') {
        if (!params.branch?.trim()) throw new Error('Missing branch')
        workspaceInfo = await droid.createWorkspace({
          projectDir: sourceDir,
          mode: 'branch',
          branch: params.branch.trim(),
          baseBranch: params.baseBranch?.trim() || undefined,
        })
        if (!workspaceInfo) throw new Error('Failed to create branch')
      } else if (params.mode === 'new-worktree') {
        if (!params.branch?.trim()) throw new Error('Missing branch')
        workspaceInfo = await droid.createWorkspace({
          projectDir: sourceDir,
          mode: 'worktree',
          branch: params.branch.trim(),
          baseBranch: params.baseBranch?.trim() || undefined,
        })
        if (!workspaceInfo) throw new Error('Failed to create worktree')
      } else {
        workspaceInfo = await s._resolveWorkspace(sourceDir).catch(() => null)
      }

      if (!workspaceInfo) {
        workspaceInfo = {
          repoRoot: params.repoRoot || sourceDir,
          projectDir: sourceDir,
          branch: '',
          workspaceType: 'branch',
        }
      }

      const targetDir = workspaceInfo.projectDir
      const { sessionId: newId } = await droid.createSession({
        cwd: targetDir,
        modelId: inheritModel,
        autoLevel: inheritAutoLevel,
        reasoningEffort: inheritReasoningEffort || undefined,
      })

      const initialTitle = defaultSessionTitleFromBranch(workspaceInfo.branch)
      const now = Date.now()
      get()._setProjects((prev) =>
        upsertSessionMeta(prev, {
          id: newId,
          projectDir: targetDir,
          repoRoot: workspaceInfo!.repoRoot,
          branch: workspaceInfo!.branch,
          workspaceType: workspaceInfo!.workspaceType,
          title: initialTitle,
          savedAt: now,
          messageCount: 0,
          model: inheritModel,
          autoLevel: inheritAutoLevel,
          reasoningEffort: inheritReasoningEffort || undefined,
          baseBranch: workspaceInfo!.baseBranch,
        }),
      )
      const initialBuffer = {
        ...makeBuffer(targetDir, {
          repoRoot: workspaceInfo!.repoRoot,
          branch: workspaceInfo!.branch,
          workspaceType: workspaceInfo!.workspaceType,
          baseBranch: workspaceInfo!.baseBranch,
        }),
        model: inheritModel,
        autoLevel: inheritAutoLevel,
        reasoningEffort: inheritReasoningEffort || '',
      }
      get()._setSessionBuffers((prev) => {
        const next = new Map(prev)
        next.set(newId, initialBuffer)
        return next
      })

      set({ activeProjectDir: targetDir, activeSessionId: newId })
      droid.setProjectDir(targetDir)
      void get()._saveSessionToDisk(newId, initialBuffer)

      const repoKey = String(workspaceInfo.repoRoot || '').trim()
      const setupScript = repoKey
        ? String(get().projectSettingsByRepo[repoKey]?.setupScript || '').trim()
        : ''
      if (setupScript) {
        void get()._runSetupScriptForSession({
          sessionId: newId,
          projectDir: targetDir,
          script: setupScript,
        })
      }

      return newId
    } finally {
      set({ isCreatingSession: false })
    }
  },

  handleSwitchWorkspaceForSession: async (params) => {
    const s = get()
    const sid = params.sessionId || s.activeSessionId
    const branch = String(params.branch || '').trim()
    if (!sid || !branch) return false

    const buf = s.sessionBuffers.get(sid)
    const meta = s.projects.flatMap((p) => p.sessions).find((x) => x.id === sid)
    const projectDir = buf?.projectDir || meta?.projectDir || s.activeProjectDir
    if (!projectDir) return false

    let info: WorkspaceInfo | null = null
    try {
      info = await droid.switchWorkspace({ projectDir, branch })
    } catch {
      return false
    }
    if (!info) return false

    get()._setSessionBuffers((prev) => {
      const cur = prev.get(sid)
      if (!cur) return prev
      const next = new Map(prev)
      next.set(sid, {
        ...cur,
        projectDir: info!.projectDir,
        repoRoot: info!.repoRoot,
        branch: info!.branch,
        workspaceType: info!.workspaceType,
      })
      return next
    })

    const now = Date.now()
    const updatedMeta: SessionMeta = {
      id: sid,
      projectDir: info.projectDir,
      repoRoot: info.repoRoot,
      branch: info.branch,
      workspaceType: info.workspaceType,
      baseBranch: meta?.baseBranch || buf?.baseBranch,
      title: meta?.title || 'Untitled',
      savedAt: meta?.savedAt || now,
      messageCount: meta?.messageCount || 0,
      model: meta?.model || (buf?.model ?? DEFAULT_MODEL),
      autoLevel: meta?.autoLevel || (buf?.autoLevel ?? DEFAULT_AUTO_LEVEL),
      reasoningEffort: meta?.reasoningEffort || buf?.reasoningEffort || undefined,
      lastMessageAt: meta?.lastMessageAt,
    }
    get()._setProjects((prev) => upsertSessionMeta(prev, updatedMeta))

    if ((params.sessionId || s.activeSessionId) === s.activeSessionId) {
      set({ activeProjectDir: info.projectDir })
      droid.setProjectDir(info.projectDir)
    }
    return true
  },

  // --- New-session helpers ---
  _maybeFlushPendingInitialSend: () => {
    const s = get()
    const pending = s.pendingInitialSend
    if (!pending?.sessionId) return
    if (pending.sessionId !== s.activeSessionId) return

    const buf = s.sessionBuffers.get(pending.sessionId)
    if (!buf) return
    if (buf.isSetupRunning || buf.setupScript.status === 'failed') return

    set({ pendingInitialSend: null })
    // Send via the normal pipeline (now that setup is not blocking).
    s.handleSend(pending.input, pending.attachments)
  },

  _confirmPendingNewSessionAndSend: async ({ input, attachments }) => {
    const s = get()
    const pending = s.pendingNewSession
    if (!pending) return
    if (!s._initialLoadDone) return
    if (s.isCreatingSession) return

    s.clearWorkspaceError()

    const repoRoot = String(pending.repoRoot || '').trim()
    if (!repoRoot) {
      set({ workspaceError: 'Missing repo root' })
      return
    }

    const settings = s.projectSettingsByRepo[repoRoot] || {}
    const prefix = sanitizeWorktreePrefix(settings.worktreePrefix || '') || 'droi'

    const baseBranchFromSettings =
      typeof settings.baseBranch === 'string' ? settings.baseBranch.trim() : ''

    const queuedAttachments = attachments ?? []

    let branch = String(pending.branch || '').trim()
    let mode: 'new-worktree' | 'switch-branch'

    if (pending.isExistingBranch && branch) {
      mode = 'switch-branch'
    } else {
      mode = 'new-worktree'
      if (!branch) {
        branch = generateWorktreeBranch(prefix)
      }
    }

    const baseBranch = String(baseBranchFromSettings || '').trim()
    if (mode === 'new-worktree' && !baseBranch) {
      set({ workspaceError: 'Missing base branch. Configure it in Project Settings first.' })
      return
    }

    // Create the session/workspace first.
    try {
      const newId = await s.handleCreateSessionWithWorkspace({
        repoRoot,
        projectDir: repoRoot,
        mode,
        branch,
        ...(mode === 'new-worktree' ? { baseBranch } : {}),
      })
      if (!newId) return

      set({ pendingNewSession: null })

      const buf = get().sessionBuffers.get(newId)
      const repoKey = String(buf?.repoRoot || repoRoot).trim()
      const setupScript = repoKey
        ? String(get().projectSettingsByRepo[repoKey]?.setupScript || '').trim()
        : ''

      if (setupScript) {
        set({ pendingInitialSend: { sessionId: newId, input, attachments: queuedAttachments } })
        // In case setup did not start (or is very fast), attempt an immediate flush.
        s._maybeFlushPendingInitialSend()
        return
      }

      // No setup script: send immediately as the first message.
      s.handleSend(input, queuedAttachments)
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      set({ workspaceError: msg || 'Failed to create session' })
    }
  },

  // --- Send / Cancel ---
  handleClearSessionContext: async (sessionId?: string) => {
    const s = get()
    const sid = sessionId || s.activeSessionId
    if (!sid) return

    const buf = s.sessionBuffers.get(sid)
    if (!buf) return
    if (buf.isRunning || buf.isSetupRunning) {
      get()._setSessionBuffers((prev) =>
        appendDebugTrace(prev, sid, 'ui-clear: session is running; cancel first'),
      )
      return
    }

    const meta = await droid.clearSession({ id: sid })
    if (!meta) return

    const now = Date.now()
    const normalizedMeta: SessionMeta = {
      ...meta,
      savedAt: meta.savedAt || now,
      lastMessageAt: undefined,
      messageCount: 0,
    }

    if (meta.id && meta.id !== sid) {
      get()._setProjects((prev) => replaceSessionIdInProjects(prev, sid, normalizedMeta))
      get()._setSessionBuffers((prev) => {
        const session = prev.get(sid)
        if (!session) return prev
        const next = new Map(prev)
        next.delete(sid)
        next.set(meta.id, {
          ...session,
          isRunning: false,
          isCancelling: false,
          pendingSendMessageIds: {},
          pendingPermissionRequests: [],
          pendingAskUserRequests: [],
          messages: [],
          debugTrace: [],
        })
        return next
      })
      set((prev) => ({
        activeSessionId: prev.activeSessionId === sid ? meta.id : prev.activeSessionId,
      }))
      set((prev) => {
        const gens = prev._sessionGenerations
        if (!gens.has(sid)) return prev
        const next = new Map(gens)
        next.set(meta.id, next.get(sid) || 0)
        next.delete(sid)
        return { _sessionGenerations: next }
      })
      return
    }

    get()._setProjects((prev) => upsertSessionMeta(prev, normalizedMeta))
    get()._setSessionBuffers((prev) => {
      const session = prev.get(sid)
      if (!session) return prev
      const next = new Map(prev)
      next.set(sid, {
        ...session,
        isRunning: false,
        isCancelling: false,
        pendingSendMessageIds: {},
        pendingPermissionRequests: [],
        pendingAskUserRequests: [],
        messages: [],
        debugTrace: [],
      })
      return next
    })
  },
  handleSend: (input, attachments) => {
    const s = get()

    if (s.pendingNewSession) {
      void s._confirmPendingNewSessionAndSend({ input, attachments })
      return
    }

    const sid = s.activeSessionId
    const projDir = s.activeProjectDir
    const normalized = typeof input === 'string' ? { text: input } : input || { text: '' }
    const text = String(normalized.text || '')
    const trimmedText = text.trim()
    const tagType =
      normalized.tag?.type === 'command' || normalized.tag?.type === 'skill'
        ? normalized.tag.type
        : null
    const tagName =
      typeof normalized.tag?.name === 'string' && normalized.tag.name.trim()
        ? normalized.tag.name.trim()
        : ''
    const hasTag = Boolean(tagType && tagName)

    const isCommandTag = hasTag && tagType === 'command'
    const isSkillTag = hasTag && tagType === 'skill'

    const hasUserText = Boolean(text.trim())
    const queuedAttachments = attachments ?? []
    const hasAttachments = queuedAttachments.length > 0
    if ((!hasUserText && !hasTag && !hasAttachments) || !projDir || !sid) return

    if (
      !hasTag &&
      !hasAttachments &&
      (trimmedText === '/clear' || trimmedText === '/reset' || trimmedText === '/restart')
    ) {
      if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
        ;(droid as any).appendDiagnosticsEvent({
          sessionId: sid,
          event: 'ui.session.clear_requested',
          level: 'info',
          data: { command: trimmedText },
        })
      }
      void get().handleClearSessionContext(sid)
      return
    }

    const rawPromptForTitle = hasTag ? `/${tagName}${text.trim() ? ` ${text.trim()}` : ''}` : text

    const buf = s.sessionBuffers.get(sid)
    if (buf?.isSetupRunning || buf?.setupScript.status === 'failed') return
    const sessionModel = buf?.model ?? DEFAULT_MODEL
    const sessionAutoLevel = buf?.autoLevel ?? DEFAULT_AUTO_LEVEL
    const sessionReasoningEffort = buf?.reasoningEffort ?? ''

    const existingMeta = s.projects.flatMap((p) => p.sessions).find((x) => x.id === sid)

    const now = Date.now()
    const userMessage: ChatMessage = {
      id: uuidv4(),
      role: 'user' as const,
      blocks: [
        ...(isCommandTag
          ? [
              { kind: 'command' as const, name: tagName },
              { kind: 'text' as const, content: text },
            ]
          : isSkillTag
            ? [
                { kind: 'skill' as const, name: tagName },
                { kind: 'text' as const, content: text },
              ]
            : [{ kind: 'text' as const, content: text }]),
        ...queuedAttachments.map((a) => ({
          kind: 'attachment' as const,
          name: a.name,
          path: a.path,
        })),
      ],
      timestamp: now,
    }

    const nextMessages = [...(buf?.messages ?? []), userMessage]
    const messageCount = nextMessages.length
    const nextTitle =
      !existingMeta || existingMeta.title === 'Untitled' || existingMeta.messageCount === 0
        ? getTitleFromPrompt(rawPromptForTitle)
        : existingMeta.title
    const draftMeta: SessionMeta = {
      id: sid,
      projectDir: projDir,
      repoRoot: buf?.repoRoot || projDir,
      branch: buf?.branch,
      workspaceType: buf?.workspaceType,
      baseBranch: buf?.baseBranch,
      title: nextTitle,
      savedAt: now,
      messageCount,
      model: sessionModel,
      autoLevel: sessionAutoLevel,
      reasoningEffort: sessionReasoningEffort || undefined,
      apiKeyFingerprint: buf?.apiKeyFingerprint || undefined,
    }

    get()._setProjects((prev) => upsertSessionMeta(prev, draftMeta))

    void droid
      .saveSession({
        id: sid,
        projectDir: projDir,
        repoRoot: buf?.repoRoot || projDir,
        branch: buf?.branch,
        workspaceType: buf?.workspaceType,
        baseBranch: buf?.baseBranch,
        model: sessionModel,
        autoLevel: sessionAutoLevel,
        reasoningEffort: sessionReasoningEffort || undefined,
        apiKeyFingerprint: buf?.apiKeyFingerprint || undefined,
        pinned: existingMeta?.pinned || undefined,
        messages: nextMessages,
      })
      .then((meta) => {
        if (!meta) return
        const normalized: SessionMeta = {
          ...meta,
          projectDir: meta.projectDir || projDir,
          repoRoot: meta.repoRoot || buf?.repoRoot || projDir,
          branch: meta.branch || buf?.branch,
          workspaceType: meta.workspaceType || buf?.workspaceType,
          baseBranch: meta.baseBranch || buf?.baseBranch,
        }
        get()._setProjects((prev) => upsertSessionMeta(prev, normalized))
      })

    const missingHooks = getMissingDroidHooks(droid)
    if (missingHooks.length > 0) {
      set({ _hookMismatchReported: true })
      const mismatchMessage = buildHookMismatchMessage(missingHooks)
      get()._setSessionBuffers((prev) => {
        const session = prev.get(sid) || makeBuffer(projDir)
        let next = new Map(prev)
        next.set(sid, {
          ...session,
          isRunning: false,
          messages: [...session.messages, userMessage],
        })
        next = appendDebugTrace(
          next,
          sid,
          `ui-send: model=${sessionModel} auto=${sessionAutoLevel} chars=${rawPromptForTitle.length}`,
        )
        next = appendDebugTrace(
          next,
          sid,
          `ui-send-blocked: missing-hooks=${missingHooks.join(',')}`,
        )
        next = applyError(next, sid, mismatchMessage)
        return next
      })
      return
    }

    const generation = s._getSessionGeneration(sid)
    const injectingAtDispatch = Boolean(buf?.isRunning)

    get()._setSessionBuffers((prev) => {
      const session = prev.get(sid) || makeBuffer(projDir)
      const injecting = Boolean(session.isRunning)
      const pendingSendMessageIds: Record<string, true> = {
        ...session.pendingSendMessageIds,
        [userMessage.id]: true,
      }
      let next = new Map(prev)
      next.set(sid, {
        ...session,
        isRunning: true,
        messages: [...session.messages, userMessage],
        pendingSendMessageIds,
      })
      next = appendDebugTrace(
        next,
        sid,
        `ui-send: model=${sessionModel} auto=${sessionAutoLevel} chars=${rawPromptForTitle.length}`,
      )
      if (injecting) next = appendDebugTrace(next, sid, `ui-inject: messageId=${userMessage.id}`)
      return next
    })

    if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
      const basePromptKind = isCommandTag ? 'command' : isSkillTag ? 'skill' : 'plain'
      ;(droid as any).appendDiagnosticsEvent({
        sessionId: sid,
        event: 'ui.send.start',
        level: 'info',
        correlation: { uiMessageId: userMessage.id },
        data: {
          isRunningAtDispatch: injectingAtDispatch,
          queuedAttachmentsCount: queuedAttachments.length,
          basePromptKind,
          userTextLen: text.length,
        },
      })
      if (injectingAtDispatch) {
        ;(droid as any).appendDiagnosticsEvent({
          sessionId: sid,
          event: 'ui.inject',
          level: 'info',
          correlation: { uiMessageId: userMessage.id },
          data: { reason: 'session_already_running' },
        })
      }
    }

    void (async () => {
      let basePrompt = text

      if (isSkillTag) {
        const args = text.trim()
        basePrompt = `Use skill "${tagName}".${args ? `\n\n${args}` : ''}`
        get()._setSessionBuffers((prev) =>
          appendDebugTrace(prev, sid, `ui-skill-send: name=${tagName} argsLen=${args.length}`),
        )
      }

      if (isCommandTag) {
        basePrompt = rawPromptForTitle
        try {
          const res = await droid.resolveSlashCommand({ text: basePrompt })
          if (!get()._isSessionGenerationCurrent(sid, generation)) return
          const nextPrompt =
            res.matched || res.expandedText !== basePrompt
              ? String(res.expandedText || basePrompt)
              : basePrompt
          basePrompt = nextPrompt
          const cmdInfo = res.command
            ? ` name=${res.command.name} scope=${res.command.scope} file=${res.command.filePath}`
            : ''
          const errInfo = res.error ? ` error=${res.error}` : ''
          get()._setSessionBuffers((prev) =>
            appendDebugTrace(
              prev,
              sid,
              `ui-slash: matched=${String(res.matched)}${cmdInfo}${errInfo}`,
            ),
          )
          if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
            ;(droid as any).appendDiagnosticsEvent({
              sessionId: sid,
              event: 'ui.send.resolved_slash',
              level: 'debug',
              correlation: { uiMessageId: userMessage.id },
              data: {
                matched: Boolean(res.matched),
                expandedLen: String(res.expandedText || '').length,
                hasError: Boolean(res.error),
              },
            })
          }
        } catch (err) {
          if (!get()._isSessionGenerationCurrent(sid, generation)) return
          const msg = err instanceof Error ? err.message : String(err)
          get()._setSessionBuffers((prev) => appendDebugTrace(prev, sid, `ui-slash: error=${msg}`))
          if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
            ;(droid as any).appendDiagnosticsEvent({
              sessionId: sid,
              event: 'ui.send.slash_error',
              level: 'warn',
              correlation: { uiMessageId: userMessage.id },
              data: { error: msg },
            })
          }
        }
      }

      if (!get()._isSessionGenerationCurrent(sid, generation)) return

      const attachmentSuffix =
        queuedAttachments.length > 0
          ? `\n\nAttached files:\n${queuedAttachments.map((a) => `- ${a.path}`).join('\n')}`
          : ''
      const finalPrompt = `${basePrompt}${attachmentSuffix}`

      try {
        let activeKeyFp = ''
        try {
          const info = await droid.getActiveKeyInfo()
          activeKeyFp = String((info as any)?.apiKeyFingerprint || '')
        } catch {
          // ignore
        }

        if (!get()._isSessionGenerationCurrent(sid, generation)) return

        if (activeKeyFp) {
          if (injectingAtDispatch) {
            get()._setSessionBuffers((prev) => {
              const session = prev.get(sid)
              if (!session) return prev
              const next = new Map(prev)
              next.set(sid, { ...session, pendingApiKeyFingerprint: activeKeyFp })
              return appendDebugTrace(
                next,
                sid,
                `api-key-rotation-deferred: ${session.apiKeyFingerprint || '(unknown)'} -> ${activeKeyFp}`,
              )
            })
          } else {
            const before = get().sessionBuffers.get(sid)
            const prevFp = before?.apiKeyFingerprint
            if (prevFp !== activeKeyFp) {
              get()._setSessionBuffers((prev) =>
                appendDebugTrace(
                  prev,
                  sid,
                  `api-key-rotation: ${prevFp || '(unknown)'} -> ${activeKeyFp}`,
                ),
              )
              try {
                await droid.restartSessionWithActiveKey({ sessionId: sid })
                get()._setSessionBuffers((prev) => {
                  const session = prev.get(sid)
                  if (!session) return prev
                  const next = new Map(prev)
                  next.set(sid, {
                    ...session,
                    apiKeyFingerprint: activeKeyFp,
                    pendingApiKeyFingerprint: undefined,
                  })
                  return appendDebugTrace(next, sid, `api-key-restarted: fp=${activeKeyFp}`)
                })
              } catch (err) {
                const msg = err instanceof Error ? err.message : String(err)
                get()._setSessionBuffers((prev) =>
                  appendDebugTrace(prev, sid, `api-key-restart-failed: ${msg}`),
                )
              }
            } else {
              get()._setSessionBuffers((prev) => {
                const session = prev.get(sid)
                if (!session || session.apiKeyFingerprint === activeKeyFp) return prev
                const next = new Map(prev)
                next.set(sid, { ...session, apiKeyFingerprint: activeKeyFp })
                return next
              })
            }
          }
        }

        if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
          ;(droid as any).appendDiagnosticsEvent({
            sessionId: sid,
            event: 'ui.exec.dispatched',
            level: 'info',
            correlation: { uiMessageId: userMessage.id },
            data: {
              finalPromptLen: finalPrompt.length,
              queuedAttachmentsCount: queuedAttachments.length,
            },
          })
        }
        await droid.exec({
          prompt: finalPrompt,
          sessionId: sid,
          modelId: sessionModel,
          autoLevel: sessionAutoLevel,
          reasoningEffort: sessionReasoningEffort || undefined,
        })

        get()._setSessionBuffers((prev) => {
          const session = prev.get(sid)
          if (!session || !session.pendingSendMessageIds[userMessage.id]) return prev
          const nextPending = { ...session.pendingSendMessageIds }
          delete nextPending[userMessage.id]
          const next = new Map(prev)
          next.set(sid, { ...session, pendingSendMessageIds: nextPending })
          return next
        })
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
          ;(droid as any).appendDiagnosticsEvent({
            sessionId: sid,
            event: 'ui.exec.failed',
            level: 'error',
            correlation: { uiMessageId: userMessage.id },
            data: { error: msg },
          })
        }
        get()._setSessionBuffers((prev) => {
          let next = appendDebugTrace(prev, sid, `ui-exec-throw: ${msg}`)

          if (injectingAtDispatch) {
            const session = next.get(sid)
            if (!session) return next
            const errorMessage: ChatMessage = {
              id: uuidv4(),
              role: 'error' as const,
              blocks: [{ kind: 'text' as const, content: `Failed to dispatch exec: ${msg}` }],
              timestamp: Date.now(),
            }
            const final = new Map(next)
            final.set(sid, { ...session, messages: [...session.messages, errorMessage] })
            return final
          }

          next = applyError(next, sid, `Failed to dispatch exec: ${msg}`)
          next = applyTurnEnd(next, sid)
          return next
        })
      }
    })()
  },

  handleCancel: () => {
    const s = get()
    const sid = s.activeSessionId || null
    if (!sid) return
    const buf = s.sessionBuffers.get(sid)
    if (!buf?.isRunning && !buf?.isSetupRunning) return
    s._bumpSessionGeneration(sid)
    if (buf?.isRunning) {
      get()._setSessionBuffers((prev) => {
        const session = prev.get(sid)
        if (!session) return prev
        const next = new Map(prev)
        next.set(sid, { ...session, isCancelling: true })
        return appendDebugTrace(next, sid, 'ui-cancel')
      })
      droid.cancel({ sessionId: sid })
    }
    if (buf?.isSetupRunning) {
      get()._setSessionBuffers((prev) => appendDebugTrace(prev, sid, 'ui-setup-cancel'))
      droid.cancelSetupScript({ sessionId: sid })
    }
  },

  handleForceCancel: () => {
    const s = get()
    const sid = s.activeSessionId || null
    if (!sid) return
    const buf = s.sessionBuffers.get(sid)
    if (!buf?.isCancelling) return
    s._bumpSessionGeneration(sid)
    get()._setSessionBuffers((prev) =>
      applyTurnEnd(appendDebugTrace(prev, sid, 'ui-force-cancel'), sid),
    )
    droid.cancel({ sessionId: sid })
  },

  handleRespondPermission: (params) => {
    const s = get()
    const sid = s.activeSessionId
    const buf = s.sessionBuffers.get(sid)
    const req = buf?.pendingPermissionRequests?.[0]
    if (!sid || !req) return

    const selectedOption = params.selectedOption
    const autoLevelMap: Partial<Record<DroidPermissionOption, string>> = {
      proceed_auto_run_low: 'low',
      proceed_auto_run_medium: 'medium',
      proceed_auto_run_high: 'high',
      proceed_auto_run: 'high',
    }
    const newAutoLevel = params.autoLevel || autoLevelMap[selectedOption]

    get()._setSessionBuffers((prev) =>
      appendDebugTrace(
        prev,
        sid,
        `ui-permission-response: ${selectedOption} requestId=${req.requestId}`,
      ),
    )
    droid.respondPermission({ sessionId: sid, requestId: req.requestId, selectedOption })

    get()._setSessionBuffers((prev) => {
      const session = prev.get(sid)
      if (!session) return prev
      const rest = (session.pendingPermissionRequests || []).filter(
        (r) => r.requestId !== req.requestId,
      )
      const next = new Map(prev)
      next.set(sid, {
        ...session,
        pendingPermissionRequests: rest,
        ...(newAutoLevel ? { autoLevel: newAutoLevel } : {}),
      })
      return next
    })

    if (
      newAutoLevel &&
      (newAutoLevel === 'low' || newAutoLevel === 'medium' || newAutoLevel === 'high')
    ) {
      void (async () => {
        try {
          await droid.updateSessionSettings({ sessionId: sid, autoLevel: newAutoLevel })
          get()._setSessionBuffers((prev) =>
            appendDebugTrace(prev, sid, `ui-session-settings-update: auto=${newAutoLevel}`),
          )
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err)
          get()._setSessionBuffers((prev) =>
            appendDebugTrace(prev, sid, `ui-session-settings-update-failed: ${msg}`),
          )
        }
      })()
    }
  },

  handleRespondAskUser: (params) => {
    const s = get()
    const sid = s.activeSessionId
    const buf = s.sessionBuffers.get(sid)
    const req = buf?.pendingAskUserRequests?.[0]
    if (!sid || !req) return
    get()._setSessionBuffers((prev) =>
      appendDebugTrace(
        prev,
        sid,
        `ui-askuser-response: cancelled=${Boolean(params.cancelled)} requestId=${req.requestId}`,
      ),
    )
    droid.respondAskUser({
      sessionId: sid,
      requestId: req.requestId,
      cancelled: params.cancelled,
      answers: params.answers,
    })
    get()._setSessionBuffers((prev) => {
      const session = prev.get(sid)
      if (!session) return prev
      const rest = (session.pendingAskUserRequests || []).filter(
        (r) => r.requestId !== req.requestId,
      )
      const next = new Map(prev)
      next.set(sid, { ...session, pendingAskUserRequests: rest })
      return next
    })
  },

  // --- Workspace for meta ---
  _ensureWorkspaceForMeta: async (meta) => {
    const desiredDir = String(meta.projectDir || '').trim()
    if (!desiredDir) throw new Error('Session is missing project directory')

    let info: WorkspaceInfo | null = null
    try {
      info = await get()._resolveWorkspace(desiredDir)
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(`${msg || 'Failed to resolve git workspace'} (dir: ${desiredDir})`, {
        cause: err,
      })
    }
    if (!info) throw new Error(`Failed to resolve git workspace (dir: ${desiredDir})`)

    const desiredBranch = typeof meta.branch === 'string' ? meta.branch.trim() : ''
    if (desiredBranch && desiredBranch !== info.branch) {
      const switched = await droid.switchWorkspace({
        projectDir: info.projectDir || desiredDir,
        branch: desiredBranch,
      })
      if (!switched) throw new Error(`Failed to switch to branch ${desiredBranch}`)
      return switched
    }

    return info
  },

  _switchToSessionWithAligned: async (selectedMeta, aligned) => {
    const sessionId = selectedMeta.id
    const normalizedMeta: SessionMeta = {
      ...selectedMeta,
      projectDir: aligned.projectDir,
      repoRoot: aligned.repoRoot,
      branch: aligned.branch,
      workspaceType: aligned.workspaceType,
      baseBranch: selectedMeta.baseBranch || aligned.baseBranch,
    }
    get()._setProjects((prev) => upsertSessionMeta(prev, normalizedMeta))

    get()._setSessionBuffers((prev) => {
      const existing = prev.get(sessionId)
      if (!existing) return prev
      const next = new Map(prev)
      next.set(sessionId, {
        ...existing,
        projectDir: aligned.projectDir,
        repoRoot: aligned.repoRoot,
        branch: aligned.branch,
        workspaceType: aligned.workspaceType,
      })
      return next
    })

    set({ activeProjectDir: aligned.projectDir, activeSessionId: sessionId })
    droid.setProjectDir(aligned.projectDir)

    const existingBuffer = get().sessionBuffers.get(sessionId)
    if (!existingBuffer) {
      const data = await droid.loadSession(sessionId)
      const loaded = (data?.messages as ChatMessage[]) ?? []
      get()._setSessionBuffers((prev) => {
        const next = new Map(prev)
        const base = makeBuffer(aligned.projectDir, {
          repoRoot: aligned.repoRoot,
          branch: aligned.branch,
          workspaceType: aligned.workspaceType,
          baseBranch: (data as any)?.baseBranch || selectedMeta.baseBranch || aligned.baseBranch,
        })
        next.set(sessionId, {
          ...base,
          messages: loaded,
          model: data?.model || selectedMeta.model || DEFAULT_MODEL,
          autoLevel: data?.autoLevel || selectedMeta.autoLevel || DEFAULT_AUTO_LEVEL,
          reasoningEffort: (data as any)?.reasoningEffort || selectedMeta.reasoningEffort || '',
          apiKeyFingerprint: (data as any)?.apiKeyFingerprint || selectedMeta.apiKeyFingerprint,
        })
        return next
      })
    }
  },

  // --- Navigation ---
  handleNewSession: (repoRoot) => {
    void (async () => {
      const s = get()
      if (!s._initialLoadDone) return
      s.clearWorkspaceError()
      try {
        const sourceDir = s._pickProjectDirForRepo(repoRoot)
        if (!sourceDir) throw new Error('No project directory available')

        const resolved = await s._resolveWorkspace(sourceDir).catch(() => null)
        if (!resolved) throw new Error('Not a git repository')
        const commonRepoRoot = resolved.repoRoot || repoRoot || sourceDir
        if (!commonRepoRoot) throw new Error('Missing repo root')

        await s._ensureProjectSettingsInitialized({
          repoRoot: commonRepoRoot,
          hintBranch: resolved.branch,
        })

        set({
          pendingNewSession: {
            repoRoot: commonRepoRoot,
            branch: '',
            isExistingBranch: false,
          },
        })
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        set({ workspaceError: msg || 'Failed to create session' })
      }
    })()
  },

  handleSetProjectDir: (dir) => {
    if (!dir) return
    if (!get()._initialLoadDone) return
    void (async () => {
      const s = get()
      const info = await s._resolveWorkspace(dir).catch(() => null)
      if (!info) {
        set({ workspaceError: 'Not a git repository' })
        return
      }
      const repoRoot = info.repoRoot

      get()._setProjects((prev) => {
        if (prev.find((p) => p.dir === repoRoot)) return prev
        const name = repoRoot.split(/[\\/]/).pop() || repoRoot
        return [...prev, { dir: repoRoot, name, sessions: [] }]
      })

      await s._ensureProjectSettingsInitialized({ repoRoot, hintBranch: info.branch })

      get().handleNewSession(repoRoot)
    })()
  },

  handleAddProject: async () => {
    if (!get()._initialLoadDone) return
    const dir = await droid.openDirectory()
    if (!dir) return
    get().handleSetProjectDir(dir)
  },

  handleSelectSession: async (sessionId) => {
    const s = get()
    const selectedMeta = s.projects.flatMap((p) => p.sessions).find((x) => x.id === sessionId)
    if (!selectedMeta) return

    set({ pendingNewSession: null })

    const currentSid = s.activeSessionId
    const currentBuf = s.sessionBuffers.get(currentSid)
    if (currentBuf && currentBuf.messages.length > 0 && !currentBuf.isRunning) {
      void s._saveSessionToDisk(currentSid, currentBuf)
    }

    s.clearWorkspaceError()

    try {
      const aligned = await s._ensureWorkspaceForMeta(selectedMeta)
      await get()._switchToSessionWithAligned(selectedMeta, aligned)
      s._maybeFlushPendingInitialSend()
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      set({ workspaceError: msg || 'Failed to switch to session workspace' })
    }
  },

  handleTogglePin: (sessionId) => {
    if (!sessionId) return
    const s = get()
    const meta = s.projects.flatMap((p) => p.sessions).find((x) => x.id === sessionId)
    if (!meta) return
    const nextPinned = !meta.pinned
    get()._setProjects((prev) =>
      prev.map((p) => ({
        ...p,
        sessions: p.sessions.map((sess) =>
          sess.id === sessionId ? { ...sess, pinned: nextPinned } : sess,
        ),
      })),
    )
    const buf = s.sessionBuffers.get(sessionId)
    if (buf) {
      void droid.saveSession({
        id: sessionId,
        projectDir: buf.projectDir,
        repoRoot: buf.repoRoot,
        branch: buf.branch,
        workspaceType: buf.workspaceType,
        baseBranch: buf.baseBranch,
        model: buf.model || DEFAULT_MODEL,
        autoLevel: buf.autoLevel || DEFAULT_AUTO_LEVEL,
        reasoningEffort: buf.reasoningEffort || undefined,
        apiKeyFingerprint: buf.apiKeyFingerprint || undefined,
        pinned: nextPinned,
        messages: buf.messages,
      })
    }
  },

  handleDeleteSession: async (sessionId) => {
    if (!sessionId) return
    const s = get()
    if (s.deletingSessionIds.has(sessionId)) return

    set((prev) => {
      if (prev.deletingSessionIds.has(sessionId)) return prev
      const next = new Set(prev.deletingSessionIds)
      next.add(sessionId)
      return { deletingSessionIds: next }
    })

    try {
      const sessionMeta = get()
        .projects.flatMap((p) => p.sessions)
        .find((x) => x.id === sessionId)
      if (!sessionMeta) return

      get()._bumpSessionGeneration(sessionId)
      const buffer = get().sessionBuffers.get(sessionId)
      if (buffer?.isRunning) droid.cancel({ sessionId })
      if (buffer?.isSetupRunning) droid.cancelSetupScript({ sessionId })

      if (sessionMeta.workspaceType === 'worktree') {
        const repoRoot = String(sessionMeta.repoRoot || '').trim()
        const worktreeDir = String(sessionMeta.projectDir || '').trim()
        if (!repoRoot || !worktreeDir) throw new Error('Session is missing repoRoot/worktreeDir')
        await droid.removeWorktree({ repoRoot, worktreeDir, force: true })
      }

      await droid.deleteSession(sessionId)
      get()._clearSessionGeneration(sessionId)

      const wasActive = get().activeSessionId === sessionId
      const deletedRepoRoot = String(sessionMeta.repoRoot || '').trim()

      const allRemaining = get()
        .projects.flatMap((p) => p.sessions)
        .filter((x) => x.id !== sessionId)
        .sort((a, b) => (b.lastMessageAt ?? b.savedAt) - (a.lastMessageAt ?? a.savedAt))
      const sameProjectFallback = deletedRepoRoot
        ? allRemaining.filter((x) => (x.repoRoot || x.projectDir) === deletedRepoRoot)
        : []
      const pickFallback = sameProjectFallback[0] || allRemaining[0] || null

      get()._setProjects((prev) =>
        prev.map((p) => ({ ...p, sessions: p.sessions.filter((x) => x.id !== sessionId) })),
      )
      get()._setSessionBuffers((prev) => {
        const next = new Map(prev)
        next.delete(sessionId)
        return next
      })

      if (!wasActive) return

      if (pickFallback) {
        get().clearWorkspaceError()
        try {
          const aligned = await get()._ensureWorkspaceForMeta(pickFallback)
          const normalizedMeta: SessionMeta = {
            ...pickFallback,
            projectDir: aligned.projectDir,
            repoRoot: aligned.repoRoot,
            branch: aligned.branch,
            workspaceType: aligned.workspaceType,
            baseBranch: pickFallback.baseBranch || aligned.baseBranch,
          }
          get()._setProjects((prev) => upsertSessionMeta(prev, normalizedMeta))

          set({ activeProjectDir: aligned.projectDir, activeSessionId: pickFallback.id })
          droid.setProjectDir(aligned.projectDir)

          const existingBuffer = get().sessionBuffers.get(pickFallback.id)
          if (!existingBuffer) {
            const data = await droid.loadSession(pickFallback.id)
            const loaded = (data?.messages as ChatMessage[]) ?? []
            get()._setSessionBuffers((prev) => {
              const next = new Map(prev)
              const base = makeBuffer(aligned.projectDir, {
                repoRoot: aligned.repoRoot,
                branch: aligned.branch,
                workspaceType: aligned.workspaceType,
                baseBranch:
                  (data as any)?.baseBranch || pickFallback.baseBranch || aligned.baseBranch,
              })
              next.set(pickFallback.id, {
                ...base,
                messages: loaded,
                model: data?.model || pickFallback.model || DEFAULT_MODEL,
                autoLevel: data?.autoLevel || pickFallback.autoLevel || DEFAULT_AUTO_LEVEL,
                reasoningEffort:
                  (data as any)?.reasoningEffort || pickFallback.reasoningEffort || '',
                apiKeyFingerprint:
                  (data as any)?.apiKeyFingerprint || pickFallback.apiKeyFingerprint,
              })
              return next
            })
          }
          return
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err)
          set({ workspaceError: msg || 'Failed to switch to fallback session' })
        }
      }

      set({ activeProjectDir: '', activeSessionId: '' })
      droid.setProjectDir(null)
      const newId = uuidv4()
      get()._setSessionBuffers((prev) => {
        const next = new Map(prev)
        next.set(newId, makeBuffer(''))
        return next
      })
      set({ activeSessionId: newId })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      set({ workspaceError: msg || 'Failed to delete session' })
    } finally {
      set((prev) => {
        if (!prev.deletingSessionIds.has(sessionId)) return prev
        const next = new Set(prev.deletingSessionIds)
        next.delete(sessionId)
        return { deletingSessionIds: next }
      })
    }
  },

  handleDeleteProject: (repoRoot) => {
    if (!repoRoot) return

    set((prev) =>
      prev.pendingNewSession?.repoRoot === repoRoot ? { pendingNewSession: null } : {},
    )

    void (async () => {
      const targetProject = get().projects.find((p) => p.dir === repoRoot)
      const targetSessions = targetProject?.sessions || []
      const targetSessionIds = targetSessions.map((x) => x.id)

      for (const sid of targetSessionIds) {
        get()._bumpSessionGeneration(sid)
        const buffer = get().sessionBuffers.get(sid)
        if (buffer?.isRunning) droid.cancel({ sessionId: sid })
        if (buffer?.isSetupRunning) droid.cancelSetupScript({ sessionId: sid })
      }

      for (const session of targetSessions) {
        if (session.workspaceType === 'worktree') {
          const wtRepoRoot = String(session.repoRoot || '').trim()
          const wtProjectDir = String(session.projectDir || '').trim()
          if (wtRepoRoot && wtProjectDir) {
            try {
              await droid.removeWorktree({
                repoRoot: wtRepoRoot,
                worktreeDir: wtProjectDir,
                force: true,
              })
            } catch {
              // ignore
            }
          }
        }

        try {
          await droid.deleteSession(session.id)
        } catch {
          // ignore
        } finally {
          get()._clearSessionGeneration(session.id)
        }
      }

      get()._setProjects((prev) => prev.filter((p) => p.dir !== repoRoot))
      get()._setSessionBuffers((prev) => {
        const next = new Map(prev)
        for (const sid of targetSessionIds) next.delete(sid)
        return next
      })

      const activeSession = get().sessionBuffers.get(get().activeSessionId)
      if (activeSession && (activeSession.repoRoot || activeSession.projectDir) === repoRoot) {
        set({ activeProjectDir: '' })
        droid.setProjectDir(null)
        const newId = uuidv4()
        get()._setSessionBuffers((prev) => {
          const next = new Map(prev)
          next.set(newId, makeBuffer(''))
          return next
        })
        set({ activeSessionId: newId })
      }
    })()
  },
}))

// --- Fine-grained selector hooks ---
// Each hook subscribes to only the slice of state it needs,
// so components only re-render when their specific data changes.
//
// IMPORTANT: Selectors must return referentially stable values.
// - Never use `?? []` or `|| {}` inline (creates new ref every call).
// - Never return functions selected from the store (new ref each snapshot).
// - Use module-level sentinel values for fallbacks.

const EMPTY_MESSAGES: ChatMessage[] = []
const EMPTY_DEBUG_TRACE: string[] = []
const EMPTY_PENDING_SEND: Record<string, true> = {}

const selectActiveBuffer = (s: AppStore) => s.sessionBuffers.get(s.activeSessionId)

export const useMessages = () =>
  useAppStore((s) => selectActiveBuffer(s)?.messages ?? EMPTY_MESSAGES)
export const useIsRunning = () =>
  useAppStore((s) => {
    const buf = selectActiveBuffer(s)
    return Boolean(buf?.isRunning)
  })
export const useIsAnyRunning = () =>
  useAppStore((s) => Array.from(s.sessionBuffers.values()).some((b) => Boolean(b?.isRunning)))
export const useDebugTrace = () =>
  useAppStore(
    (s) => (selectActiveBuffer(s)?.debugTrace as string[] | undefined) ?? EMPTY_DEBUG_TRACE,
  )
export const useSetupScript = () => useAppStore((s) => selectActiveBuffer(s)?.setupScript ?? null)
export const useIsSetupBlocked = () =>
  useAppStore((s) => {
    const buf = selectActiveBuffer(s)
    return Boolean(buf && (buf.isSetupRunning || buf.setupScript.status === 'failed'))
  })
export const useModel = () => useAppStore((s) => selectActiveBuffer(s)?.model ?? DEFAULT_MODEL)
export const useAutoLevel = () =>
  useAppStore((s) => selectActiveBuffer(s)?.autoLevel ?? DEFAULT_AUTO_LEVEL)
export const useReasoningEffort = () =>
  useAppStore((s) => selectActiveBuffer(s)?.reasoningEffort ?? '')
export const useTokenUsage = () => useAppStore((s) => selectActiveBuffer(s)?.tokenUsage ?? null)
export const useMcpServers = () => useAppStore((s) => selectActiveBuffer(s)?.mcpServers ?? null)
export const useMcpAuthRequired = () =>
  useAppStore((s) => selectActiveBuffer(s)?.mcpAuthRequired ?? null)
export const useSettingsFlashAt = () =>
  useAppStore((s) => selectActiveBuffer(s)?.settingsFlashAt ?? 0)
export const useIsCancelling = () =>
  useAppStore((s) => Boolean(selectActiveBuffer(s)?.isCancelling))
export const usePendingPermissionRequest = () =>
  useAppStore((s) => selectActiveBuffer(s)?.pendingPermissionRequests?.[0] ?? null)
export const usePendingAskUserRequest = () =>
  useAppStore((s) => selectActiveBuffer(s)?.pendingAskUserRequests?.[0] ?? null)
export const usePendingSendMessageIds = () =>
  useAppStore((s) => selectActiveBuffer(s)?.pendingSendMessageIds ?? EMPTY_PENDING_SEND)
export const useActiveSessionTitle = () =>
  useAppStore((s) => {
    for (const p of s.projects) {
      const sess = p.sessions.find((x) => x.id === s.activeSessionId)
      if (sess) return sess.title
    }
    return ''
  })

export const useDroidVersion = () => useAppStore((s) => s.droidVersion)
export const useAppVersion = () => useAppStore((s) => s.appVersion)
export const useApiKey = () => useAppStore((s) => s.apiKey)
export const useTraceChainEnabled = () => useAppStore((s) => s.traceChainEnabled)
export const useShowDebugTrace = () => useAppStore((s) => s.showDebugTrace)
export const useDebugTraceMaxLines = () => useAppStore((s) => s.debugTraceMaxLines)
export const useLocalDiagnosticsEnabled = () => useAppStore((s) => s.localDiagnosticsEnabled)
export const useLocalDiagnosticsRetentionDays = () =>
  useAppStore((s) => s.localDiagnosticsRetentionDays)
export const useLocalDiagnosticsMaxTotalMb = () => useAppStore((s) => s.localDiagnosticsMaxTotalMb)
export const useDiagnosticsDir = () => useAppStore((s) => s.diagnosticsDir)
export const useCommitMessageModelId = () => useAppStore((s) => s.commitMessageModelId)
export const useLanAccessEnabled = () => useAppStore((s) => s.lanAccessEnabled)
export const useCustomModels = () => useAppStore((s) => s.customModels)
export const useProjects = () => useAppStore((s) => s.projects)
export const useProjectSettingsByRepo = () => useAppStore((s) => s.projectSettingsByRepo)
export const useActiveProjectDir = () => useAppStore((s) => s.activeProjectDir)
export const useActiveSessionId = () => useAppStore((s) => s.activeSessionId)
export const usePendingNewSession = () => useAppStore((s) => s.pendingNewSession)
export const useWorkspaceError = () => useAppStore((s) => s.workspaceError)
export const useDeletingSessionIds = () => useAppStore((s) => s.deletingSessionIds)
export const useIsCreatingSession = () => useAppStore((s) => s.isCreatingSession)
export const useIsInitialLoadDone = () => useAppStore((s) => s._initialLoadDone)

// Actions are stable function references on the store object and never change,
// so we read them directly via getState() without subscribing.
export function useActions() {
  return useAppStore.getState()
}
