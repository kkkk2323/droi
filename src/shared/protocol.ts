// Shared, isomorphic types for Droid App (Electron + Browser modes)

// === stream-jsonrpc protocol (JSON-RPC over JSONL) ===

export const JSONRPC_VERSION = '2.0' as const
export const FACTORY_API_VERSION = '1.0.0' as const

export interface JsonRpcMeta {
  traceparent?: string
  tracestate?: string
}

export interface JsonRpcBase {
  jsonrpc: typeof JSONRPC_VERSION
  factoryApiVersion: typeof FACTORY_API_VERSION
  _meta?: JsonRpcMeta
}

export interface JsonRpcRequest extends JsonRpcBase {
  type: 'request'
  id: string
  method: string
  params?: unknown
}

export interface JsonRpcError {
  code: number
  message: string
  data?: unknown
}

export interface JsonRpcResponse extends JsonRpcBase {
  type: 'response'
  id: string | null
  result?: unknown
  error?: JsonRpcError
}

export interface JsonRpcNotification extends JsonRpcBase {
  type: 'notification'
  method: string
  params?: unknown
}

export type JsonRpcMessage = JsonRpcRequest | JsonRpcResponse | JsonRpcNotification

export type DroidInteractionMode = 'spec' | 'auto'

// Tool permission level (equivalent to `droid exec --auto <level>` semantics).
export type DroidAutonomyLevel = 'off' | 'low' | 'medium' | 'high'

export type DroidPermissionOption =
  | 'proceed_once'
  | 'proceed_always'
  | 'proceed_auto_run'
  | 'proceed_auto_run_low'
  | 'proceed_auto_run_medium'
  | 'proceed_auto_run_high'
  | 'proceed_edit'
  | 'cancel'

export interface DroidSessionNotificationAssistantTextDelta {
  type: 'assistant_text_delta'
  messageId: string
  blockIndex: number
  textDelta: string
}

export interface DroidSessionNotificationThinkingTextDelta {
  type: 'thinking_text_delta'
  messageId: string
  blockIndex: number
  textDelta: string
}

export interface DroidSessionNotificationToolUse {
  type: 'tool_use'
  id: string
  name: string
  input: Record<string, unknown>
  thoughtSignature?: string
}

export interface DroidSessionNotificationToolResult {
  type: 'tool_result'
  toolUseId: string
  content?: unknown
  isError?: boolean
}

export interface DroidSessionNotificationToolProgressUpdate {
  type: 'tool_progress_update'
  toolUseId: string
  toolName: string
  update: unknown
}

export interface DroidSessionNotificationWorkingStateChanged {
  type: 'droid_working_state_changed'
  newState: string
}

export interface DroidSessionNotificationError {
  type: 'error'
  message: string
  errorType?: string
  timestamp?: string
  error?: { name: string; message: string }
}

export interface DroidSessionNotificationPermissionResolved {
  type: 'permission_resolved'
  requestId: string
  toolUseIds: string[]
  selectedOption: DroidPermissionOption
}

export type DroidSessionNotification =
  | DroidSessionNotificationAssistantTextDelta
  | DroidSessionNotificationThinkingTextDelta
  | DroidSessionNotificationToolUse
  | DroidSessionNotificationToolResult
  | DroidSessionNotificationToolProgressUpdate
  | DroidSessionNotificationWorkingStateChanged
  | DroidSessionNotificationError
  | DroidSessionNotificationPermissionResolved
  | ({ type: string } & Record<string, unknown>)

export interface DroidSessionNotificationEnvelope {
  notification: DroidSessionNotification
}

// === UI message blocks ===

export interface TextBlock {
  kind: 'text'
  content: string
}

export interface ToolCallBlock {
  kind: 'tool_call'
  callId: string
  toolName: string
  parameters: Record<string, unknown>
  progress?: string
  result?: string
  isError?: boolean
}

export interface AttachmentBlock {
  kind: 'attachment'
  name: string
  path: string
}

export interface CommandBlock {
  kind: 'command'
  name: string
}

export interface SkillBlock {
  kind: 'skill'
  name: string
}

export interface ThinkingBlock {
  kind: 'thinking'
  content: string
}

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant' | 'error'
  blocks: (
    | TextBlock
    | ToolCallBlock
    | AttachmentBlock
    | CommandBlock
    | SkillBlock
    | ThinkingBlock
  )[]
  timestamp: number
  endTimestamp?: number
}

// === Session / Project ===

