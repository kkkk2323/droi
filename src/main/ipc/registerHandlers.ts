import { ipcMain, dialog, shell, app, type BrowserWindow } from 'electron'
import { copyFile, mkdir, readFile, stat, writeFile } from 'fs/promises'
import { basename, extname, join } from 'path'
import { homedir } from 'os'
import { execFile } from 'child_process'
import { createHash } from 'crypto'
import { getDroidVersion, DroidExecManager } from '../../backend/droid/droidExecRunner'
import { LocalDiagnostics } from '../../backend/diagnostics/localDiagnostics'
import {
  formatNotificationTrace,
  isTraceChainEnabled,
  setTraceChainEnabledOverride,
} from '../../backend/droid/jsonrpc/notificationFingerprint'
import {
  resolveSlashCommandText,
  scanSlashCommands,
  type SlashCommandEntry,
} from '../../backend/slashCommands/slashCommands'
import { scanSkills } from '../../backend/skills/skills'
import {
  createWorkspace,
  getWorkspaceInfo,
  listBranches,
  listWorktreeBranchesInUse,
  pushBranch,
  removeWorktree,
  switchWorkspaceBranch,
} from '../../backend/git/workspaceManager'
import { commitWorkflow, detectGitTools } from '../../backend/git/commitWorkflow'
import { generateCommitMeta } from '../../backend/git/generateCommitMeta'
import { SetupScriptRunner } from '../../backend/session/setupScriptRunner'
import { createKeyStore } from '../../backend/keys/keyStore'
import { createAppStateStore } from '../../backend/storage/appStateStore'
import { createSessionStore } from '../../backend/storage/sessionStore'
import type {
  PersistedAppState,
  PersistedAppStateV2,
  SaveSessionRequest,
  DroidAutonomyLevel,
  CustomModelDef,
  SlashCommandDef,
  SlashResolveResult,
  SkillDef,
  ProjectSettings,
  GenerateCommitMetaRequest,
  CommitWorkflowRequest,
} from '../../shared/protocol'

function apiKeyFingerprint(key: string): string {
  const k = String(key || '')
  if (!k) return ''
  return createHash('sha256').update(k, 'utf8').digest('hex').slice(0, 12)
}

function toAutonomyLevel(autoLevel: unknown): DroidAutonomyLevel {
  const v = typeof autoLevel === 'string' ? autoLevel : 'default'
  if (v === 'low') return 'auto-low'
  if (v === 'medium') return 'auto-medium'
  if (v === 'high') return 'auto-high'
  return 'spec'
}

function readTraceChainEnabled(state: PersistedAppState): boolean | undefined {
  const raw = (state as any)?.traceChainEnabled
  return typeof raw === 'boolean' ? raw : undefined
}

function readLocalDiagnosticsEnabled(state: PersistedAppState): boolean | undefined {
  const raw = (state as any)?.localDiagnosticsEnabled
  return typeof raw === 'boolean' ? raw : undefined
}

function readLocalDiagnosticsRetention(state: PersistedAppState): {
  retentionDays?: number
  maxTotalMb?: number
} {
  const daysRaw = (state as any)?.localDiagnosticsRetentionDays
  const mbRaw = (state as any)?.localDiagnosticsMaxTotalMb
  const retentionDays =
    typeof daysRaw === 'number' && Number.isFinite(daysRaw)
      ? Math.max(1, Math.floor(daysRaw))
      : undefined
  const maxTotalMb =
    typeof mbRaw === 'number' && Number.isFinite(mbRaw) ? Math.max(1, Math.floor(mbRaw)) : undefined
  return { retentionDays, maxTotalMb }
}

function isNotGitRepoError(err: unknown): boolean {
  const msg = err instanceof Error ? err.message : String(err || '')
  return /not a git repository/i.test(msg)
}

async function isExistingDir(dir: string): Promise<boolean> {
  try {
    const s = await stat(dir)
    return s.isDirectory()
  } catch {
    return false
  }
}

