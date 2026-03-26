import { create } from 'zustand'
import { getDroidClient } from '@/droidClient'
import type {
  ChatMessage,
  Project,
  SessionMeta,
  WorkspaceInfo,
  ProjectSettings,
  MissionModelSettings,
} from '@/types'
import type { DroidPermissionOption, CustomModelDef } from '@/types'
import { buildHookMismatchMessage, getMissingDroidHooks } from '@/lib/droidHooks'
import { uuidv4 } from './lib/uuid.ts'
import {
  defaultSessionTitleFromBranch,
  generateWorktreeBranch,
  sanitizeWorktreePrefix,
} from '@/lib/sessionWorktree'
import {
  createLocalWorkspaceInfo,
  getGitWorkspaceDir,
  getLaunchProjectDir,
  isLocalWorkspaceType,
  supportsGitWorkspace,
} from '@/lib/workspaceType'
import { isTraceChainEnabled, setTraceChainEnabledOverride } from '@/lib/notificationFingerprint'
import { getModelDefaultReasoning } from '@/types'
import {
  DEFAULT_AUTO_LEVEL,
  DEFAULT_MODEL,
  makeBuffer,
  applySetupScriptEvent,
  appendDebugTrace,
  appendRuntimeLog,
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
import { buildRestoredSessionBuffer } from '@/state/sessionRestore'
import {
  getTitleFromPrompt,
  upsertSessionMeta,
  replaceSessionIdInProjects,
  renameProject,
} from './store/projectHelpers'
import { isExitSpecPermission } from '@/components/SpecReviewCard'
import {
  getPendingSessionProtocol,
  mergePendingSessionDraft,
  type PendingSessionDraft,
  type PendingSessionDraftMode,
} from '@/lib/pendingSessionDraft'
import {
  autonomyLevelFromAutoLevel,
  interactionModeFromAutoLevel,
  resolveSessionProtocolFields,
} from '../../shared/sessionProtocol.ts'
import { resolveSessionRuntimeSelection } from '@/lib/missionModelState'

const droid = getDroidClient()
const MISSION_RESUME_AFTER_WORKER_FOLLOWUP_PROMPT =
  'Resume the mission run now. The paused worker has already received additional user guidance from me.'

export type SendInput = string | { text: string; tag?: { type: 'command' | 'skill'; name: string } }

export type PendingNewSessionMode = PendingSessionDraftMode

export type PendingNewSession = PendingSessionDraft

type PendingInitialSend = {
  sessionId: string
  input: SendInput
  attachments: Array<{ name: string; path: string }>
}

function getSessionProtocol(
  source?:
    | Partial<
        Pick<
          SessionMeta,
          | 'autoLevel'
          | 'isMission'
          | 'sessionKind'
          | 'interactionMode'
          | 'autonomyLevel'
          | 'decompSessionType'
        >
      >
    | Partial<
        Pick<
          SessionBuffer,
          | 'autoLevel'
          | 'isMission'
          | 'sessionKind'
          | 'interactionMode'
          | 'autonomyLevel'
          | 'decompSessionType'
        >
      >
    | null,
) {
  return resolveSessionProtocolFields({
    autoLevel: source?.autoLevel,
    explicit: {
      isMission: source?.isMission,
      sessionKind: source?.sessionKind,
      interactionMode: source?.interactionMode,
      autonomyLevel: source?.autonomyLevel,
      decompSessionType: source?.decompSessionType,
    },
  })
}

function createPendingSessionFromWorkspace(
  workspaceInfo: WorkspaceInfo,
  sessionKind: PendingNewSession['sessionKind'] = 'normal',
): PendingNewSession {
  return {
    repoRoot: workspaceInfo.repoRoot,
    projectDir: workspaceInfo.projectDir,
    workspaceDir: workspaceInfo.workspaceDir,
    cwdSubpath: workspaceInfo.cwdSubpath,
    workspaceType: workspaceInfo.workspaceType,
    branch: '',
    isExistingBranch: false,
    mode: 'local',
    sessionKind,
  }
}

function getMissionBaseSessionId(
  source?:
    | Partial<Pick<SessionMeta, 'missionBaseSessionId' | 'autoLevel' | 'isMission' | 'sessionKind'>>
    | Partial<
        Pick<SessionBuffer, 'missionBaseSessionId' | 'autoLevel' | 'isMission' | 'sessionKind'>
      >
    | null,
  fallbackSessionId?: string,
): string | undefined {
  const explicit =
    typeof source?.missionBaseSessionId === 'string' ? source.missionBaseSessionId.trim() : ''
  if (explicit) return explicit
  const protocol = getSessionProtocol(source)
  const fallback = String(fallbackSessionId || '').trim()
  return protocol.isMission && fallback ? fallback : undefined
}

function getWorkerFollowupAliasSessionId(parentSessionId: string, workerSessionId: string): string {
  return `mission-worker:${parentSessionId}:${workerSessionId}`
}

// --- Zustand Store ---

interface AppState {
  // Session buffers
  sessionBuffers: Map<string, SessionBuffer>
  sessionEventParentBySessionId: Record<string, string>
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
  commitMessageReasoningEffort: string
  lanAccessEnabled: boolean
  telemetryEnabled: boolean
  missionModelSettings: MissionModelSettings
  customModels: CustomModelDef[]
  projects: Project[]
  projectSettingsByRepo: Record<string, ProjectSettings>
  workspaceError: string
  deletingSessionIds: Set<string>
  isCreatingSession: boolean

  // Update notification
  updateAvailable: { version: string } | null
  updateDownloading: boolean
  updateDownloadProgress: number
  updateReady: boolean

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
  setCommitMessageReasoningEffort: (r: string) => void
  setLanAccessEnabled: (enabled: boolean) => void
  setTelemetryEnabled: (enabled: boolean) => void
  telemetryCapture: (
    event: string,
    properties?: Record<string, string | number | boolean | undefined>,
  ) => void
  setMissionModelSettings: (settings: MissionModelSettings) => Promise<void>
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
  handleKillWorker: (workerSessionId?: string) => Promise<void>
  handleSendWorkerFollowup: (params: { workerSessionId?: string; prompt: string }) => Promise<void>
  handleRespondPermission: (params: {
    selectedOption: DroidPermissionOption
    autoLevel?: 'low' | 'medium' | 'high'
    selectedExitSpecModeOptionIndex?: number
    exitSpecModeComment?: string
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
    workspaceDir?: string
    cwdSubpath?: string
    workspaceType?: WorkspaceInfo['workspaceType']
    mode: 'plain' | 'switch-branch' | 'new-branch' | 'new-worktree'
    branch?: string
    baseBranch?: string
    sessionKind?: PendingNewSession['sessionKind']
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
  handleRenameProject: (repoRoot: string, displayName: string) => void

  // Internal helpers
  _bumpSessionGeneration: (sid: string) => number
  _isSessionGenerationCurrent: (sid: string, generation: number) => boolean
  _clearSessionGeneration: (sid: string) => void
  _getSessionGeneration: (sid: string) => number
  _resolveWorkspace: (projectDir: string, cwdSubpath?: string) => Promise<WorkspaceInfo | null>
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

  // Update actions
  setUpdateAvailable: (update: { version: string } | null) => void
  setUpdateDownloading: (downloading: boolean) => void
  setUpdateDownloadProgress: (progress: number) => void
  setUpdateReady: (ready: boolean) => void
}

type AppStore = AppState & AppActions

export const useAppStore = create<AppStore>((set, get) => ({
  // --- Initial State ---
  sessionBuffers: new Map(),
  sessionEventParentBySessionId: {},
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
  commitMessageReasoningEffort: '',
  lanAccessEnabled: false,
  telemetryEnabled: true,
  missionModelSettings: {},
  customModels: [],
  projects: [],
  projectSettingsByRepo: {},
  workspaceError: '',
  deletingSessionIds: new Set(),
  isCreatingSession: false,
  updateAvailable: null,
  updateDownloading: false,
  updateDownloadProgress: 0,
  updateReady: false,
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
  _resolveWorkspace: async (projectDir, cwdSubpath) => {
    if (!projectDir) return null
    return droid.getWorkspaceInfo({ projectDir, cwdSubpath })
  },

  _pickProjectDirForRepo: (repoRoot) => {
    const s = get()
    if (!repoRoot) return s.activeProjectDir
    const activeBuf = s.activeSessionId ? s.sessionBuffers.get(s.activeSessionId) : null
    if (
      activeBuf &&
      (activeBuf.repoRoot || activeBuf.workspaceDir || activeBuf.projectDir) === repoRoot
    )
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
    const protocol = getSessionProtocol(buf || existingMeta)
    const missionBaseSessionId = getMissionBaseSessionId(buf || existingMeta, sid)
    const meta = await droid.saveSession({
      id: sid,
      projectDir: buf.projectDir,
      workspaceDir: buf.workspaceDir,
      cwdSubpath: buf.cwdSubpath,
      repoRoot: buf.repoRoot,
      branch: buf.branch,
      workspaceType: buf.workspaceType,
      baseBranch: buf.baseBranch,
      model: buf.model || DEFAULT_MODEL,
      autoLevel: buf.autoLevel || DEFAULT_AUTO_LEVEL,
      missionDir: buf.missionDir,
      missionBaseSessionId,
      isMission: protocol.isMission,
      sessionKind: protocol.sessionKind,
      interactionMode: protocol.interactionMode,
      autonomyLevel: protocol.autonomyLevel,
      decompSessionType: protocol.decompSessionType,
      reasoningEffort: buf.reasoningEffort || undefined,
      apiKeyFingerprint: buf.apiKeyFingerprint || undefined,
      pinned: existingMeta?.pinned || undefined,
      messages: buf.messages,
      runtimeLogs: buf.runtimeLogs,
    })
    if (!meta) return

    const normalizedMeta: SessionMeta = {
      ...meta,
      projectDir: meta.projectDir || buf.projectDir,
      workspaceDir: meta.workspaceDir || buf.workspaceDir || meta.projectDir || buf.projectDir,
      cwdSubpath: meta.cwdSubpath || buf.cwdSubpath,
      repoRoot:
        meta.repoRoot ||
        buf.repoRoot ||
        meta.workspaceDir ||
        meta.projectDir ||
        buf.workspaceDir ||
        buf.projectDir,
      branch: meta.branch || buf.branch,
      workspaceType: meta.workspaceType || buf.workspaceType,
      baseBranch: meta.baseBranch || buf.baseBranch,
      missionDir: meta.missionDir || buf.missionDir,
      missionBaseSessionId: meta.missionBaseSessionId || missionBaseSessionId,
      isMission: meta.isMission ?? protocol.isMission,
      sessionKind: meta.sessionKind || protocol.sessionKind,
      interactionMode: meta.interactionMode || protocol.interactionMode,
      autonomyLevel: meta.autonomyLevel || protocol.autonomyLevel,
      decompSessionType: meta.decompSessionType || protocol.decompSessionType,
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
      const isMission = buf.isMission === true || buf.sessionKind === 'mission'
      next.set(sid, {
        ...buf,
        autoLevel: l,
        ...(!isMission
          ? {
              interactionMode: interactionModeFromAutoLevel(l),
              autonomyLevel: autonomyLevelFromAutoLevel(l),
            }
          : {}),
      })
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
    const defaultReasoning = getModelDefaultReasoning(next)
    set({ commitMessageModelId: next, commitMessageReasoningEffort: defaultReasoning })
    if (typeof (droid as any)?.setCommitMessageModelId === 'function') {
      ;(droid as any).setCommitMessageModelId(next)
    }
    if (typeof (droid as any)?.setCommitMessageReasoningEffort === 'function') {
      ;(droid as any).setCommitMessageReasoningEffort(defaultReasoning)
    }
  },

  setCommitMessageReasoningEffort: (r) => {
    const next = String(r || '').trim()
    set({ commitMessageReasoningEffort: next })
    if (typeof (droid as any)?.setCommitMessageReasoningEffort === 'function') {
      ;(droid as any).setCommitMessageReasoningEffort(next)
    }
  },

  setLanAccessEnabled: (enabled) => {
    const next = Boolean(enabled)
    set({ lanAccessEnabled: next })
    if (typeof (droid as any)?.setLanAccessEnabled === 'function') {
      ;(droid as any).setLanAccessEnabled(next)
    }
  },

  setTelemetryEnabled: (enabled) => {
    const next = Boolean(enabled)
    set({ telemetryEnabled: next })
    if (typeof (droid as any)?.setTelemetryEnabled === 'function') {
      ;(droid as any).setTelemetryEnabled(next)
    }
  },

  telemetryCapture: (event, properties) => {
    if (!get().telemetryEnabled) return
    if (typeof (droid as any)?.telemetryCapture === 'function') {
      ;(droid as any).telemetryCapture({ event, properties })
    }
  },

  setMissionModelSettings: async (settings) => {
    const normalize = (value: unknown) => {
      if (typeof value !== 'string') return undefined
      const trimmed = value.trim()
      return trimmed || undefined
    }
    const next = {
      orchestratorModel: normalize(settings.orchestratorModel),
      workerModel: normalize(settings.workerModel),
      validationWorkerModel: normalize(settings.validationWorkerModel),
    }
    set({ missionModelSettings: next })
    if (typeof (droid as any)?.setMissionModelSettings === 'function') {
      const persisted = await (droid as any).setMissionModelSettings(next)
      set({ missionModelSettings: persisted ?? next })
    }

    const latest = get()
    const sid = latest.activeSessionId
    if (!sid) return

    const activeBuffer = latest.sessionBuffers.get(sid)
    const activeMeta = latest.projects.flatMap((p) => p.sessions).find((x) => x.id === sid)
    const protocol = getSessionProtocol(activeBuffer || activeMeta)
    if (!protocol.isMission) return

    const runtimeSelection = resolveSessionRuntimeSelection({
      isMission: true,
      sessionModel: activeBuffer?.model || activeMeta?.model,
      sessionReasoningEffort: activeBuffer?.reasoningEffort || activeMeta?.reasoningEffort,
      missionModelSettings: get().missionModelSettings,
    })

    const currentModel = activeBuffer?.model || activeMeta?.model || DEFAULT_MODEL
    const currentReasoningEffort =
      activeBuffer?.reasoningEffort || activeMeta?.reasoningEffort || ''
    if (
      currentModel === runtimeSelection.model &&
      currentReasoningEffort === runtimeSelection.reasoningEffort
    ) {
      return
    }

    let nextBuffer = activeBuffer
    if (activeBuffer) {
      nextBuffer = {
        ...activeBuffer,
        model: runtimeSelection.model,
        reasoningEffort: runtimeSelection.reasoningEffort,
      }
      get()._setSessionBuffers((prev) => {
        const session = prev.get(sid)
        if (!session) return prev
        const updated = new Map(prev)
        updated.set(sid, {
          ...session,
          model: runtimeSelection.model,
          reasoningEffort: runtimeSelection.reasoningEffort,
        })
        return updated
      })
    }

    get()._setProjects((prev) =>
      upsertSessionMeta(prev, {
        ...(activeMeta || {
          id: sid,
          projectDir: activeBuffer?.projectDir || latest.activeProjectDir,
          title: 'Untitled',
          savedAt: Date.now(),
          messageCount: activeBuffer?.messages.length || 0,
          autoLevel: activeBuffer?.autoLevel || DEFAULT_AUTO_LEVEL,
        }),
        model: runtimeSelection.model,
        reasoningEffort: runtimeSelection.reasoningEffort || undefined,
      } as SessionMeta),
    )

    await droid.updateSessionSettings({
      sessionId: sid,
      modelId: runtimeSelection.model,
      autoLevel: activeBuffer?.autoLevel || activeMeta?.autoLevel || DEFAULT_AUTO_LEVEL,
      isMission: protocol.isMission,
      sessionKind: protocol.sessionKind,
      interactionMode: protocol.interactionMode,
      autonomyLevel: protocol.autonomyLevel,
      decompSessionType: protocol.decompSessionType,
      reasoningEffort: runtimeSelection.reasoningEffort || undefined,
    })

    if (nextBuffer) {
      await get()._saveSessionToDisk(sid, nextBuffer)
    }
  },

  // --- Update actions ---
  setUpdateAvailable: (update) => set({ updateAvailable: update }),
  setUpdateDownloading: (downloading) => set({ updateDownloading: downloading }),
  setUpdateDownloadProgress: (progress) => set({ updateDownloadProgress: progress }),
  setUpdateReady: (ready) => set({ updateReady: ready }),

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
    set((prev) => {
      const cur = prev.pendingNewSession
      if (!cur) return {}
      return {
        pendingNewSession: mergePendingSessionDraft(cur, patch as Partial<PendingNewSession>),
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
      const sourceProjectDir = getLaunchProjectDir({
        projectDir: params.projectDir || s._pickProjectDirForRepo(params.repoRoot),
        workspaceDir: params.workspaceDir,
        repoRoot: params.repoRoot,
      })
      const sourceWorkspaceDir = getGitWorkspaceDir({
        workspaceDir: params.workspaceDir,
        projectDir: sourceProjectDir,
      })
      if (!sourceProjectDir || !sourceWorkspaceDir)
        throw new Error('No project directory available')

      const currentSid = s.activeSessionId
      const currentBuf = s.sessionBuffers.get(currentSid)
      if (currentBuf && currentBuf.messages.length > 0 && !currentBuf.isRunning) {
        void s._saveSessionToDisk(currentSid, currentBuf)
      }

      const inheritModel = currentBuf?.model ?? DEFAULT_MODEL
      const inheritAutoLevel = currentBuf?.autoLevel ?? DEFAULT_AUTO_LEVEL
      const inheritReasoningEffort = currentBuf?.reasoningEffort ?? ''
      const sessionProtocol = getPendingSessionProtocol(
        { sessionKind: params.sessionKind },
        inheritAutoLevel,
      )
      const runtimeSelection = resolveSessionRuntimeSelection({
        isMission: sessionProtocol.isMission,
        sessionModel: inheritModel,
        sessionReasoningEffort: inheritReasoningEffort,
        missionModelSettings: s.missionModelSettings,
      })
      const sourceCwdSubpath =
        typeof params.cwdSubpath === 'string' && params.cwdSubpath.trim()
          ? params.cwdSubpath.trim()
          : undefined
      const sourceWorkspaceType = params.workspaceType

      let workspaceInfo: WorkspaceInfo | null = null
      if (params.mode === 'switch-branch') {
        if (!params.branch?.trim()) throw new Error('Missing branch')
        workspaceInfo = await droid.switchWorkspace({
          workspaceDir: sourceWorkspaceDir,
          branch: params.branch.trim(),
          cwdSubpath: sourceCwdSubpath,
        })
        if (!workspaceInfo) throw new Error('Failed to switch branch')
      } else if (params.mode === 'new-branch') {
        if (!params.branch?.trim()) throw new Error('Missing branch')
        workspaceInfo = await droid.createWorkspace({
          workspaceDir: sourceWorkspaceDir,
          projectDir: sourceProjectDir,
          mode: 'branch',
          branch: params.branch.trim(),
          baseBranch: params.baseBranch?.trim() || undefined,
          cwdSubpath: sourceCwdSubpath,
        })
        if (!workspaceInfo) throw new Error('Failed to create branch')
      } else if (params.mode === 'new-worktree') {
        if (!params.branch?.trim()) throw new Error('Missing branch')
        workspaceInfo = await droid.createWorkspace({
          workspaceDir: sourceWorkspaceDir,
          projectDir: sourceProjectDir,
          mode: 'worktree',
          branch: params.branch.trim(),
          baseBranch: params.baseBranch?.trim() || undefined,
          cwdSubpath: sourceCwdSubpath,
        })
        if (!workspaceInfo) throw new Error('Failed to create worktree')
      } else {
        workspaceInfo = await s
          ._resolveWorkspace(sourceProjectDir, sourceCwdSubpath)
          .catch(() => null)
      }

      if (!workspaceInfo) {
        if (isLocalWorkspaceType(sourceWorkspaceType)) {
          const normalizedSourceDir = String(sourceProjectDir || '').trim()
          workspaceInfo = createLocalWorkspaceInfo({
            projectDir: normalizedSourceDir,
            repoRoot: params.repoRoot || normalizedSourceDir,
            workspaceDir: normalizedSourceDir,
            cwdSubpath: sourceCwdSubpath,
          })
        } else {
          throw new Error('Failed to resolve workspace')
        }
      }

      const targetDir = workspaceInfo.projectDir
      const { sessionId: newId } = await droid.createSession({
        cwd: targetDir,
        modelId: runtimeSelection.model,
        autoLevel: inheritAutoLevel,
        isMission: sessionProtocol.isMission,
        sessionKind: sessionProtocol.sessionKind,
        interactionMode: sessionProtocol.interactionMode,
        autonomyLevel: sessionProtocol.autonomyLevel,
        decompSessionType: sessionProtocol.decompSessionType,
        reasoningEffort: runtimeSelection.reasoningEffort || undefined,
      })

      const initialTitle = defaultSessionTitleFromBranch(workspaceInfo.branch)
      const now = Date.now()
      const missionBaseSessionId = sessionProtocol.isMission ? newId : undefined
      get()._setProjects((prev) =>
        upsertSessionMeta(prev, {
          id: newId,
          projectDir: targetDir,
          workspaceDir: workspaceInfo!.workspaceDir,
          cwdSubpath: workspaceInfo!.cwdSubpath,
          repoRoot: workspaceInfo!.repoRoot,
          branch: workspaceInfo!.branch,
          workspaceType: workspaceInfo!.workspaceType,
          title: initialTitle,
          savedAt: now,
          messageCount: 0,
          model: runtimeSelection.model,
          autoLevel: inheritAutoLevel,
          missionBaseSessionId,
          isMission: sessionProtocol.isMission,
          sessionKind: sessionProtocol.sessionKind,
          interactionMode: sessionProtocol.interactionMode,
          autonomyLevel: sessionProtocol.autonomyLevel,
          decompSessionType: sessionProtocol.decompSessionType,
          reasoningEffort: runtimeSelection.reasoningEffort || undefined,
          baseBranch: workspaceInfo!.baseBranch,
        }),
      )
      const initialBuffer = {
        ...makeBuffer(targetDir, {
          repoRoot: workspaceInfo!.repoRoot,
          workspaceDir: workspaceInfo!.workspaceDir,
          cwdSubpath: workspaceInfo!.cwdSubpath,
          branch: workspaceInfo!.branch,
          workspaceType: workspaceInfo!.workspaceType,
          baseBranch: workspaceInfo!.baseBranch,
        }),
        model: runtimeSelection.model,
        autoLevel: inheritAutoLevel,
        missionBaseSessionId,
        isMission: sessionProtocol.isMission,
        sessionKind: sessionProtocol.sessionKind,
        interactionMode: sessionProtocol.interactionMode,
        autonomyLevel: sessionProtocol.autonomyLevel,
        decompSessionType: sessionProtocol.decompSessionType,
        reasoningEffort: runtimeSelection.reasoningEffort || '',
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
    if (isLocalWorkspaceType(buf?.workspaceType || meta?.workspaceType)) return false
    const workspaceDir = getGitWorkspaceDir({
      workspaceDir: buf?.workspaceDir || meta?.workspaceDir,
      projectDir: buf?.projectDir || meta?.projectDir || s.activeProjectDir,
    })
    const cwdSubpath = buf?.cwdSubpath || meta?.cwdSubpath
    if (!workspaceDir) return false

    let info: WorkspaceInfo | null = null
    try {
      info = await droid.switchWorkspace({ workspaceDir, branch, cwdSubpath })
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
        workspaceDir: info!.workspaceDir,
        cwdSubpath: info!.cwdSubpath,
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
      workspaceDir: info.workspaceDir,
      cwdSubpath: info.cwdSubpath,
      repoRoot: info.repoRoot,
      branch: info.branch,
      workspaceType: info.workspaceType,
      baseBranch: meta?.baseBranch || buf?.baseBranch,
      title: meta?.title || 'Untitled',
      savedAt: meta?.savedAt || now,
      messageCount: meta?.messageCount || 0,
      model: meta?.model || (buf?.model ?? DEFAULT_MODEL),
      autoLevel: meta?.autoLevel || (buf?.autoLevel ?? DEFAULT_AUTO_LEVEL),
      missionDir: meta?.missionDir || buf?.missionDir,
      missionBaseSessionId: meta?.missionBaseSessionId || buf?.missionBaseSessionId,
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

    // If a new-session flow is active, temporarily clear it so handleSend routes
    // to the current (existing) session instead of being hijacked by pendingNewSession.
    const savedPending = s.pendingNewSession
    try {
      if (savedPending) {
        set({ pendingNewSession: null })
      }
      s.handleSend(pending.input, pending.attachments)
    } finally {
      if (savedPending) {
        set({ pendingNewSession: savedPending })
      }
    }
  },

  _confirmPendingNewSessionAndSend: async ({ input, attachments }) => {
    const s = get()
    const pending = s.pendingNewSession
    if (!pending) return
    if (!s._initialLoadDone) return
    if (s.isCreatingSession) return

    s.clearWorkspaceError()

    const repoRoot = String(pending.repoRoot || '').trim()
    const projectDir = String(pending.projectDir || repoRoot).trim()
    const cwdSubpath = String(pending.cwdSubpath || '').trim() || undefined
    const workspaceType = pending.workspaceType
    if (!repoRoot) {
      set({ workspaceError: 'Missing repo root' })
      return
    }

    if (isLocalWorkspaceType(workspaceType)) {
      if (pending.sessionKind === 'mission') {
        set({ workspaceError: 'Mission mode requires a Git project.' })
        return
      }
      if (pending.mode && pending.mode !== 'local') {
        set({ workspaceError: 'Non-Git projects only support Work From Local.' })
        return
      }
    }

    const settings = s.projectSettingsByRepo[repoRoot] || {}
    const prefix = sanitizeWorktreePrefix(settings.worktreePrefix || '') || 'droi'

    const baseBranchFromSettings =
      typeof settings.baseBranch === 'string' ? settings.baseBranch.trim() : ''

    const queuedAttachments = attachments ?? []

    let branch = String(pending.branch || '').trim()
    let createMode: 'plain' | 'new-worktree' | 'switch-branch'

    if (pending.mode === 'local') {
      createMode = 'plain'
    } else if (pending.isExistingBranch && branch) {
      createMode = 'switch-branch'
    } else {
      createMode = 'new-worktree'
      if (!branch) {
        branch = generateWorktreeBranch(prefix)
      }
    }

    const baseBranch = String(baseBranchFromSettings || '').trim()
    if (createMode === 'new-worktree' && !baseBranch) {
      set({ workspaceError: 'Missing base branch. Configure it in Project Settings first.' })
      return
    }

    // Create the session/workspace first.
    try {
      const newId = await s.handleCreateSessionWithWorkspace({
        repoRoot,
        projectDir,
        workspaceDir: pending.workspaceDir,
        cwdSubpath,
        workspaceType,
        mode: createMode,
        branch,
        sessionKind: pending.sessionKind,
        ...(createMode === 'new-worktree' ? { baseBranch } : {}),
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
          missionBaseSessionId: normalizedMeta.missionBaseSessionId || session.missionBaseSessionId,
          isRunning: false,
          isCancelling: false,
          pendingSendMessageIds: {},
          pendingPermissionRequests: [],
          pendingAskUserRequests: [],
          messages: [],
          debugTrace: [],
          runtimeLogs: [],
          runtimeLogState: undefined,
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
        runtimeLogs: [],
        runtimeLogState: undefined,
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
    const existingMeta = s.projects.flatMap((p) => p.sessions).find((x) => x.id === sid)
    const protocol = getSessionProtocol(buf || existingMeta)
    const runtimeSelection = resolveSessionRuntimeSelection({
      isMission: protocol.isMission,
      sessionModel: buf?.model,
      sessionReasoningEffort: buf?.reasoningEffort,
      missionModelSettings: s.missionModelSettings,
    })
    const sessionModel = runtimeSelection.model
    const sessionAutoLevel = buf?.autoLevel ?? DEFAULT_AUTO_LEVEL
    const sessionReasoningEffort = runtimeSelection.reasoningEffort

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
    const missionBaseSessionId = getMissionBaseSessionId(buf || existingMeta, sid)
    const draftMeta: SessionMeta = {
      id: sid,
      projectDir: projDir,
      workspaceDir: buf?.workspaceDir || projDir,
      cwdSubpath: buf?.cwdSubpath,
      repoRoot: buf?.repoRoot || buf?.workspaceDir || projDir,
      branch: buf?.branch,
      workspaceType: buf?.workspaceType,
      baseBranch: buf?.baseBranch,
      title: nextTitle,
      savedAt: now,
      messageCount,
      model: sessionModel,
      autoLevel: sessionAutoLevel,
      missionDir: buf?.missionDir,
      missionBaseSessionId,
      isMission: protocol.isMission,
      sessionKind: protocol.sessionKind,
      interactionMode: protocol.interactionMode,
      autonomyLevel: protocol.autonomyLevel,
      decompSessionType: protocol.decompSessionType,
      reasoningEffort: sessionReasoningEffort || undefined,
      apiKeyFingerprint: buf?.apiKeyFingerprint || undefined,
    }

    get()._setProjects((prev) => upsertSessionMeta(prev, draftMeta))

    void droid
      .saveSession({
        id: sid,
        projectDir: projDir,
        workspaceDir: buf?.workspaceDir || projDir,
        cwdSubpath: buf?.cwdSubpath,
        repoRoot: buf?.repoRoot || buf?.workspaceDir || projDir,
        branch: buf?.branch,
        workspaceType: buf?.workspaceType,
        baseBranch: buf?.baseBranch,
        model: sessionModel,
        autoLevel: sessionAutoLevel,
        missionDir: buf?.missionDir,
        missionBaseSessionId,
        isMission: protocol.isMission,
        sessionKind: protocol.sessionKind,
        interactionMode: protocol.interactionMode,
        autonomyLevel: protocol.autonomyLevel,
        decompSessionType: protocol.decompSessionType,
        reasoningEffort: sessionReasoningEffort || undefined,
        apiKeyFingerprint: buf?.apiKeyFingerprint || undefined,
        pinned: existingMeta?.pinned || undefined,
        messages: nextMessages,
      })
      .then((meta) => {
        if (!meta) return
        const savedMeta: SessionMeta = {
          ...meta,
          projectDir: meta.projectDir || projDir,
          workspaceDir: meta.workspaceDir || buf?.workspaceDir || meta.projectDir || projDir,
          cwdSubpath: meta.cwdSubpath || buf?.cwdSubpath,
          repoRoot:
            meta.repoRoot ||
            buf?.repoRoot ||
            meta.workspaceDir ||
            meta.projectDir ||
            buf?.workspaceDir ||
            projDir,
          branch: meta.branch || buf?.branch,
          workspaceType: meta.workspaceType || buf?.workspaceType,
          baseBranch: meta.baseBranch || buf?.baseBranch,
          missionDir: meta.missionDir || buf?.missionDir,
          missionBaseSessionId: meta.missionBaseSessionId || missionBaseSessionId,
          isMission: meta.isMission ?? protocol.isMission,
          sessionKind: meta.sessionKind || protocol.sessionKind,
          interactionMode: meta.interactionMode || protocol.interactionMode,
          autonomyLevel: meta.autonomyLevel || protocol.autonomyLevel,
          decompSessionType: meta.decompSessionType || protocol.decompSessionType,
        }
        get()._setProjects((prev) => upsertSessionMeta(prev, savedMeta))
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
          model: sessionModel,
          reasoningEffort: sessionReasoningEffort,
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
        model: sessionModel,
        reasoningEffort: sessionReasoningEffort,
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
          const info = await droid.getActiveKeyInfo(sid)
          activeKeyFp = String((info as any)?.apiKeyFingerprint || '')
        } catch {
          // ignore
        }

        if (!get()._isSessionGenerationCurrent(sid, generation)) return

        if (activeKeyFp) {
          const before = get().sessionBuffers.get(sid)
          const prevFp = before?.apiKeyFingerprint
          if (prevFp !== activeKeyFp) {
            get()._setSessionBuffers((prev) => {
              const session = prev.get(sid)
              if (!session) return prev
              const next = new Map(prev)
              next.set(sid, { ...session, apiKeyFingerprint: activeKeyFp })
              return appendDebugTrace(
                next,
                sid,
                `api-key-selected: ${prevFp || '(unknown)'} -> ${activeKeyFp}`,
              )
            })
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
          isMission: protocol.isMission,
          sessionKind: protocol.sessionKind,
          interactionMode: protocol.interactionMode,
          autonomyLevel: protocol.autonomyLevel,
          decompSessionType: protocol.decompSessionType,
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

  handleKillWorker: async (workerSessionId) => {
    const s = get()
    const sid = s.activeSessionId || null
    if (!sid) return
    const buf = s.sessionBuffers.get(sid)
    const liveWorkerSessionId =
      (typeof workerSessionId === 'string' ? workerSessionId.trim() : '') ||
      String(buf?.mission?.liveWorkerSessionId || '').trim()
    if (!liveWorkerSessionId) return

    get()._setSessionBuffers((prev) =>
      appendDebugTrace(prev, sid, `ui-kill-worker: ${liveWorkerSessionId}`),
    )

    await droid.killWorkerSession({
      sessionId: sid,
      workerSessionId: liveWorkerSessionId,
    })
  },

  handleSendWorkerFollowup: async ({ workerSessionId, prompt }) => {
    const s = get()
    const sid = s.activeSessionId || null
    if (!sid) throw new Error('No active Mission session.')

    const buf = s.sessionBuffers.get(sid)
    const targetWorkerSessionId =
      (typeof workerSessionId === 'string' ? workerSessionId.trim() : '') ||
      String(buf?.mission?.pausedWorkerSessionId || '').trim()
    const text = String(prompt || '').trim()
    const cwd = String(buf?.projectDir || '').trim()
    if (!targetWorkerSessionId) throw new Error('No paused worker is available for follow-up.')
    if (!text) throw new Error('Follow-up message is empty.')
    if (!cwd) throw new Error('Missing working directory for Mission session.')

    const aliasSessionId = getWorkerFollowupAliasSessionId(sid, targetWorkerSessionId)
    set((prev) => ({
      sessionEventParentBySessionId: {
        ...prev.sessionEventParentBySessionId,
        [aliasSessionId]: sid,
      },
    }))

    get()._setSessionBuffers((prev) => {
      let next = appendDebugTrace(
        prev,
        sid,
        `ui-worker-followup: worker=${targetWorkerSessionId} chars=${text.length}`,
      )
      next = appendRuntimeLog(next, sid, {
        ts: Date.now(),
        stream: 'system',
        kind: 'status',
        workerSessionId: targetWorkerSessionId,
        text: `Sending follow-up to paused worker ${targetWorkerSessionId}`,
      })
      return next
    })

    let followupDelivered = false
    try {
      await droid.sendWorkerFollowup({
        sessionId: sid,
        workerSessionId: targetWorkerSessionId,
        aliasSessionId,
        cwd,
        prompt: text,
      })
      followupDelivered = true
      get()._setSessionBuffers((prev) => {
        let next = appendDebugTrace(
          prev,
          sid,
          `ui-worker-followup-delivered: worker=${targetWorkerSessionId}`,
        )
        next = appendRuntimeLog(next, sid, {
          ts: Date.now(),
          stream: 'system',
          kind: 'status',
          workerSessionId: targetWorkerSessionId,
          text: `Follow-up delivered to paused worker ${targetWorkerSessionId}. Requesting Mission resume...`,
        })
        return next
      })

      await droid.addUserMessage({
        sessionId: sid,
        text: MISSION_RESUME_AFTER_WORKER_FOLLOWUP_PROMPT,
      })

      get()._setSessionBuffers((prev) =>
        appendRuntimeLog(
          appendDebugTrace(
            prev,
            sid,
            `ui-worker-followup-resume-requested: worker=${targetWorkerSessionId}`,
          ),
          sid,
          {
            ts: Date.now(),
            stream: 'system',
            kind: 'status',
            workerSessionId: targetWorkerSessionId,
            text: `Mission resume requested after follow-up to paused worker ${targetWorkerSessionId}`,
          },
        ),
      )
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      get()._setSessionBuffers((prev) => {
        let next = appendDebugTrace(
          prev,
          sid,
          followupDelivered
            ? `ui-worker-followup-resume-failed: ${message}`
            : `ui-worker-followup-failed: ${message}`,
        )
        next = appendRuntimeLog(next, sid, {
          ts: Date.now(),
          stream: 'system',
          kind: 'status',
          workerSessionId: targetWorkerSessionId,
          text: followupDelivered
            ? `Follow-up reached paused worker ${targetWorkerSessionId}, but Mission resume failed: ${message}`
            : `Failed to send follow-up to paused worker ${targetWorkerSessionId}: ${message}`,
        })
        return next
      })
      if (followupDelivered) {
        throw new Error(`Follow-up delivered, but failed to resume Mission: ${message}`, {
          cause: error,
        })
      }
      throw error
    }
  },

  handleRespondPermission: (params) => {
    const s = get()
    const sid = s.activeSessionId
    const buf = s.sessionBuffers.get(sid)
    const req = buf?.pendingPermissionRequests?.[0]
    if (!sid || !req) return
    const requestSessionId = req.sessionId || sid

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
        `ui-permission-response: ${selectedOption} requestKey=${req.requestKey}`,
      ),
    )

    // Workaround: droid CLI stream-jsonrpc strips extra fields from permission response.
    // Send option/comment as a user message BEFORE approving so the model sees it in context.
    if (
      selectedOption !== 'cancel' &&
      isExitSpecPermission(req) &&
      (params.selectedExitSpecModeOptionIndex != null || params.exitSpecModeComment)
    ) {
      let optionName: string | undefined
      if (params.selectedExitSpecModeOptionIndex != null) {
        for (const item of req.toolUses as any[]) {
          const raw = (item as any)?.toolUse || item
          const input = (raw as any)?.input || (raw as any)?.parameters
          if (input && Array.isArray(input.optionNames)) {
            optionName = input.optionNames[params.selectedExitSpecModeOptionIndex]
            break
          }
        }
      }
      const parts: string[] = []
      if (optionName) parts.push(`I chose: ${optionName}`)
      if (params.exitSpecModeComment) parts.push(`Comment: ${params.exitSpecModeComment}`)
      if (parts.length > 0) {
        const feedbackText = `[Spec Feedback] ${parts.join('\n')}`
        const capturedSid = sid
        const capturedRequestSessionId = requestSessionId
        const capturedParams = params
        void droid.addUserMessage({ sessionId: capturedSid, text: feedbackText }).then(
          () => {
            droid.respondPermission({
              sessionId: capturedRequestSessionId,
              selectedOption: capturedParams.selectedOption,
              selectedExitSpecModeOptionIndex: capturedParams.selectedExitSpecModeOptionIndex,
              exitSpecModeComment: capturedParams.exitSpecModeComment,
            })
          },
          () => {
            // Fallback: send permission response even if addUserMessage fails
            droid.respondPermission({
              sessionId: capturedRequestSessionId,
              selectedOption: capturedParams.selectedOption,
              selectedExitSpecModeOptionIndex: capturedParams.selectedExitSpecModeOptionIndex,
              exitSpecModeComment: capturedParams.exitSpecModeComment,
            })
          },
        )
      } else {
        droid.respondPermission({
          sessionId: requestSessionId,
          selectedOption,
          selectedExitSpecModeOptionIndex: params.selectedExitSpecModeOptionIndex,
          exitSpecModeComment: params.exitSpecModeComment,
        })
      }
    } else {
      droid.respondPermission({
        sessionId: requestSessionId,
        selectedOption,
        selectedExitSpecModeOptionIndex: params.selectedExitSpecModeOptionIndex,
        exitSpecModeComment: params.exitSpecModeComment,
      })
    }

    get()._setSessionBuffers((prev) => {
      const session = prev.get(sid)
      if (!session) return prev
      const rest = (session.pendingPermissionRequests || []).filter(
        (r) => r.requestKey !== req.requestKey,
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
          const session = get().sessionBuffers.get(sid)
          const protocol = getSessionProtocol(session)
          await droid.updateSessionSettings({
            sessionId: sid,
            autoLevel: newAutoLevel,
            isMission: protocol.isMission,
            sessionKind: protocol.sessionKind,
            interactionMode: protocol.interactionMode,
            autonomyLevel: protocol.autonomyLevel,
            decompSessionType: protocol.decompSessionType,
          })
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
    const requestSessionId = req.sessionId || sid
    get()._setSessionBuffers((prev) =>
      appendDebugTrace(
        prev,
        sid,
        `ui-askuser-response: cancelled=${Boolean(params.cancelled)} requestKey=${req.requestKey}`,
      ),
    )
    droid.respondAskUser({
      sessionId: requestSessionId,
      cancelled: params.cancelled,
      answers: params.answers,
    })
    get()._setSessionBuffers((prev) => {
      const session = prev.get(sid)
      if (!session) return prev
      const rest = (session.pendingAskUserRequests || []).filter(
        (r) => r.requestKey !== req.requestKey,
      )
      const next = new Map(prev)
      next.set(sid, { ...session, pendingAskUserRequests: rest })
      return next
    })
  },

  // --- Workspace for meta ---
  _ensureWorkspaceForMeta: async (meta) => {
    const desiredProjectDir = getLaunchProjectDir({
      projectDir: meta.projectDir,
      workspaceDir: meta.workspaceDir,
      repoRoot: meta.repoRoot,
    })
    const desiredWorkspaceDir = getGitWorkspaceDir({
      workspaceDir: meta.workspaceDir,
      projectDir: desiredProjectDir,
    })
    const desiredCwdSubpath = String(meta.cwdSubpath || '').trim() || undefined
    if (!desiredProjectDir || !desiredWorkspaceDir)
      throw new Error('Session is missing project directory')

    if (isLocalWorkspaceType(meta.workspaceType)) {
      return createLocalWorkspaceInfo({
        projectDir: desiredProjectDir,
        repoRoot: meta.repoRoot || desiredProjectDir,
        workspaceDir: meta.workspaceDir || desiredProjectDir,
        cwdSubpath: desiredCwdSubpath,
      })
    }

    let info: WorkspaceInfo | null = null
    try {
      info = await get()._resolveWorkspace(desiredWorkspaceDir, desiredCwdSubpath)
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      throw new Error(`${msg || 'Failed to resolve git workspace'} (dir: ${desiredWorkspaceDir})`, {
        cause: err,
      })
    }
    if (!info) throw new Error(`Failed to resolve git workspace (dir: ${desiredWorkspaceDir})`)

    const desiredBranch = typeof meta.branch === 'string' ? meta.branch.trim() : ''
    if (desiredBranch && desiredBranch !== info.branch) {
      const switched = await droid.switchWorkspace({
        workspaceDir: info.workspaceDir || desiredWorkspaceDir,
        branch: desiredBranch,
        cwdSubpath: desiredCwdSubpath || info.cwdSubpath,
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
      workspaceDir: aligned.workspaceDir,
      cwdSubpath: aligned.cwdSubpath,
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
        workspaceDir: aligned.workspaceDir,
        cwdSubpath: aligned.cwdSubpath,
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
      const startupSessionData =
        typeof (droid as any)?.loadSessionStored === 'function'
          ? (droid as any).loadSessionStored(sessionId)
          : droid.loadSession(sessionId)
      const data = await startupSessionData
      get()._setSessionBuffers((prev) => {
        const next = new Map(prev)
        next.set(
          sessionId,
          buildRestoredSessionBuffer({
            projectDir: aligned.projectDir,
            workspace: {
              repoRoot: aligned.repoRoot,
              workspaceDir: aligned.workspaceDir,
              cwdSubpath: aligned.cwdSubpath,
              branch: aligned.branch,
              workspaceType: aligned.workspaceType,
              baseBranch:
                (data as any)?.baseBranch || selectedMeta.baseBranch || aligned.baseBranch,
            },
            meta: selectedMeta,
            data,
            missionModelSettings: get().missionModelSettings,
          }),
        )
        return next
      })

      void (async () => {
        const liveData = await droid.loadSession(sessionId)
        const latest = get()
        if (latest.activeSessionId !== sessionId) return
        latest._setSessionBuffers((prev) => {
          const current = prev.get(sessionId)
          const next = new Map(prev)
          next.set(
            sessionId,
            buildRestoredSessionBuffer({
              projectDir: aligned.projectDir,
              workspace: {
                repoRoot: (liveData as any)?.repoRoot || current?.repoRoot || aligned.repoRoot,
                workspaceDir:
                  (liveData as any)?.workspaceDir || current?.workspaceDir || aligned.workspaceDir,
                cwdSubpath:
                  (liveData as any)?.cwdSubpath || current?.cwdSubpath || aligned.cwdSubpath,
                branch: (liveData as any)?.branch || current?.branch || aligned.branch,
                workspaceType:
                  (liveData as any)?.workspaceType ||
                  current?.workspaceType ||
                  aligned.workspaceType,
                baseBranch:
                  (liveData as any)?.baseBranch ||
                  current?.baseBranch ||
                  selectedMeta.baseBranch ||
                  aligned.baseBranch,
              },
              meta: selectedMeta,
              data: liveData,
              missionModelSettings: get().missionModelSettings,
            }),
          )
          return next
        })
      })()
    }
  },

  // --- Navigation ---
  handleNewSession: (repoRoot) => {
    void (async () => {
      const s = get()
      if (!s._initialLoadDone) return
      s.clearWorkspaceError()
      try {
        const sourceDir = String(repoRoot || s.activeProjectDir || '').trim()
        if (!sourceDir) throw new Error('No project directory available')
        const targetRepoRoot = String(repoRoot || sourceDir).trim()
        const targetProject = s.projects.find((project) => project.dir === targetRepoRoot)

        const resolved = await s._resolveWorkspace(sourceDir).catch(() => null)
        if (!resolved) {
          if (!isLocalWorkspaceType(targetProject?.workspaceType)) {
            throw new Error('Not a git repository')
          }

          const localWorkspace = createLocalWorkspaceInfo({
            projectDir: sourceDir,
            repoRoot: targetRepoRoot,
            workspaceDir: sourceDir,
          })
          set({ pendingNewSession: createPendingSessionFromWorkspace(localWorkspace) })
          return
        }
        const commonRepoRoot = resolved.repoRoot || repoRoot || sourceDir
        if (!commonRepoRoot) throw new Error('Missing repo root')

        await s._ensureProjectSettingsInitialized({
          repoRoot: commonRepoRoot,
          hintBranch: resolved.branch,
        })

        set({ pendingNewSession: createPendingSessionFromWorkspace(resolved) })
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        set({ workspaceError: msg || 'Failed to create session' })
      }
    })()
  },

  handleSetProjectDir: (dir) => {
    if (!dir) return
    void (async () => {
      const s = get()
      const info = await s._resolveWorkspace(dir).catch(() => null)
      const workspaceInfo =
        info || createLocalWorkspaceInfo({ projectDir: dir, repoRoot: dir, workspaceDir: dir })
      const repoRoot = workspaceInfo.repoRoot

      get()._setProjects((prev) => {
        const existing = prev.find((p) => p.dir === repoRoot)
        if (existing) {
          if (existing.workspaceType === workspaceInfo.workspaceType) return prev
          return prev.map((project) =>
            project.dir === repoRoot
              ? { ...project, workspaceType: project.workspaceType || workspaceInfo.workspaceType }
              : project,
          )
        }
        const name = repoRoot.split(/[\\/]/).pop() || repoRoot
        return [
          ...prev,
          { dir: repoRoot, name, workspaceType: workspaceInfo.workspaceType, sessions: [] },
        ]
      })

      if (supportsGitWorkspace(workspaceInfo.workspaceType)) {
        await s._ensureProjectSettingsInitialized({ repoRoot, hintBranch: workspaceInfo.branch })
      }

      set({ pendingNewSession: createPendingSessionFromWorkspace(workspaceInfo) })
    })()
  },

  handleAddProject: async () => {
    if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
      ;(droid as any).appendDiagnosticsEvent({
        sessionId: get().activeSessionId || null,
        event: 'ui.project_picker.open.start',
        level: 'info',
      })
    }
    const timeoutResult = '__dialog_timeout__'
    const dir = await Promise.race([
      droid.openDirectory(),
      new Promise<string | null | typeof timeoutResult>((resolve) => {
        setTimeout(() => resolve(timeoutResult), 10_000)
      }),
    ])
    if (dir === timeoutResult) {
      if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
        ;(droid as any).appendDiagnosticsEvent({
          sessionId: get().activeSessionId || null,
          event: 'ui.project_picker.open.timeout',
          level: 'warn',
        })
      }
      set({ workspaceError: 'Directory picker did not respond. Please try again.' })
      return
    }
    if (!dir) return
    if (typeof (droid as any)?.appendDiagnosticsEvent === 'function') {
      ;(droid as any).appendDiagnosticsEvent({
        sessionId: get().activeSessionId || null,
        event: 'ui.project_picker.open.success',
        level: 'info',
        data: { dir },
      })
    }
    get().clearWorkspaceError()
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
      const protocol = getSessionProtocol(buf || meta)
      const missionBaseSessionId = getMissionBaseSessionId(buf || meta, sessionId)
      void droid.saveSession({
        id: sessionId,
        projectDir: buf.projectDir,
        workspaceDir: buf.workspaceDir,
        cwdSubpath: buf.cwdSubpath,
        repoRoot: buf.repoRoot,
        branch: buf.branch,
        workspaceType: buf.workspaceType,
        baseBranch: buf.baseBranch,
        model: buf.model || DEFAULT_MODEL,
        autoLevel: buf.autoLevel || DEFAULT_AUTO_LEVEL,
        missionDir: buf.missionDir,
        missionBaseSessionId,
        isMission: protocol.isMission,
        sessionKind: protocol.sessionKind,
        interactionMode: protocol.interactionMode,
        autonomyLevel: protocol.autonomyLevel,
        decompSessionType: protocol.decompSessionType,
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
        const worktreeDir = String(sessionMeta.workspaceDir || sessionMeta.projectDir || '').trim()
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
        ? allRemaining.filter(
            (x) => (x.repoRoot || x.workspaceDir || x.projectDir) === deletedRepoRoot,
          )
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
            workspaceDir: aligned.workspaceDir,
            cwdSubpath: aligned.cwdSubpath,
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
            const startupSessionData =
              typeof (droid as any)?.loadSessionStored === 'function'
                ? (droid as any).loadSessionStored(pickFallback.id)
                : droid.loadSession(pickFallback.id)
            const data = await startupSessionData
            const loaded = (data?.messages as ChatMessage[]) ?? []
            get()._setSessionBuffers((prev) => {
              const next = new Map(prev)
              const restored = buildRestoredSessionBuffer({
                projectDir: aligned.projectDir,
                workspace: {
                  repoRoot: aligned.repoRoot,
                  workspaceDir: aligned.workspaceDir,
                  cwdSubpath: aligned.cwdSubpath,
                  branch: aligned.branch,
                  workspaceType: aligned.workspaceType,
                  baseBranch:
                    (data as any)?.baseBranch || pickFallback.baseBranch || aligned.baseBranch,
                },
                meta: pickFallback,
                data: data ? { ...data, messages: loaded } : null,
                missionModelSettings: get().missionModelSettings,
              })
              next.set(pickFallback.id, restored)
              return next
            })

            void (async () => {
              const liveData = await droid.loadSession(pickFallback.id)
              const latest = get()
              if (latest.activeSessionId !== pickFallback.id) return
              latest._setSessionBuffers((prev) => {
                const current = prev.get(pickFallback.id)
                const next = new Map(prev)
                next.set(
                  pickFallback.id,
                  buildRestoredSessionBuffer({
                    projectDir: aligned.projectDir,
                    workspace: {
                      repoRoot:
                        (liveData as any)?.repoRoot || current?.repoRoot || aligned.repoRoot,
                      workspaceDir:
                        (liveData as any)?.workspaceDir ||
                        current?.workspaceDir ||
                        aligned.workspaceDir,
                      cwdSubpath:
                        (liveData as any)?.cwdSubpath || current?.cwdSubpath || aligned.cwdSubpath,
                      branch: (liveData as any)?.branch || current?.branch || aligned.branch,
                      workspaceType:
                        (liveData as any)?.workspaceType ||
                        current?.workspaceType ||
                        aligned.workspaceType,
                      baseBranch:
                        (liveData as any)?.baseBranch ||
                        current?.baseBranch ||
                        pickFallback.baseBranch ||
                        aligned.baseBranch,
                    },
                    meta: pickFallback,
                    data: liveData,
                    missionModelSettings: get().missionModelSettings,
                  }),
                )
                return next
              })
            })()
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
          const wtProjectDir = String(session.workspaceDir || session.projectDir || '').trim()
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
      if (
        activeSession &&
        (activeSession.repoRoot || activeSession.workspaceDir || activeSession.projectDir) ===
          repoRoot
      ) {
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

  handleRenameProject: (repoRoot, displayName) => {
    if (!repoRoot) return
    get()._setProjects((prev) => renameProject(prev, repoRoot, displayName))
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
export const useSessionRunning = (sessionId: string) =>
  useAppStore((s) => Boolean(s.sessionBuffers.get(sessionId)?.isRunning))
export const useIsAnyRunning = () =>
  useAppStore((s) => Array.from(s.sessionBuffers.values()).some((b) => Boolean(b?.isRunning)))
export const useWorkingState = () => useAppStore((s) => selectActiveBuffer(s)?.workingState)
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
export const useCommitMessageReasoningEffort = () =>
  useAppStore((s) => s.commitMessageReasoningEffort)
export const useLanAccessEnabled = () => useAppStore((s) => s.lanAccessEnabled)
export const useTelemetryEnabled = () => useAppStore((s) => s.telemetryEnabled)
export const useMissionModelSettings = () => useAppStore((s) => s.missionModelSettings)
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
export const useUpdateAvailable = () => useAppStore((s) => s.updateAvailable)
export const useUpdateDownloading = () => useAppStore((s) => s.updateDownloading)
export const useUpdateDownloadProgress = () => useAppStore((s) => s.updateDownloadProgress)
export const useUpdateReady = () => useAppStore((s) => s.updateReady)

// --- Session attention selector ---
// Returns true when a session has pending permission or ask-user requests.
export function getSessionNeedsAttention(sessionId: string): boolean {
  const buf = useAppStore.getState().sessionBuffers.get(sessionId)
  if (!buf) return false
  return (
    (buf.pendingPermissionRequests != null && buf.pendingPermissionRequests.length > 0) ||
    (buf.pendingAskUserRequests != null && buf.pendingAskUserRequests.length > 0)
  )
}

export const useSessionNeedsAttention = (sessionId: string) =>
  useAppStore((s) => {
    const buf = s.sessionBuffers.get(sessionId)
    if (!buf) return false
    return (
      (buf.pendingPermissionRequests != null && buf.pendingPermissionRequests.length > 0) ||
      (buf.pendingAskUserRequests != null && buf.pendingAskUserRequests.length > 0)
    )
  })

// Actions are stable function references on the store object and never change,
// so we read them directly via getState() without subscribing.
export function useActions() {
  return useAppStore.getState()
}