export interface SessionMeta {
  id: string
  projectDir: string
  repoRoot?: string
  branch?: string
  workspaceType?: WorkspaceType
  title: string
  savedAt: number
  messageCount: number
  model: string
  autoLevel: string
  reasoningEffort?: string
  apiKeyFingerprint?: string
  lastMessageAt?: number
  baseBranch?: string
  pinned?: boolean
}

export interface Project {
  dir: string
  name: string
  sessions: SessionMeta[]
}

export interface ProjectSettings {
  // Base branch used when creating a new session worktree (fork point).
  baseBranch?: string
  // Prefix used for session branches, e.g. "droi" -> "droi/brave-otter-k3f9".
  worktreePrefix?: string
  // Optional shell script executed right after creating a new session.
  setupScript?: string
}

// === Multi-Key Management ===

export interface ApiKeyEntry {
  key: string
  note?: string
  addedAt: number
}

export interface ApiKeyUsage {
  used: number | null
  total: number | null
  expires: string | null
  expiresTs: number | null
  lastCheckedAt: number
  error?: string
}

export interface PersistedAppStateV1 {
  version: 1
  apiKey?: string
  projects?: Array<{ dir: string; name: string }>
  activeProjectDir?: string
  traceChainEnabled?: boolean
}

export interface PersistedAppStateV2 {
  version: 2
  machineId: string
  apiKey?: string
  apiKeys?: ApiKeyEntry[]
  projects?: Array<{ dir: string; name: string }>
  activeProjectDir?: string
  traceChainEnabled?: boolean
  showDebugTrace?: boolean
  debugTraceMaxLines?: number
  localDiagnosticsEnabled?: boolean
  localDiagnosticsRetentionDays?: number
  localDiagnosticsMaxTotalMb?: number
  // Model used for generating commit messages / PR metadata.
  commitMessageModelId?: string
  lanAccessEnabled?: boolean
  projectSettings?: Record<string, ProjectSettings>
}

export type PersistedAppState = PersistedAppStateV1 | PersistedAppStateV2

export interface SaveSessionRequest {
  id: string
  projectDir: string
  repoRoot?: string
  branch?: string
  workspaceType?: WorkspaceType
  baseBranch?: string
  model: string
  autoLevel: string
  reasoningEffort?: string
  apiKeyFingerprint?: string
  pinned?: boolean
  messages: ChatMessage[]
}

export interface LoadSessionResponse {
  id: string
  projectDir: string
  repoRoot?: string
  branch?: string
  workspaceType?: WorkspaceType
  baseBranch?: string
  model: string
  autoLevel: string
  reasoningEffort?: string
  apiKeyFingerprint?: string
  pinned?: boolean
  title: string
  savedAt: number
  messages: ChatMessage[]
  lastMessageAt?: number
}

// === Client API (renderer-facing) ===

export interface SlashCommandDef {
  name: string
  description?: string
  argumentHint?: string
  scope: 'project' | 'user'
  filePath: string
}

export interface SlashResolveResult {
  matched: boolean
  expandedText: string
  command?: SlashCommandDef
  error?: string
}

export interface SkillDef {
  name: string
  description?: string
  scope: 'project' | 'user'
  filePath: string
  enabled?: boolean
  userInvocable?: boolean
  version?: string
  location?: 'personal' | 'project'
}

export interface DroidClientAPI {
  getVersion: () => Promise<string>
  getAppVersion: () => Promise<string>
  exec: (params: {
    prompt: string
    sessionId?: string | null
    modelId?: string
    autoLevel?: string
    reasoningEffort?: string
  }) => void
  cancel: (params: { sessionId: string | null }) => void
  setActiveSession: (params: { sessionId: string | null }) => void
  updateSessionSettings: (params: {
    sessionId: string
    modelId?: string
    autoLevel?: string
    reasoningEffort?: string
  }) => Promise<{ ok: true }>

  createSession: (params: {
    cwd: string
    modelId?: string
    autoLevel?: string
    reasoningEffort?: string
  }) => Promise<{ sessionId: string }>

  restartSessionWithActiveKey: (params: {
    sessionId: string
  }) => Promise<{ ok: true; apiKeyFingerprint: string }>

  runSetupScript: (params: {
    sessionId: string
    projectDir: string
    script: string
  }) => Promise<{ ok: true }>
  cancelSetupScript: (params: { sessionId: string }) => void
  onSetupScriptEvent: (
    callback: (payload: { event: SetupScriptEvent; sessionId: string | null }) => void,
  ) => () => void

  listSlashCommands: () => Promise<SlashCommandDef[]>
  resolveSlashCommand: (params: { text: string }) => Promise<SlashResolveResult>
  listSkills: () => Promise<SkillDef[]>

