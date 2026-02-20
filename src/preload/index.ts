import { contextBridge, ipcRenderer } from 'electron'
import type { IpcRendererEvent } from 'electron'
import type { DroidClientAPI, SaveSessionRequest } from '../shared/protocol'

const droidAPI: DroidClientAPI = {
  getVersion: () => ipcRenderer.invoke('droid:version'),
  exec: (params) => ipcRenderer.send('droid:exec', params),
  cancel: (params) => ipcRenderer.send('droid:cancel', params),
  setActiveSession: () => {},
  updateSessionSettings: (params) => ipcRenderer.invoke('droid:updateSessionSettings', params),

  createSession: (params) => ipcRenderer.invoke('session:create', params),
  restartSessionWithActiveKey: (params) => ipcRenderer.invoke('session:restart', params),
  runSetupScript: (params) => ipcRenderer.invoke('session:setup:run', params),
  cancelSetupScript: (params) => ipcRenderer.send('session:setup:cancel', params),
  onSetupScriptEvent: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { event: any; sessionId: string | null }) => callback(payload as any)
    ipcRenderer.on('session:setup-event', handler)
    return () => ipcRenderer.removeListener('session:setup-event', handler)
  },

  listSlashCommands: () => ipcRenderer.invoke('slash:list'),
  resolveSlashCommand: (params) => ipcRenderer.invoke('slash:resolve', params),
  listSkills: () => ipcRenderer.invoke('skills:list'),

  onRpcNotification: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { message: any; sessionId: string | null }) => callback(payload as any)
    ipcRenderer.on('droid:rpc-notification', handler)
    return () => ipcRenderer.removeListener('droid:rpc-notification', handler)
  },
  onRpcRequest: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { message: any; sessionId: string | null }) => callback(payload as any)
    ipcRenderer.on('droid:rpc-request', handler)
    return () => ipcRenderer.removeListener('droid:rpc-request', handler)
  },
  onTurnEnd: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { code: number; sessionId: string | null }) => callback(payload)
    ipcRenderer.on('droid:turn-end', handler)
    return () => ipcRenderer.removeListener('droid:turn-end', handler)
  },
  onDebug: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { message: string; sessionId: string | null }) => callback(payload)
    ipcRenderer.on('droid:debug', handler)
    return () => ipcRenderer.removeListener('droid:debug', handler)
  },

  onSessionIdReplaced: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { oldSessionId: string; newSessionId: string; reason: string }) => callback(payload)
    ipcRenderer.on('droid:session-id-replaced', handler)
    return () => ipcRenderer.removeListener('droid:session-id-replaced', handler)
  },

  respondPermission: (params) => ipcRenderer.send('droid:permission-response', params),
  respondAskUser: (params) => ipcRenderer.send('droid:askuser-response', params),
  onStdout: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { data: string; sessionId: string | null }) => callback(payload)
    ipcRenderer.on('droid:stdout', handler)
    return () => ipcRenderer.removeListener('droid:stdout', handler)
  },
  onStderr: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { data: string; sessionId: string | null }) => callback(payload)
    ipcRenderer.on('droid:stderr', handler)
    return () => ipcRenderer.removeListener('droid:stderr', handler)
  },
  onError: (callback) => {
    const handler = (_event: IpcRendererEvent, payload: { message: string; sessionId: string | null }) => callback(payload)
    ipcRenderer.on('droid:error', handler)
    return () => ipcRenderer.removeListener('droid:error', handler)
  },

  setApiKey: (apiKey) => ipcRenderer.send('droid:setApiKey', apiKey),
  getApiKey: () => ipcRenderer.invoke('droid:getApiKey'),
  listKeys: () => ipcRenderer.invoke('keys:list'),
  addKeys: (keys: string[]) => ipcRenderer.invoke('keys:add', { keys }),
  removeKeyByIndex: (index: number) => ipcRenderer.invoke('keys:remove', { index }),
  updateKeyNote: (index: number, note: string) => ipcRenderer.invoke('keys:note', { index, note }),
  refreshKeys: () => ipcRenderer.invoke('keys:refresh'),
  getActiveKeyInfo: () => ipcRenderer.invoke('keys:active'),
  setTraceChainEnabled: (enabled) => ipcRenderer.send('appState:setTraceChainEnabled', Boolean(enabled)),
  setShowDebugTrace: (enabled) => ipcRenderer.send('appState:setShowDebugTrace', Boolean(enabled)),
  setLocalDiagnosticsEnabled: (enabled) => ipcRenderer.send('appState:setLocalDiagnosticsEnabled', Boolean(enabled)),
  setLocalDiagnosticsRetention: (params) => ipcRenderer.send('appState:setLocalDiagnosticsRetention', params),
  setLanAccessEnabled: (enabled) => ipcRenderer.send('appState:setLanAccessEnabled', Boolean(enabled)),
  appendDiagnosticsEvent: (params) => ipcRenderer.send('diagnostics:event', params),
  getDiagnosticsDir: () => ipcRenderer.invoke('diagnostics:getDir'),
  exportDiagnostics: (params) => ipcRenderer.invoke('diagnostics:export', params),
  openPath: (path) => ipcRenderer.invoke('diagnostics:openPath', path),
  openInEditor: (params) => ipcRenderer.invoke('shell:openInEditor', params),
  openWithEditor: (params) => ipcRenderer.invoke('shell:openWithEditor', params),
  detectEditors: () => ipcRenderer.invoke('shell:detectEditors'),
  openDirectory: () => ipcRenderer.invoke('dialog:openDirectory'),
  openFile: () => ipcRenderer.invoke('dialog:openFile'),
  saveAttachments: (params) => ipcRenderer.invoke('attachment:save', params),
  saveClipboardImage: (params) => ipcRenderer.invoke('attachment:saveClipboardImage', params),
  setProjectDir: (dir) => ipcRenderer.send('project:setDir', dir),
  getProjectDir: () => ipcRenderer.invoke('project:getDir'),
  saveSession: (req: SaveSessionRequest) => ipcRenderer.invoke('session:save', req),
  loadSession: (id) => ipcRenderer.invoke('session:load', id),
  clearSession: (params) => ipcRenderer.invoke('session:clear', params),
  listSessions: () => ipcRenderer.invoke('session:list'),
  deleteSession: (id) => ipcRenderer.invoke('session:delete', id),
  loadAppState: () => ipcRenderer.invoke('appState:load'),
  saveProjects: (projects) => ipcRenderer.send('appState:saveProjects', projects),
  updateProjectSettings: (params) => ipcRenderer.invoke('appState:updateProjectSettings', params),
  setCommitMessageModelId: (modelId) => ipcRenderer.send('appState:setCommitMessageModelId', modelId),
  getGitStatus: (params) => ipcRenderer.invoke('git:status', params),
  getGitBranch: (params) => ipcRenderer.invoke('git:branch', params),
  listGitBranches: (params) => ipcRenderer.invoke('git:list-branches', params),
  listGitWorktreeBranchesInUse: (params) => ipcRenderer.invoke('git:worktree-branches-in-use', params),
  getWorkspaceInfo: (params) => ipcRenderer.invoke('git:workspace-info', params),
  switchWorkspace: (params) => ipcRenderer.invoke('git:switch-workspace', params),
  createWorkspace: (params) => ipcRenderer.invoke('git:create-workspace', params),
  removeWorktree: (params) => ipcRenderer.invoke('git:remove-worktree', params),
  pushBranch: (params) => ipcRenderer.invoke('git:push-branch', params),
  detectGitTools: (params) => ipcRenderer.invoke('git:detect-tools', params),
  generateCommitMeta: (params) => ipcRenderer.invoke('git:generate-commit-meta', params),
  commitWorkflow: (params) => ipcRenderer.invoke('git:commit-workflow', params),
  onCommitWorkflowProgress: (callback) => {
    const handler = (_event: IpcRendererEvent, progress: any) => callback(progress)
    ipcRenderer.on('git:commit-workflow-progress', handler)
    return () => ipcRenderer.removeListener('git:commit-workflow-progress', handler)
  },
  getCustomModels: () => ipcRenderer.invoke('factory:getCustomModels'),
}

contextBridge.exposeInMainWorld('droid', droidAPI)
contextBridge.exposeInMainWorld('__DROID_TRACE_CHAIN', process.env['DROID_TRACE_CHAIN'] || '')