export function registerIpcHandlers(opts: {
  getMainWindow: () => BrowserWindow | null
  baseDir: string
  diagnostics?: LocalDiagnostics
}) {
  const appStateStore = createAppStateStore({ baseDir: opts.baseDir })
  const sessionStore = createSessionStore({ baseDir: opts.baseDir })
  const keyStore = createKeyStore(appStateStore)
  const diagnostics = opts.diagnostics || new LocalDiagnostics({ baseDir: opts.baseDir })
  const execManager = new DroidExecManager({ diagnostics })
  const setupScriptRunner = new SetupScriptRunner()

  let cachedState: PersistedAppState = { version: 2, machineId: '' }
  let activeProjectDir = ''
  let slashCache: {
    projectDir: string
    at: number
    commands: Map<string, SlashCommandEntry>
  } | null = null
  let skillCache: { projectDir: string; at: number; skills: SkillDef[] } | null = null

  const getSlashCommands = async (): Promise<Map<string, SlashCommandEntry>> => {
    const projectDir = activeProjectDir || cachedState.activeProjectDir || ''
    const now = Date.now()
    if (slashCache && slashCache.projectDir === projectDir && now - slashCache.at < 1000)
      return slashCache.commands
    const commands = await scanSlashCommands({ projectDir: projectDir || undefined })
    slashCache = { projectDir, at: now, commands }
    return commands
  }

  const getSkills = async (): Promise<SkillDef[]> => {
    const projectDir = activeProjectDir || cachedState.activeProjectDir || ''
    const now = Date.now()
    if (skillCache && skillCache.projectDir === projectDir && now - skillCache.at < 1000)
      return skillCache.skills
    const skills = await scanSkills({ projectDir: projectDir || undefined })
    skillCache = { projectDir, at: now, skills }
    return skills
  }

  void (async () => {
    cachedState = await appStateStore.load()
    activeProjectDir = cachedState.activeProjectDir || ''
    setTraceChainEnabledOverride(readTraceChainEnabled(cachedState))
    const diagEnabled = readLocalDiagnosticsEnabled(cachedState)
    diagnostics.setEnabled(typeof diagEnabled === 'boolean' ? diagEnabled : true)
    const retention = readLocalDiagnosticsRetention(cachedState)
    const bytes =
      typeof retention.maxTotalMb === 'number' ? retention.maxTotalMb * 1024 * 1024 : undefined
    diagnostics.setRetention({ maxAgeDays: retention.retentionDays, maxTotalBytes: bytes })
    await diagnostics.startMaintenance()
  })()

  ipcMain.handle('droid:version', async () => getDroidVersion())
  ipcMain.handle('app:version', async () => app.getVersion())

  ipcMain.handle('slash:list', async (): Promise<SlashCommandDef[]> => {
    const commands = await getSlashCommands()
    const defs: SlashCommandDef[] = []
    for (const entry of commands.values()) {
      const { body: _body, ...def } = entry
      defs.push(def)
    }
    defs.sort((a, b) => a.name.localeCompare(b.name))
    return defs
  })

  ipcMain.handle(
    'slash:resolve',
    async (_event, payload: { text: unknown }): Promise<SlashResolveResult> => {
      const text = typeof payload?.text === 'string' ? payload.text : ''
      const projectDir = activeProjectDir || cachedState.activeProjectDir || ''
      if (!text) return { matched: false, expandedText: '' }
      const commands = await getSlashCommands()
      return resolveSlashCommandText({ text, commands, projectDir })
    },
  )

  ipcMain.handle('skills:list', async (): Promise<SkillDef[]> => {
    const skills = await getSkills()
    skills.sort((a, b) => a.name.localeCompare(b.name))
    return skills
  })

  ipcMain.on('droid:exec', (_event, { prompt, sessionId, modelId, autoLevel, reasoningEffort }) => {
    const sid = typeof sessionId === 'string' ? sessionId : null
    const win = opts.getMainWindow()
    const emitDebug = (message: string) => {
      if (!win) return
      win.webContents.send('droid:debug', { message, sessionId: sid })
    }
    emitDebug(
      `ipc-exec-received: sessionId=${sid ?? 'null'} model=${typeof modelId === 'string' ? modelId : 'default'} auto=${typeof autoLevel === 'string' ? autoLevel : 'default'}`,
    )
    if (typeof prompt === 'string' && sid) {
      const sig = diagnostics.computePromptSig(prompt)
      diagnostics.noteInputPromptSig(sid, sig)
      void diagnostics.append({
        ts: new Date().toISOString(),
        level: 'info',
        scope: 'main',
        event: 'ipc.exec.received',
        sessionId: sid,
        correlation: {
          modelId: typeof modelId === 'string' ? modelId : undefined,
          autoLevel: typeof autoLevel === 'string' ? autoLevel : undefined,
        },
        data: { promptSig: sig },
      })
    }
    if (typeof prompt !== 'string' || !prompt.trim() || !sid) {
      emitDebug(
        `ipc-exec-precheck-failed: sessionId=${sid ?? 'null'} reason=invalid-prompt-or-session`,
      )
      void diagnostics.append({
        ts: new Date().toISOString(),
        level: 'warn',
        scope: 'main',
        event: 'ipc.exec.precheck_failed',
        sessionId: sid || undefined,
        data: { reason: 'invalid-prompt-or-session' },
      })
      return
    }

    const cwd = activeProjectDir
    const env: Record<string, string | undefined> = { ...process.env }

    if (!win) return
    void (async () => {
      if (!cwd || !(await isExistingDir(cwd))) {
        emitDebug(
          `ipc-exec-precheck-failed: sessionId=${sid} reason=missing-or-invalid-project-dir cwd=${cwd || '(empty)'}`,
        )
        win.webContents.send('droid:error', {
          message: 'No active project directory set. Select a project first.',
          sessionId: sid,
        })
        win.webContents.send('droid:turn-end', { code: 1, sessionId: sid })
        return
      }

      const machineId = (cachedState as PersistedAppStateV2).machineId
      if (!machineId) {
        emitDebug(`ipc-exec-precheck-failed: sessionId=${sid} reason=missing-machine-id`)
        win.webContents.send('droid:error', {
          message: 'Missing machineId in app state.',
          sessionId: sid,
        })
        win.webContents.send('droid:turn-end', { code: 1, sessionId: sid })
        return
      }

      const resumeSessionId = sid

      if (!execManager.hasSession(sid)) {
        if (cachedState.apiKey) {
          env['FACTORY_API_KEY'] = cachedState.apiKey
        } else {
          const activeKey = await keyStore.getActiveKey()
          if (activeKey) {
            env['FACTORY_API_KEY'] = activeKey
            if (activeKey !== cachedState.apiKey) {
              cachedState = {
                ...(cachedState as PersistedAppStateV2),
                apiKey: activeKey,
                version: 2,
              }
            }
          }
        }
      }

      emitDebug(`ipc-exec-send-start: sessionId=${sid}`)
      void diagnostics.append({
        ts: new Date().toISOString(),
        level: 'debug',
        scope: 'main',
        event: 'ipc.exec.send_start',
        sessionId: sid,
      })
      try {
        await execManager.send({
          sessionId: sid,
          resumeSessionId,
          machineId,
          prompt,
          cwd,
          modelId: typeof modelId === 'string' ? modelId : undefined,
          autonomyLevel: toAutonomyLevel(autoLevel),
          reasoningEffort: typeof reasoningEffort === 'string' ? reasoningEffort : undefined,
          env,
        })
        emitDebug(`ipc-exec-send-returned: sessionId=${sid}`)
        void diagnostics.append({
          ts: new Date().toISOString(),
          level: 'debug',
          scope: 'main',
          event: 'ipc.exec.send_returned',
          sessionId: sid,
        })
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        emitDebug(`ipc-exec-send-threw: sessionId=${sid} error=${msg}`)
        void diagnostics.append({
          ts: new Date().toISOString(),
          level: 'error',
          scope: 'main',
          event: 'ipc.exec.send_threw',
          sessionId: sid,
          data: { error: msg },
        })
        win.webContents.send('droid:error', { message: msg, sessionId: sid })
        win.webContents.send('droid:turn-end', { code: 1, sessionId: sid })
      }
    })()
  })

  const unsub = execManager.onEvent((ev) => {
    const w = opts.getMainWindow()
    if (!w || w.isDestroyed()) return
    if (ev.type === 'session-id-replaced') {
      void sessionStore.replaceSessionId(ev.oldSessionId, ev.newSessionId)
      w.webContents.send('droid:session-id-replaced', {
        oldSessionId: ev.oldSessionId,
        newSessionId: ev.newSessionId,
        reason: ev.reason,
      })
      return
    }

    const sid = ev.sessionId
    if (ev.type === 'stdout') w.webContents.send('droid:stdout', { data: ev.data, sessionId: sid })
    else if (ev.type === 'stderr')
      w.webContents.send('droid:stderr', { data: ev.data, sessionId: sid })
    else if (ev.type === 'error')
      w.webContents.send('droid:error', { message: ev.message, sessionId: sid })
    else if (ev.type === 'rpc-notification') {
      if (isTraceChainEnabled()) {
        w.webContents.send('droid:debug', {
          message: formatNotificationTrace('ipc-out', ev.message),
          sessionId: sid,
        })
      }
      w.webContents.send('droid:rpc-notification', { message: ev.message, sessionId: sid })
    } else if (ev.type === 'rpc-request')
      w.webContents.send('droid:rpc-request', { message: ev.message, sessionId: sid })
    else if (ev.type === 'turn-end')
      w.webContents.send('droid:turn-end', { code: ev.code, sessionId: sid })
    else if (ev.type === 'debug')
      w.webContents.send('droid:debug', { message: ev.message, sessionId: sid })

    if (diagnostics.isEnabled()) {
      const ts = new Date().toISOString()
      if (ev.type === 'stdout')
        void diagnostics.append({
          ts,
          level: 'debug',
          scope: 'backend',
          event: 'backend.stdout',
          sessionId: sid,
          data: { data: ev.data },
        })
      else if (ev.type === 'stderr')
        void diagnostics.append({
          ts,
          level: 'debug',
          scope: 'backend',
          event: 'backend.stderr',
          sessionId: sid,
          data: { data: ev.data },
        })
      else if (ev.type === 'error')
        void diagnostics.append({
          ts,
          level: 'error',
          scope: 'backend',
          event: 'backend.error',
          sessionId: sid,
          data: { message: ev.message },
        })
      else if (ev.type === 'turn-end')
        void diagnostics.append({
          ts,
          level: 'info',
          scope: 'backend',
          event: 'backend.turn_end',
          sessionId: sid,
          data: { code: ev.code },
        })
      else if (ev.type === 'debug')
        void diagnostics.append({
          ts,
          level: 'debug',
          scope: 'backend',
          event: 'backend.debug',
          sessionId: sid,
          data: { message: ev.message },
        })
    }
  })

  const unsubSetup = setupScriptRunner.onEvent((event) => {
    const w = opts.getMainWindow()
    if (!w || w.isDestroyed()) return
    w.webContents.send('session:setup-event', { event, sessionId: event.sessionId })
  })

  ipcMain.on('droid:cancel', (_event, payload: { sessionId: string | null }) => {
    const sid = typeof payload?.sessionId === 'string' ? payload.sessionId : null
    if (!sid) return
    execManager.cancel(sid)
    const w = opts.getMainWindow()
    if (w) w.webContents.send('droid:turn-end', { code: 1, sessionId: sid })
  })

  ipcMain.handle(
    'droid:updateSessionSettings',
    async (
      _event,
      payload: {
        sessionId: string
        modelId?: string
        autoLevel?: string
        reasoningEffort?: string
      },
    ) => {
      if (!payload || typeof payload !== 'object') throw new Error('Invalid payload')
      if (typeof payload.sessionId !== 'string' || !payload.sessionId.trim())
        throw new Error('Missing sessionId')
      await execManager.updateSessionSettings({
        sessionId: payload.sessionId,
        modelId: typeof payload.modelId === 'string' ? payload.modelId : undefined,
        autonomyLevel:
          typeof payload.autoLevel === 'string' ? toAutonomyLevel(payload.autoLevel) : undefined,
        reasoningEffort:
          typeof payload.reasoningEffort === 'string' ? payload.reasoningEffort : undefined,
      })

      return { ok: true } as const
    },
  )

  ipcMain.on(
    'droid:permission-response',
    (_event, payload: { sessionId: string; requestId: string; selectedOption: any }) => {
      if (!payload || typeof payload !== 'object') return
      if (typeof payload.sessionId !== 'string' || typeof payload.requestId !== 'string') return
      execManager.respondPermission({
        sessionId: payload.sessionId,
        requestId: payload.requestId,
        selectedOption: payload.selectedOption,
      })
    },
  )

  ipcMain.on(
    'droid:askuser-response',
    (
      _event,
      payload: { sessionId: string; requestId: string; cancelled?: boolean; answers: any[] },
    ) => {
      if (!payload || typeof payload !== 'object') return
      if (
        typeof payload.sessionId !== 'string' ||
        typeof payload.requestId !== 'string' ||
        !Array.isArray(payload.answers)
      )
        return
      execManager.respondAskUser({
        sessionId: payload.sessionId,
        requestId: payload.requestId,
        cancelled: payload.cancelled,
        answers: payload.answers as any,
      })
    },
  )

  ipcMain.on('droid:setApiKey', (_event, apiKey: string) => {
    cachedState = {
      ...(cachedState as PersistedAppStateV2),
      apiKey: apiKey || undefined,
      version: 2,
    }
    void appStateStore.save(cachedState)
  })

  ipcMain.handle(
    'droid:getApiKey',
    async () => cachedState.apiKey || process.env['FACTORY_API_KEY'] || '',
  )

  // Multi-key management
  ipcMain.handle('keys:list', async () => {
    const [keys, usages] = await Promise.all([keyStore.getKeys(), keyStore.getUsages()])
    const activeKey = cachedState.apiKey || ''
    return keys.map((entry, index) => ({
      key: entry.key,
      note: entry.note || '',
      addedAt: entry.addedAt,
      index,
      isActive: entry.key === activeKey,
      usage: usages.get(entry.key) || null,
    }))
  })

  ipcMain.handle('keys:add', async (_event, payload: { keys: string[] }) => {
    const rawKeys = Array.isArray(payload?.keys)
      ? payload.keys.filter((k): k is string => typeof k === 'string')
      : []
    if (rawKeys.length === 0) return { added: 0, duplicates: 0 }
    const result = await keyStore.addKeys(rawKeys)
    cachedState = await appStateStore.load()
    return result
  })

  ipcMain.handle('keys:remove', async (_event, payload: { index: number }) => {
    const index = typeof payload?.index === 'number' ? payload.index : -1
    if (index < 0) return
    await keyStore.removeKey(index)
    cachedState = await appStateStore.load()
  })

  ipcMain.handle('keys:note', async (_event, payload: { index: number; note: string }) => {
    const index = typeof payload?.index === 'number' ? payload.index : -1
    const note = typeof payload?.note === 'string' ? payload.note : ''
    if (index < 0) return
    await keyStore.updateNote(index, note)
  })

  ipcMain.handle('keys:refresh', async () => {
    const usages = await keyStore.refreshUsages()
    const keys = await keyStore.getKeys()
    const activeKey = cachedState.apiKey || ''
    return keys.map((entry, index) => ({
      key: entry.key,
      note: entry.note || '',
      addedAt: entry.addedAt,
      index,
      isActive: entry.key === activeKey,
      usage: usages.get(entry.key) || null,
    }))
  })

  ipcMain.handle('keys:active', async () => {
    const key = await keyStore.getActiveKey()
    if (key && key !== cachedState.apiKey) {
      cachedState = { ...(cachedState as PersistedAppStateV2), apiKey: key, version: 2 }
    }
    return { key: key || '', apiKeyFingerprint: key ? apiKeyFingerprint(key) : '' }
  })

  ipcMain.handle('shell:openInEditor', async (_event, params: { dir: string }) => {
    const dir = typeof params?.dir === 'string' ? params.dir.trim() : ''
    if (!dir) return
    await shell.openPath(dir)
  })

  ipcMain.handle('shell:detectEditors', async () => {
    const { detectInstalledEditors } = await import('../editors')
    return detectInstalledEditors()
  })

  ipcMain.handle(
    'shell:openWithEditor',
    async (_event, params: { dir: string; editorId: string }) => {
      const dir = typeof params?.dir === 'string' ? params.dir.trim() : ''
      const editorId = typeof params?.editorId === 'string' ? params.editorId.trim() : ''
      if (!dir || !editorId) return
      const { openWithEditor } = await import('../editors')
      await openWithEditor(dir, editorId)
    },
  )

  ipcMain.handle('dialog:openDirectory', async () => {
    const win = opts.getMainWindow()
    if (!win) return null
    const result = await dialog.showOpenDialog(win, { properties: ['openDirectory'] })
    if (result.canceled || result.filePaths.length === 0) return null
    return result.filePaths[0]
  })

  ipcMain.handle('dialog:openFile', async () => {
    const win = opts.getMainWindow()
    if (!win) return null
    const result = await dialog.showOpenDialog(win, { properties: ['openFile', 'multiSelections'] })
    if (result.canceled || result.filePaths.length === 0) return null
    return result.filePaths
  })

  ipcMain.handle(
    'attachment:save',
    async (_event, params: { sourcePaths: string[]; projectDir: string }) => {
      if (!params || !Array.isArray(params.sourcePaths) || typeof params.projectDir !== 'string')
        return []
      const attachDir = join(params.projectDir, '.attachment')
      await mkdir(attachDir, { recursive: true })
      const results: Array<{ name: string; path: string }> = []
      for (const src of params.sourcePaths) {
        const ext = extname(src)
        const base = basename(src, ext)
        let destName = basename(src)
        let dest = join(attachDir, destName)
        try {
          await stat(dest)
          destName = `${base}-${Date.now()}${ext}`
          dest = join(attachDir, destName)
        } catch {
          // no collision
        }
        await copyFile(src, dest)
        results.push({ name: destName, path: dest })
      }
      return results
    },
  )

  ipcMain.handle(
    'attachment:saveClipboardImage',
    async (
      _event,
      params: { data: number[]; mimeType: string; projectDir: string; fileName?: string },
    ) => {
      if (!params || !Array.isArray(params.data) || typeof params.projectDir !== 'string')
        return null
      const attachDir = join(params.projectDir, '.attachment')
      await mkdir(attachDir, { recursive: true })
      const extMap: Record<string, string> = {
        'image/png': '.png',
        'image/jpeg': '.jpg',
        'image/gif': '.gif',
        'image/webp': '.webp',
        'image/svg+xml': '.svg',
      }
      const inputName = typeof params.fileName === 'string' ? basename(params.fileName.trim()) : ''
      const fallbackExt = extMap[params.mimeType] || '.png'
      const hasExt = inputName ? extname(inputName) : ''
      let name = inputName
        ? hasExt
          ? inputName
          : `${inputName}${fallbackExt}`
        : `clipboard-${Date.now()}${fallbackExt}`
      let dest = join(attachDir, name)
      try {
        await stat(dest)
        const ext = extname(name)
        const base = basename(name, ext)
        name = `${base}-${Date.now()}${ext}`
        dest = join(attachDir, name)
      } catch {
        // no collision
      }
      await writeFile(dest, Buffer.from(params.data))
      return { name, path: dest }
    },
  )

  ipcMain.on('project:setDir', (_event, dir: string | null) => {
    activeProjectDir = dir || ''
    cachedState = {
      ...(cachedState as PersistedAppStateV2),
      activeProjectDir: dir || undefined,
      version: 2,
    }
    void appStateStore.save(cachedState)
  })
  ipcMain.handle('project:getDir', async () => activeProjectDir || '')

  ipcMain.on('appState:setTraceChainEnabled', (_event, enabled: unknown) => {
    if (typeof enabled !== 'boolean') return
    cachedState = {
      ...(cachedState as PersistedAppStateV2),
      traceChainEnabled: enabled,
      version: 2,
    }
    setTraceChainEnabledOverride(enabled)
    void appStateStore.save(cachedState)
  })

  ipcMain.on('appState:setShowDebugTrace', (_event, enabled: unknown) => {
    if (typeof enabled !== 'boolean') return
    cachedState = { ...(cachedState as PersistedAppStateV2), showDebugTrace: enabled, version: 2 }
    void appStateStore.save(cachedState)
  })

  ipcMain.on('appState:setDebugTraceMaxLines', (_event, maxLines: unknown) => {
    if (maxLines !== null && typeof maxLines !== 'number') return
    const v =
      typeof maxLines === 'number' && Number.isFinite(maxLines)
        ? Math.min(10_000, Math.max(1, Math.floor(maxLines)))
        : undefined
    cachedState = { ...(cachedState as PersistedAppStateV2), debugTraceMaxLines: v, version: 2 }
    void appStateStore.save(cachedState)
  })

  ipcMain.on('appState:setLocalDiagnosticsEnabled', (_event, enabled: unknown) => {
    if (typeof enabled !== 'boolean') return
    cachedState = {
      ...(cachedState as PersistedAppStateV2),
      localDiagnosticsEnabled: enabled,
      version: 2,
    }
    diagnostics.setEnabled(enabled)
    void appStateStore.save(cachedState)
  })

  ipcMain.on('appState:setLocalDiagnosticsRetention', (_event, payload: unknown) => {
    const daysRaw = (payload as any)?.retentionDays
    const mbRaw = (payload as any)?.maxTotalMb
    if (typeof daysRaw !== 'number' && typeof mbRaw !== 'number') return
    const retentionDays =
      typeof daysRaw === 'number' && Number.isFinite(daysRaw)
        ? Math.max(1, Math.floor(daysRaw))
        : undefined
    const maxTotalMb =
      typeof mbRaw === 'number' && Number.isFinite(mbRaw)
        ? Math.max(1, Math.floor(mbRaw))
        : undefined

    cachedState = {
      ...(cachedState as PersistedAppStateV2),
      localDiagnosticsRetentionDays: retentionDays,
      localDiagnosticsMaxTotalMb: maxTotalMb,
      version: 2,
    }
    const bytes = typeof maxTotalMb === 'number' ? maxTotalMb * 1024 * 1024 : undefined
    diagnostics.setRetention({ maxAgeDays: retentionDays, maxTotalBytes: bytes })
    void appStateStore.save(cachedState)
  })

  ipcMain.on('appState:setLanAccessEnabled', (_event, enabled: unknown) => {
    if (typeof enabled !== 'boolean') return
    cachedState = { ...(cachedState as PersistedAppStateV2), lanAccessEnabled: enabled, version: 2 }
    void appStateStore.save(cachedState)
  })

  ipcMain.on('appState:setCommitMessageModelId', (_event, modelId: unknown) => {
    const id = typeof modelId === 'string' ? modelId.trim() : ''
    cachedState = {
      ...(cachedState as PersistedAppStateV2),
      commitMessageModelId: id || undefined,
      version: 2,
    }
    void appStateStore.save(cachedState)
  })

  ipcMain.handle('appState:load', async () => {
    cachedState = await appStateStore.load()
    activeProjectDir = cachedState.activeProjectDir || ''
    setTraceChainEnabledOverride(readTraceChainEnabled(cachedState))
    const diagEnabled = readLocalDiagnosticsEnabled(cachedState)
    diagnostics.setEnabled(typeof diagEnabled === 'boolean' ? diagEnabled : true)
    const retention = readLocalDiagnosticsRetention(cachedState)
    const bytes =
      typeof retention.maxTotalMb === 'number' ? retention.maxTotalMb * 1024 * 1024 : undefined
    diagnostics.setRetention({ maxAgeDays: retention.retentionDays, maxTotalBytes: bytes })
    return cachedState
  })

  ipcMain.handle('diagnostics:getDir', async () => diagnostics.getDiagnosticsDir())
  ipcMain.handle(
    'diagnostics:export',
    async (_event, params: { sessionId?: string | null; debugTraceText?: string }) => {
      const sessionId = typeof params?.sessionId === 'string' ? params.sessionId : null
      const debugTraceText = typeof params?.debugTraceText === 'string' ? params.debugTraceText : ''
      const suggestedName = `droi-diagnostics${sessionId ? `-${sessionId}` : ''}.zip`
      const win = opts.getMainWindow()
      const dialogOpts = {
        title: 'Export diagnostics bundle',
        defaultPath: join(diagnostics.getDiagnosticsDir(), 'bundles', suggestedName),
        filters: [{ name: 'Zip', extensions: ['zip'] }],
      }
      const res = win
        ? await dialog.showSaveDialog(win, dialogOpts)
        : await dialog.showSaveDialog(dialogOpts)
      if (res.canceled || !res.filePath) return { path: '' }
      const state = (cachedState as PersistedAppStateV2) || null
      const path = await diagnostics.exportToPath({
        outputPath: res.filePath,
        sessionId,
        appVersion: '',
        appState: state,
        debugTraceText,
      })
      return { path }
    },
  )
  ipcMain.handle('diagnostics:openPath', async (_event, targetPath: unknown) => {
    const p = typeof targetPath === 'string' ? targetPath : ''
    if (!p) return { ok: true as const }
    await shell.openPath(p)
    return { ok: true as const }
  })
  ipcMain.on(
    'diagnostics:event',
    (
      _event,
      payload: {
        sessionId?: string | null
        event: string
        level?: string
        data?: unknown
        correlation?: Record<string, unknown>
      },
    ) => {
      const event = typeof payload?.event === 'string' ? payload.event : ''
      if (!event) return
      const sid = typeof payload?.sessionId === 'string' ? payload.sessionId : undefined
      const levelRaw = typeof payload?.level === 'string' ? payload.level : 'debug'
      const level =
        levelRaw === 'info' || levelRaw === 'warn' || levelRaw === 'error' ? levelRaw : 'debug'
      void diagnostics.append({
        ts: new Date().toISOString(),
        level,
        scope: 'renderer',
        event,
        sessionId: sid,
        correlation: payload?.correlation,
        data: payload?.data,
      })
    },
  )

  ipcMain.on('appState:saveProjects', (_event, projects: unknown[]) => {
    const normalized = Array.isArray(projects)
      ? projects
          .map((p) => ({ dir: (p as any)?.dir, name: (p as any)?.name }))
          .filter((p) => typeof p.dir === 'string' && p.dir && typeof p.name === 'string' && p.name)
      : []

    cachedState = { ...(cachedState as PersistedAppStateV2), projects: normalized, version: 2 }
    void appStateStore.save(cachedState)
  })

  ipcMain.handle(
    'appState:updateProjectSettings',
    async (_event, params: { repoRoot: string; settings: ProjectSettings }) => {
      const repoRoot = typeof params?.repoRoot === 'string' ? params.repoRoot.trim() : ''
      if (!repoRoot) return cachedState
      const rawSettings = (params as any)?.settings || {}
      const hasBaseBranch = Object.prototype.hasOwnProperty.call(rawSettings, 'baseBranch')
      const hasPrefix = Object.prototype.hasOwnProperty.call(rawSettings, 'worktreePrefix')
      const hasSetupScript = Object.prototype.hasOwnProperty.call(rawSettings, 'setupScript')
      const baseBranch =
        typeof rawSettings.baseBranch === 'string' ? String(rawSettings.baseBranch).trim() : ''
      const worktreePrefix =
        typeof rawSettings.worktreePrefix === 'string'
          ? String(rawSettings.worktreePrefix).trim()
          : ''
      const setupScript =
        typeof rawSettings.setupScript === 'string' ? String(rawSettings.setupScript).trim() : ''
      const settingsPatch: ProjectSettings = {
        ...(hasBaseBranch ? { baseBranch: baseBranch || undefined } : {}),
        ...(hasPrefix ? { worktreePrefix: worktreePrefix || undefined } : {}),
        ...(hasSetupScript ? { setupScript: setupScript || undefined } : {}),
      }

      const prev = cachedState as PersistedAppStateV2
      const prevMap =
        (prev as any).projectSettings && typeof (prev as any).projectSettings === 'object'
          ? ((prev as any).projectSettings as Record<string, ProjectSettings>)
          : {}
      const merged: Record<string, ProjectSettings> = {
        ...prevMap,
        [repoRoot]: { ...(prevMap[repoRoot] || {}), ...settingsPatch },
      }

      cachedState = { ...prev, projectSettings: merged, version: 2 }
      await appStateStore.save(cachedState)
      return cachedState
    },
  )

  ipcMain.handle('session:save', async (_event, req: SaveSessionRequest) => {
    if (!req || typeof req !== 'object') return null
    return sessionStore.save(req)
  })

  ipcMain.handle(
    'session:setup:run',
    async (_event, params: { sessionId: string; projectDir: string; script: string }) => {
      const sessionId = typeof params?.sessionId === 'string' ? params.sessionId.trim() : ''
      const projectDir = typeof params?.projectDir === 'string' ? params.projectDir.trim() : ''
      const script = typeof params?.script === 'string' ? params.script.trim() : ''
      if (!sessionId || !projectDir || !script)
        throw new Error('Missing sessionId/projectDir/script')
      if (!(await isExistingDir(projectDir))) throw new Error('Invalid projectDir')

      await setupScriptRunner.run({ sessionId, projectDir, script })
      return { ok: true } as const
    },
  )

  ipcMain.on('session:setup:cancel', (_event, params: { sessionId: string }) => {
    const sessionId = typeof params?.sessionId === 'string' ? params.sessionId.trim() : ''
    if (!sessionId) return
    setupScriptRunner.cancel(sessionId)
  })

  ipcMain.handle('session:load', async (_event, id: string) => sessionStore.load(id))

  ipcMain.handle(
    'session:create',
    async (
      _event,
      payload: { cwd: string; modelId?: string; autoLevel?: string; reasoningEffort?: string },
    ) => {
      const cwd = typeof payload?.cwd === 'string' ? payload.cwd.trim() : ''
      if (!cwd) throw new Error('Missing cwd')
      if (!(await isExistingDir(cwd))) throw new Error('Invalid cwd')

      const machineId = (cachedState as PersistedAppStateV2).machineId
      if (!machineId) throw new Error('Missing machineId')

      const env: Record<string, string | undefined> = { ...process.env }
      const activeKey = await keyStore.getActiveKey()
      if (activeKey) {
        env['FACTORY_API_KEY'] = activeKey
        if (activeKey !== cachedState.apiKey) {
          cachedState = { ...(cachedState as PersistedAppStateV2), apiKey: activeKey, version: 2 }
        }
      } else if (cachedState.apiKey) env['FACTORY_API_KEY'] = cachedState.apiKey

      const res = await execManager.createSession({
        machineId,
        cwd,
        modelId: typeof payload.modelId === 'string' ? payload.modelId : undefined,
        autonomyLevel:
          typeof payload.autoLevel === 'string' ? toAutonomyLevel(payload.autoLevel) : undefined,
        reasoningEffort:
          typeof payload.reasoningEffort === 'string' ? payload.reasoningEffort : undefined,
        env,
      })
      return res
    },
  )

  ipcMain.handle('session:restart', async (_event, payload: { sessionId: string }) => {
    const sessionId = typeof payload?.sessionId === 'string' ? payload.sessionId.trim() : ''
    if (!sessionId) throw new Error('Missing sessionId')
    execManager.disposeSession(sessionId)

    cachedState = await appStateStore.load()
    const key = cachedState.apiKey || ''
    return { ok: true, apiKeyFingerprint: key ? apiKeyFingerprint(key) : '' } as const
  })

  ipcMain.handle('session:clear', async (_event, payload: { id: string }) => {
    const id = typeof payload?.id === 'string' ? payload.id.trim() : ''
    if (!id) return null

    const existing = await sessionStore.load(id)
    const cwd = (existing?.projectDir || '').trim() || activeProjectDir
    if (!cwd) return null

    const machineId = (cachedState as PersistedAppStateV2).machineId
    if (!machineId) return null

    execManager.disposeSession(id)

    const env: Record<string, string | undefined> = { ...process.env }
    const activeKey = await keyStore.getActiveKey()
    if (activeKey) {
      env['FACTORY_API_KEY'] = activeKey
      if (activeKey !== cachedState.apiKey) {
        cachedState = { ...(cachedState as PersistedAppStateV2), apiKey: activeKey, version: 2 }
      }
    } else if (cachedState.apiKey) env['FACTORY_API_KEY'] = cachedState.apiKey

    const created = await execManager.createSession({
      machineId,
      cwd,
      modelId: existing?.model || undefined,
      autonomyLevel: existing?.autoLevel ? toAutonomyLevel(existing.autoLevel) : undefined,
      reasoningEffort: existing?.reasoningEffort || undefined,
      env,
    })

    const meta = await sessionStore.replaceSessionId(id, created.sessionId)
    return meta
  })
  ipcMain.handle('session:list', async () => sessionStore.list())
  ipcMain.handle('session:delete', async (_event, id: string) => sessionStore.delete(id))

  ipcMain.handle('git:status', async (_event, params: { projectDir: string }) => {
    const dir = typeof params?.projectDir === 'string' ? params.projectDir : activeProjectDir
    if (!dir) return []
    return new Promise<
      Array<{ status: string; path: string; additions: number; deletions: number }>
    >((resolve) => {
      execFile(
        'git',
        ['status', '--porcelain', '-uall'],
        { cwd: dir, timeout: 5000 },
        (err, stdout) => {
          if (err) {
            resolve([])
            return
          }
          const statusMap = new Map<string, string>()
          for (const line of stdout.split('\n').filter((l) => l.trim())) {
            const status = line.substring(0, 2).trim()
            const filePath = line.substring(3)
            statusMap.set(filePath, status)
          }
          if (statusMap.size === 0) {
            resolve([])
            return
          }
          execFile(
            'git',
            ['diff', '--numstat', 'HEAD'],
            { cwd: dir, timeout: 5000 },
            (err2, diffOut) => {
              const diffMap = new Map<string, { additions: number; deletions: number }>()
              if (!err2 && diffOut) {
                for (const line of diffOut.split('\n').filter((l) => l.trim())) {
                  const parts = line.split('\t')
                  if (parts.length >= 3) {
                    const add = parts[0] === '-' ? 0 : parseInt(parts[0], 10) || 0
                    const del = parts[1] === '-' ? 0 : parseInt(parts[1], 10) || 0
                    diffMap.set(parts[2], { additions: add, deletions: del })
                  }
                }
              }
              // Also get unstaged diff for untracked won't show in diff HEAD
              execFile(
                'git',
                ['diff', '--numstat'],
                { cwd: dir, timeout: 5000 },
                (_err3, unstagedOut) => {
                  if (unstagedOut) {
                    for (const line of unstagedOut.split('\n').filter((l) => l.trim())) {
                      const parts = line.split('\t')
                      if (parts.length >= 3 && !diffMap.has(parts[2])) {
                        const add = parts[0] === '-' ? 0 : parseInt(parts[0], 10) || 0
                        const del = parts[1] === '-' ? 0 : parseInt(parts[1], 10) || 0
                        diffMap.set(parts[2], { additions: add, deletions: del })
                      }
                    }
                  }
                  const files = Array.from(statusMap.entries()).map(([filePath, status]) => {
                    const diff = diffMap.get(filePath) || { additions: 0, deletions: 0 }
                    return {
                      status,
                      path: filePath,
                      additions: diff.additions,
                      deletions: diff.deletions,
                    }
                  })
                  resolve(files)
                },
              )
            },
          )
        },
      )
    })
  })

  ipcMain.handle('factory:getCustomModels', async (): Promise<CustomModelDef[]> => {
    try {
      const settingsPath = join(homedir(), '.factory', 'settings.json')
      const raw = JSON.parse(await readFile(settingsPath, 'utf-8'))
      const models = Array.isArray(raw?.customModels) ? raw.customModels : []
      return models
        .filter((m: any) => typeof m?.id === 'string' && typeof m?.displayName === 'string')
        .map((m: any) => ({
          id: m.id,
          displayName: m.displayName,
          model: String(m.model || ''),
          provider: String(m.provider || 'custom'),
        }))
    } catch {
      return []
    }
  })

  ipcMain.handle('git:branch', async (_event, params: { projectDir: string }) => {
    const dir = typeof params?.projectDir === 'string' ? params.projectDir : activeProjectDir
    if (!dir) return ''
    return new Promise<string>((resolve) => {
      execFile(
        'git',
        ['rev-parse', '--abbrev-ref', 'HEAD'],
        { cwd: dir, timeout: 5000 },
        (err, stdout) => {
          if (err) {
            resolve('')
            return
          }
          resolve(stdout.trim())
        },
      )
    })
  })

  ipcMain.handle('git:list-branches', async (_event, params: { projectDir: string }) => {
    const dir = typeof params?.projectDir === 'string' ? params.projectDir : activeProjectDir
    if (!dir) return []
    try {
      return await listBranches(dir)
    } catch {
      return []
    }
  })

  ipcMain.handle('git:worktree-branches-in-use', async (_event, params: { repoRoot: string }) => {
    const repoRoot = typeof params?.repoRoot === 'string' ? params.repoRoot.trim() : ''
    const dir = repoRoot || activeProjectDir
    if (!dir) return []
    try {
      return await listWorktreeBranchesInUse({ repoRoot: dir })
    } catch {
      return []
    }
  })

  ipcMain.handle('git:workspace-info', async (_event, params: { projectDir: string }) => {
    const dir = typeof params?.projectDir === 'string' ? params.projectDir : activeProjectDir
    if (!dir) return null
    try {
      return await getWorkspaceInfo(dir)
    } catch (err) {
      if (isNotGitRepoError(err)) return null
      throw err
    }
  })

  ipcMain.handle(
    'git:switch-workspace',
    async (_event, params: { projectDir: string; branch: string }) => {
      const dir = typeof params?.projectDir === 'string' ? params.projectDir : activeProjectDir
      const branch = typeof params?.branch === 'string' ? params.branch.trim() : ''
      if (!dir || !branch) return null
      return await switchWorkspaceBranch({ projectDir: dir, branch })
    },
  )

  ipcMain.handle(
    'git:create-workspace',
    async (
      _event,
      params: {
        projectDir: string
        mode: 'branch' | 'worktree'
        branch: string
        baseBranch?: string
        useExistingBranch?: boolean
      },
    ) => {
      const dir = typeof params?.projectDir === 'string' ? params.projectDir : activeProjectDir
      const mode = params?.mode === 'worktree' ? 'worktree' : 'branch'
      const branch = typeof params?.branch === 'string' ? params.branch.trim() : ''
      const baseBranch =
        typeof params?.baseBranch === 'string' ? params.baseBranch.trim() : undefined
      const useExistingBranch = Boolean(params?.useExistingBranch)
      if (!dir || !branch) return null
      return await createWorkspace({ projectDir: dir, mode, branch, baseBranch, useExistingBranch })
    },
  )

  ipcMain.handle(
    'git:remove-worktree',
    async (_event, params: { repoRoot: string; worktreeDir: string; force?: boolean }) => {
      const repoRoot = typeof params?.repoRoot === 'string' ? params.repoRoot.trim() : ''
      const worktreeDir = typeof params?.worktreeDir === 'string' ? params.worktreeDir.trim() : ''
      const force = Boolean(params?.force)
      if (!repoRoot || !worktreeDir) throw new Error('Missing repoRoot/worktreeDir')
      const deleteBranch = Boolean((params as any)?.deleteBranch)
      const branch =
        typeof (params as any)?.branch === 'string' ? String((params as any).branch).trim() : ''
      await removeWorktree({ repoRoot, worktreeDir, force, deleteBranch, branch })
      return { ok: true } as const
    },
  )

  ipcMain.handle(
    'git:push-branch',
    async (_event, params: { projectDir: string; remote?: string; branch?: string }) => {
      const dir = typeof params?.projectDir === 'string' ? params.projectDir : activeProjectDir
      if (!dir) throw new Error('Missing projectDir')
      const remote = typeof params?.remote === 'string' ? params.remote.trim() : undefined
      const branch = typeof params?.branch === 'string' ? params.branch.trim() : undefined
      const result = await pushBranch({ projectDir: dir, remote, branch })
      return { ok: true, remote: result.remote, branch: result.branch } as const
    },
  )

  ipcMain.handle('git:detect-tools', async (_event, req: { projectDir: string }) =>
    detectGitTools(req),
  )

  ipcMain.handle('git:generate-commit-meta', async (_event, req: GenerateCommitMetaRequest) => {
    const state = cachedState
    return generateCommitMeta({ req, state, execManager, keyStore })
  })

  ipcMain.handle('git:commit-workflow', async (_event, req: CommitWorkflowRequest) => {
    const win = opts.getMainWindow()
    return commitWorkflow(req, (progress) => {
      if (win && !win.isDestroyed()) {
        win.webContents.send('git:commit-workflow-progress', progress)
      }
    })
  })

  return {
    cancelActiveRun: () => {
      unsub()
      unsubSetup()
      const cancelledExec = execManager.disposeAllSessions()
      const cancelledSetup = setupScriptRunner.disposeAll() > 0
      return cancelledExec || cancelledSetup
    },
  }
}