  onRpcNotification: (
    callback: (payload: { message: JsonRpcNotification; sessionId: string | null }) => void,
  ) => () => void
  onRpcRequest: (
    callback: (payload: { message: JsonRpcRequest; sessionId: string | null }) => void,
  ) => () => void
  onTurnEnd: (callback: (payload: { code: number; sessionId: string | null }) => void) => () => void
  onDebug: (
    callback: (payload: { message: string; sessionId: string | null }) => void,
  ) => () => void

  onSessionIdReplaced: (
    callback: (payload: { oldSessionId: string; newSessionId: string; reason: string }) => void,
  ) => () => void

  respondPermission: (params: {
    sessionId: string
    requestId: string
    selectedOption: DroidPermissionOption
    selectedExitSpecModeOptionIndex?: number
    exitSpecModeComment?: string
  }) => void
  addUserMessage: (params: { sessionId: string; text: string }) => Promise<void>
  respondAskUser: (params: {
    sessionId: string
    requestId: string
    cancelled?: boolean
    answers: Array<{ index: number; question: string; answer: string }>
  }) => void
  onStdout: (callback: (payload: { data: string; sessionId: string | null }) => void) => () => void
  onStderr: (callback: (payload: { data: string; sessionId: string | null }) => void) => () => void
  onError: (
    callback: (payload: { message: string; sessionId: string | null }) => void,
  ) => () => void

  setApiKey: (apiKey: string) => void
  getApiKey: () => Promise<string>

  listKeys: () => Promise<
    Array<{
      key: string
      note: string
      addedAt: number
      index: number
      isActive: boolean
      usage: ApiKeyUsage | null
    }>
  >
  addKeys: (keys: string[]) => Promise<{ added: number; duplicates: number }>
  removeKeyByIndex: (index: number) => Promise<void>
  updateKeyNote: (index: number, note: string) => Promise<void>
  refreshKeys: () => Promise<
    Array<{
      key: string
      note: string
      addedAt: number
      index: number
      isActive?: boolean
      usage: ApiKeyUsage | null
    }>
  >
  getActiveKeyInfo: () => Promise<{ key: string; apiKeyFingerprint: string }>
  setTraceChainEnabled: (enabled: boolean) => void
  setShowDebugTrace: (enabled: boolean) => void
  setDebugTraceMaxLines: (maxLines: number | null) => void
  setLocalDiagnosticsEnabled: (enabled: boolean) => void
  setLocalDiagnosticsRetention: (params: { retentionDays: number; maxTotalMb: number }) => void
  setLanAccessEnabled: (enabled: boolean) => void
  appendDiagnosticsEvent: (params: {
    sessionId?: string | null
    event: string
    level?: string
    data?: unknown
    correlation?: Record<string, unknown>
  }) => void
  getDiagnosticsDir: () => Promise<string>
  exportDiagnostics: (params: {
    sessionId?: string | null
    debugTraceText?: string
  }) => Promise<{ path: string }>
  openPath: (path: string) => Promise<{ ok: true }>

  openInEditor: (params: { dir: string }) => Promise<void>
  openWithEditor: (params: { dir: string; editorId: string }) => Promise<void>
  detectEditors: () => Promise<EditorInfo[]>
  openDirectory: () => Promise<string | null>
  openFile: () => Promise<string[] | null>
  saveAttachments: (params: {
    sourcePaths: string[]
    projectDir: string
  }) => Promise<Array<{ name: string; path: string }>>
  saveClipboardImage: (params: {
    data: number[]
    mimeType: string
    projectDir: string
    fileName?: string
  }) => Promise<{ name: string; path: string } | null>
  setProjectDir: (dir: string | null) => void
  getProjectDir: () => Promise<string>

  saveSession: (req: SaveSessionRequest) => Promise<SessionMeta | null>
  loadSession: (id: string) => Promise<LoadSessionResponse | null>
  clearSession: (params: { id: string }) => Promise<SessionMeta | null>
  listSessions: () => Promise<SessionMeta[]>
  deleteSession: (id: string) => Promise<boolean>

  loadAppState: () => Promise<PersistedAppState>
  saveProjects: (projects: Array<{ dir: string; name: string }>) => void
  updateProjectSettings: (params: {
    repoRoot: string
    settings: ProjectSettings
  }) => Promise<PersistedAppState>

  setCommitMessageModelId: (modelId: string) => void

  getGitStatus: (params: { projectDir: string }) => Promise<GitStatusFile[]>
  getGitBranch: (params: { projectDir: string }) => Promise<string>
  listGitBranches: (params: { projectDir: string }) => Promise<string[]>
  listGitWorktreeBranchesInUse: (params: {
    repoRoot: string
  }) => Promise<Array<{ branch: string; worktreeDir: string }>>
  getWorkspaceInfo: (params: { projectDir: string }) => Promise<WorkspaceInfo | null>
  switchWorkspace: (params: { projectDir: string; branch: string }) => Promise<WorkspaceInfo | null>
  createWorkspace: (params: WorkspaceCreateParams) => Promise<WorkspaceInfo | null>
  removeWorktree: (params: {
    repoRoot: string
    worktreeDir: string
    force?: boolean
    deleteBranch?: boolean
    branch?: string
  }) => Promise<RemoveWorktreeResult>
  pushBranch: (params: {
    projectDir: string
    remote?: string
    branch?: string
  }) => Promise<PushBranchResult>
  detectGitTools: (params: { projectDir: string }) => Promise<GitToolsInfo>
  generateCommitMeta: (params: GenerateCommitMetaRequest) => Promise<GenerateCommitMetaResult>
  commitWorkflow: (params: CommitWorkflowRequest) => Promise<CommitWorkflowResult>
  onCommitWorkflowProgress: (callback: (progress: WorkflowStepProgress) => void) => () => void
  getCustomModels: () => Promise<CustomModelDef[]>

  // Updater
  checkForUpdate: () => Promise<UpdateCheckResult>
  installUpdate: () => Promise<void>
  relaunchApp: () => Promise<void>
  onUpdateProgress: (callback: (progress: UpdateDownloadProgress) => void) => () => void
}

export interface EditorInfo {
  id: string
  name: string
  command?: string
}

export interface GitStatusFile {
  status: string
  path: string
  additions: number
  deletions: number
}

export type WorkspaceType = 'branch' | 'worktree'

export interface WorkspaceInfo {
  repoRoot: string
  projectDir: string
  branch: string
  workspaceType: WorkspaceType
  baseBranch?: string
}

export interface WorkspaceCreateParams {
  projectDir: string
  mode: WorkspaceType
  branch: string
  baseBranch?: string
  useExistingBranch?: boolean
}

export interface RemoveWorktreeResult {
  ok: true
}

export interface PushBranchResult {
  ok: true
  remote: string
  branch: string
}

export type PrTool = 'gh' | 'flow'

export interface GitToolsInfo {
  hasGh: boolean
  hasFlow: boolean
  originHost?: string
  prTool: PrTool | null
  prDisabledReason?: string
}

export interface GenerateCommitMetaRequest {
  projectDir: string
  includeUnstaged: boolean
  wantPrMeta?: boolean
  prBaseBranch?: string
}

export interface GenerateCommitMetaResult {
  commitMessage: string
  prTitle?: string
  prBody?: string
  modelId: string
}

export type CommitWorkflow = 'commit' | 'commit_push' | 'commit_push_pr'

export type WorkflowStepName = 'stage' | 'commit' | 'merge' | 'push' | 'create_pr'
export type WorkflowStepStatus = 'pending' | 'running' | 'done' | 'error'

export interface WorkflowStepProgress {
  step: WorkflowStepName
  status: WorkflowStepStatus
  detail?: string
}

export interface CommitWorkflowRequest {
  projectDir: string
  includeUnstaged: boolean
  commitMessage: string
  workflow: CommitWorkflow
  prBaseBranch?: string
  prTitle?: string
  prBody?: string
  mergeEnabled?: boolean
  mergeBranch?: string
}

export interface CommitWorkflowResult {
  ok: true
  branch: string
  commitHash: string
  remote?: string
  prUrl?: string
}

export interface CustomModelDef {
  id: string
  displayName: string
  model: string
  provider: string
}

// === Updater ===

export interface UpdateCheckResult {
  available: boolean
  version?: string
  currentVersion?: string
}

export interface UpdateDownloadProgress {
  percent: number
  transferred: number
  total: number
}

export type SetupScriptStatus = 'idle' | 'running' | 'failed' | 'completed' | 'skipped'

export interface SetupScriptStartedEvent {
  type: 'started'
  sessionId: string
  projectDir: string
  script: string
}

export interface SetupScriptOutputEvent {
  type: 'output'
  sessionId: string
  stream: 'stdout' | 'stderr'
  data: string
}

export interface SetupScriptFinishedEvent {
  type: 'finished'
  sessionId: string
  success: boolean
  exitCode: number | null
  signal: string | null
  error?: string
}

export type SetupScriptEvent =
  | SetupScriptStartedEvent
  | SetupScriptOutputEvent
  | SetupScriptFinishedEvent
