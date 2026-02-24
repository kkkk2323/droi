import { execFile } from 'child_process'
import { createHash } from 'crypto'
import { createReadStream } from 'fs'
import { readFile, stat } from 'fs/promises'
import { homedir } from 'os'
import { join, resolve } from 'path'
import { PassThrough } from 'stream'
import { Hono, type Context } from 'hono'
import { getDroidVersion } from '../../../backend/droid/droidExecRunner.ts'
import { redactJson } from '../../../backend/diagnostics/redact.ts'
import {
  createWorkspace,
  getWorkspaceInfo,
  listBranches,
  listWorktreeBranchesInUse,
  pushBranch,
  removeWorktree,
  switchWorkspaceBranch,
} from '../../../backend/git/workspaceManager.ts'
import { commitWorkflow, detectGitTools } from '../../../backend/git/commitWorkflow.ts'
import { generateCommitMeta } from '../../../backend/git/generateCommitMeta.ts'
import { setTraceChainEnabledOverride } from '../../../backend/droid/jsonrpc/notificationFingerprint.ts'
import { scanSkills } from '../../../backend/skills/skills.ts'
import {
  scanSlashCommands,
  resolveSlashCommandText,
} from '../../../backend/slashCommands/slashCommands.ts'
import { isDirectory } from '../../../backend/utils/fs.ts'
import type {
  DroidAutonomyLevel,
  DroidInteractionMode,
  DroidPermissionOption,
  GenerateCommitMetaRequest,
  CommitWorkflowRequest,
  PersistedAppState,
  PersistedAppStateV2,
  ProjectSettings,
  SaveSessionRequest,
} from '../../../shared/protocol'
import { getContentType } from '../../utils/path.ts'
import { jsonError, readJsonBody } from '../../utils/http.ts'
import { nodeToWebReadable, pipelineNode } from '../../utils/stream.ts'
import { UploadError, saveMultipartFiles } from '../../utils/upload.ts'
import type { ServerEnv } from '../types.ts'

function apiKeyFingerprint(key: string): string {
  const k = String(key || '')
  if (!k) return ''
  return createHash('sha256').update(k, 'utf8').digest('hex').slice(0, 12)
}

function toInteractionMode(autoLevel: unknown): DroidInteractionMode {
  const v = typeof autoLevel === 'string' ? autoLevel : 'default'
  return v === 'default' ? 'spec' : 'auto'
}

function toAutonomyLevel(autoLevel: unknown): DroidAutonomyLevel {
  const v = typeof autoLevel === 'string' ? autoLevel : 'default'
  if (v === 'medium') return 'medium'
  if (v === 'high') return 'high'
  return 'low'
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

async function loadCachedState(c: Context<ServerEnv>): Promise<PersistedAppState> {
  const deps = c.get('deps')
  deps.cachedStateRef.value = await deps.appStateStore.load()
  setTraceChainEnabledOverride(readTraceChainEnabled(deps.cachedStateRef.value))
  const diagEnabled = readLocalDiagnosticsEnabled(deps.cachedStateRef.value)
  deps.diagnostics.setEnabled(typeof diagEnabled === 'boolean' ? diagEnabled : true)
  const retention = readLocalDiagnosticsRetention(deps.cachedStateRef.value)
  const bytes =
    typeof retention.maxTotalMb === 'number' ? retention.maxTotalMb * 1024 * 1024 : undefined
  deps.diagnostics.setRetention({ maxAgeDays: retention.retentionDays, maxTotalBytes: bytes })
  return deps.cachedStateRef.value
}

export function createApiRoutes() {
  const api = new Hono<ServerEnv>()

  api.get('/version', async (c) => {
    const version = await getDroidVersion()
    return c.json({ version })
  })

  api.get('/app-version', async (c) => {
    const version = c.get('appVersion') || 'N/A'
    return c.json({ version })
  })

  api.get('/app-state', async (c) => {
    const state = await loadCachedState(c)
    return c.json(state)
  })

  api.post('/app-state', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const patch: Partial<Omit<PersistedAppState, 'version'>> = {}
      if (typeof body.apiKey === 'string' || body.apiKey === null)
        patch.apiKey = body.apiKey || undefined
      if (typeof body.activeProjectDir === 'string' || body.activeProjectDir === null)
        patch.activeProjectDir = body.activeProjectDir || undefined
      if (Array.isArray(body.projects)) patch.projects = body.projects as any
      if (typeof body.traceChainEnabled === 'boolean' || body.traceChainEnabled === null) {
        ;(patch as any).traceChainEnabled =
          body.traceChainEnabled === null ? undefined : body.traceChainEnabled
      }
      if (typeof body.showDebugTrace === 'boolean' || body.showDebugTrace === null) {
        ;(patch as any).showDebugTrace =
          body.showDebugTrace === null ? undefined : body.showDebugTrace
      }
      if (typeof body.debugTraceMaxLines === 'number' || body.debugTraceMaxLines === null) {
        const v = body.debugTraceMaxLines
        ;(patch as any).debugTraceMaxLines =
          v === null
            ? undefined
            : typeof v === 'number' && Number.isFinite(v)
              ? Math.min(10_000, Math.max(1, Math.floor(v)))
              : undefined
      }
      if (
        typeof body.localDiagnosticsEnabled === 'boolean' ||
        body.localDiagnosticsEnabled === null
      ) {
        ;(patch as any).localDiagnosticsEnabled =
          body.localDiagnosticsEnabled === null ? undefined : body.localDiagnosticsEnabled
      }
      if (
        typeof body.localDiagnosticsRetentionDays === 'number' ||
        body.localDiagnosticsRetentionDays === null
      ) {
        const v = body.localDiagnosticsRetentionDays
        ;(patch as any).localDiagnosticsRetentionDays =
          v === null
            ? undefined
            : typeof v === 'number' && Number.isFinite(v)
              ? Math.max(1, Math.floor(v))
              : undefined
      }
      if (
        typeof body.localDiagnosticsMaxTotalMb === 'number' ||
        body.localDiagnosticsMaxTotalMb === null
      ) {
        const v = body.localDiagnosticsMaxTotalMb
        ;(patch as any).localDiagnosticsMaxTotalMb =
          v === null
            ? undefined
            : typeof v === 'number' && Number.isFinite(v)
              ? Math.max(1, Math.floor(v))
              : undefined
      }
      if (typeof body.commitMessageModelId === 'string' || body.commitMessageModelId === null) {
        const v = body.commitMessageModelId
        ;(patch as any).commitMessageModelId =
          v === null ? undefined : typeof v === 'string' && v.trim() ? v.trim() : undefined
      }
      if (typeof body.lanAccessEnabled === 'boolean' || body.lanAccessEnabled === null) {
        ;(patch as any).lanAccessEnabled =
          body.lanAccessEnabled === null ? undefined : body.lanAccessEnabled
      }

      deps.cachedStateRef.value = await deps.appStateStore.update(patch as any)
      setTraceChainEnabledOverride(readTraceChainEnabled(deps.cachedStateRef.value))
      const diagEnabled = readLocalDiagnosticsEnabled(deps.cachedStateRef.value)
      deps.diagnostics.setEnabled(typeof diagEnabled === 'boolean' ? diagEnabled : true)
      const retention = readLocalDiagnosticsRetention(deps.cachedStateRef.value)
      const bytes =
        typeof retention.maxTotalMb === 'number' ? retention.maxTotalMb * 1024 * 1024 : undefined
      deps.diagnostics.setRetention({ maxAgeDays: retention.retentionDays, maxTotalBytes: bytes })
      return c.json(deps.cachedStateRef.value)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.get('/diagnostics/dir', async (c) => {
    const deps = c.get('deps')
    return c.json({ dir: deps.diagnostics.getDiagnosticsDir() })
  })

  api.post('/diagnostics/event', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sessionId = typeof body.sessionId === 'string' ? body.sessionId : undefined
      const event = typeof body.event === 'string' ? body.event : ''
      const level = typeof body.level === 'string' ? body.level : 'debug'
      const correlation =
        body.correlation && typeof body.correlation === 'object'
          ? (body.correlation as any)
          : undefined
      if (!event) return jsonError(c, 400, 'Missing event')
      await deps.diagnostics.append({
        ts: new Date().toISOString(),
        level: level === 'info' || level === 'warn' || level === 'error' ? level : 'debug',
        scope: 'renderer',
        event,
        sessionId,
        correlation: correlation ? (redactJson(correlation) as any) : undefined,
        data: redactJson(body.data),
      })
      return c.json({ ok: true } as const)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/diagnostics/export', async (c) => {
    const deps = c.get('deps')
    const body = await readJsonBody<Record<string, unknown>>(c).catch(
      () => ({}) as Record<string, unknown>,
    )
    const sessionId = typeof body.sessionId === 'string' ? body.sessionId.trim() : ''
    const sid = sessionId || null
    const debugTraceText = typeof body.debugTraceText === 'string' ? body.debugTraceText : ''
    const state = (await loadCachedState(c)) as PersistedAppStateV2
    const buf = await deps.diagnostics.buildExportBundle({
      sessionId: sid,
      appVersion: '',
      appState: state,
      debugTraceText,
    })
    const name = `droi-diagnostics${sid ? `-${sid}` : ''}.zip`
    return new Response(buf, {
      status: 200,
      headers: {
        'Content-Type': 'application/zip',
        'Content-Disposition': `attachment; filename="${name}"`,
      },
    })
  })

  api.get('/diagnostics/export', async (c) => {
    // Compatibility download endpoint (no debug trace).
    const deps = c.get('deps')
    const sessionId = (c.req.query('sessionId') || '').trim() || null
    const state = (await loadCachedState(c)) as PersistedAppStateV2
    const buf = await deps.diagnostics.buildExportBundle({
      sessionId,
      appVersion: '',
      appState: state,
      debugTraceText: '',
    })
    const name = `droi-diagnostics${sessionId ? `-${sessionId}` : ''}.zip`
    return new Response(buf, {
      status: 200,
      headers: {
        'Content-Type': 'application/zip',
        'Content-Disposition': `attachment; filename="${name}"`,
      },
    })
  })

  api.post('/project-settings', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const repoRoot = typeof body.repoRoot === 'string' ? body.repoRoot.trim() : ''
      if (!repoRoot) return jsonError(c, 400, 'Missing repoRoot')

      const rawSettings = (body as any).settings || {}
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

      const cur = (await loadCachedState(c)) as PersistedAppStateV2
      const prevMap =
        (cur as any).projectSettings && typeof (cur as any).projectSettings === 'object'
          ? ((cur as any).projectSettings as Record<string, ProjectSettings>)
          : {}
      const merged: Record<string, ProjectSettings> = {
        ...prevMap,
        [repoRoot]: { ...(prevMap[repoRoot] || {}), ...settingsPatch },
      }

      deps.cachedStateRef.value = await deps.appStateStore.update({
        projectSettings: merged,
      } as any)
      return c.json(deps.cachedStateRef.value)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.get('/apikey', async (c) => {
    const state = await loadCachedState(c)
    return c.json({ apiKey: state.apiKey || '' })
  })

  api.post('/apikey', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const apiKey = typeof body.apiKey === 'string' ? body.apiKey : ''
      deps.cachedStateRef.value = await deps.appStateStore.update({ apiKey: apiKey || undefined })
      return c.json({ ok: true })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  // === Multi-Key Management ===

  api.get('/keys', async (c) => {
    const deps = c.get('deps')
    const [keys, usages] = await Promise.all([deps.keyStore.getKeys(), deps.keyStore.getUsages()])
    const state = await loadCachedState(c)
    const activeKey = state.apiKey || ''
    const entries = keys.map((entry, index) => ({
      key: entry.key,
      note: entry.note || '',
      addedAt: entry.addedAt,
      index,
      isActive: entry.key === activeKey,
      usage: usages.get(entry.key) || null,
    }))
    return c.json({ keys: entries })
  })

  api.post('/keys/add', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const rawKeys = Array.isArray(body.keys)
        ? body.keys.filter((k): k is string => typeof k === 'string')
        : []
      if (rawKeys.length === 0) return jsonError(c, 400, 'No keys provided')
      const result = await deps.keyStore.addKeys(rawKeys)
      deps.cachedStateRef.value = await deps.appStateStore.load()
      return c.json({ ok: true, ...result })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/keys/remove', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const index = typeof body.index === 'number' ? body.index : -1
      if (index < 0) return jsonError(c, 400, 'Invalid index')
      await deps.keyStore.removeKey(index)
      deps.cachedStateRef.value = await deps.appStateStore.load()
      return c.json({ ok: true })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/keys/note', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const index = typeof body.index === 'number' ? body.index : -1
      const note = typeof body.note === 'string' ? body.note : ''
      if (index < 0) return jsonError(c, 400, 'Invalid index')
      await deps.keyStore.updateNote(index, note)
      return c.json({ ok: true })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/keys/refresh', async (c) => {
    const deps = c.get('deps')
    const usages = await deps.keyStore.refreshUsages()
    const keys = await deps.keyStore.getKeys()
    const state = await loadCachedState(c)
    const activeKey = state.apiKey || ''
    const entries = keys.map((entry, index) => ({
      key: entry.key,
      note: entry.note || '',
      addedAt: entry.addedAt,
      index,
      isActive: entry.key === activeKey,
      usage: usages.get(entry.key) || null,
    }))
    return c.json({ ok: true, keys: entries })
  })

  api.get('/keys/active', async (c) => {
    const deps = c.get('deps')
    const key = await deps.keyStore.getActiveKey()
    if (key && deps.cachedStateRef.value.apiKey !== key) {
      deps.cachedStateRef.value = { ...deps.cachedStateRef.value, apiKey: key }
    }
    return c.json({ key: key || '', apiKeyFingerprint: key ? apiKeyFingerprint(key) : '' })
  })

  api.get('/project-dir', async (c) => {
    const state = await loadCachedState(c)
    return c.json({ dir: state.activeProjectDir || '' })
  })

  api.post('/project-dir', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir = typeof body.dir === 'string' ? body.dir : ''
      if (dir && !(await isDirectory(dir))) return jsonError(c, 400, 'Directory does not exist')

      deps.cachedStateRef.value = await deps.appStateStore.update({
        activeProjectDir: dir || undefined,
      })
      return c.json({ ok: true, dir: deps.cachedStateRef.value.activeProjectDir || '' })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/cancel', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sid = typeof body.sessionId === 'string' ? body.sessionId : ''
      if (sid) deps.execManager.cancel(sid)
    } catch {
      // ignore
    }
    return c.json({ ok: true })
  })

  api.get('/stream', async (c) => {
    const deps = c.get('deps')
    const sid = c.req.query('sessionId') || ''
    if (!sid) return jsonError(c, 400, 'Missing sessionId')

    const encoder = new TextEncoder()
    let unsubscribeExec: (() => void) | null = null
    let unsubscribeSetup: (() => void) | null = null
    let abortHandler: (() => void) | null = null
    let keepalive: ReturnType<typeof setInterval> | null = null
    let closed = false

    const listenIds = new Set<string>([sid])

    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        const write = (value: string) => {
          if (closed) return
          controller.enqueue(encoder.encode(value))
        }

        const close = () => {
          if (closed) return
          closed = true
          try {
            unsubscribeExec?.()
            unsubscribeSetup?.()
            if (keepalive) clearInterval(keepalive)
            if (abortHandler) c.req.raw.signal.removeEventListener('abort', abortHandler)
          } catch {
            // ignore
          }
          try {
            controller.close()
          } catch {
            // ignore
          }
        }

        write(':ok\n\n')

        keepalive = setInterval(() => {
          write(':ping\n\n')
        }, 15_000)

        unsubscribeExec = deps.execManager.onEvent((ev) => {
          if (closed) return
          if (ev.type === 'session-id-replaced') {
            if (!listenIds.has(ev.oldSessionId)) return
            listenIds.add(ev.newSessionId)
            write('event: session-id-replaced\n')
            write(
              `data:${JSON.stringify({ type: 'session-id-replaced', oldSessionId: ev.oldSessionId, newSessionId: ev.newSessionId, reason: ev.reason })}\n\n`,
            )
            return
          }

          if (!listenIds.has(ev.sessionId)) return
          if (ev.type === 'stdout') {
            write('event: stdout\n')
            write(`data:${JSON.stringify({ type: 'stdout', data: ev.data })}\n\n`)
          } else if (ev.type === 'stderr') {
            write('event: stderr\n')
            write(`data:${JSON.stringify({ type: 'stderr', data: ev.data })}\n\n`)
          } else if (ev.type === 'error') {
            write('event: error\n')
            write(`data:${JSON.stringify({ type: 'error', message: ev.message })}\n\n`)
          } else if (ev.type === 'debug') {
            write('event: debug\n')
            write(`data:${JSON.stringify({ type: 'debug', message: ev.message })}\n\n`)
          } else if (ev.type === 'rpc-notification') {
            write('event: rpc-notification\n')
            write(`data:${JSON.stringify({ type: 'rpc-notification', message: ev.message })}\n\n`)
          } else if (ev.type === 'rpc-request') {
            write('event: rpc-request\n')
            write(`data:${JSON.stringify({ type: 'rpc-request', message: ev.message })}\n\n`)
          } else if (ev.type === 'turn-end') {
            write('event: turn-end\n')
            write(`data:${JSON.stringify({ type: 'turn-end', code: ev.code })}\n\n`)
          }
        })

        unsubscribeSetup = deps.setupScriptRunner.onEvent((event) => {
          if (!listenIds.has(event.sessionId) || closed) return
          write('event: setup-script-event\n')
          write(`data:${JSON.stringify({ type: 'setup-script-event', event })}\n\n`)
        })

        abortHandler = () => {
          if (closed) return
          close()
        }
        c.req.raw.signal.addEventListener('abort', abortHandler, { once: true })
      },
      cancel() {
        if (closed) return
        closed = true
        unsubscribeExec?.()
        unsubscribeSetup?.()
        if (keepalive) clearInterval(keepalive)
        if (abortHandler) c.req.raw.signal.removeEventListener('abort', abortHandler)
      },
    })

    return new Response(stream, {
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      },
    })
  })

  api.post('/message', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const prompt = typeof body.prompt === 'string' ? body.prompt : ''
      const modelId = typeof body.modelId === 'string' ? body.modelId : undefined
      const autoLevel = typeof body.autoLevel === 'string' ? body.autoLevel : undefined
      const reasoningEffort =
        typeof body.reasoningEffort === 'string' ? body.reasoningEffort : undefined
      const sid = typeof body.sessionId === 'string' ? body.sessionId : ''

      if (!prompt.trim()) return jsonError(c, 400, 'Missing prompt')
      if (!sid) return jsonError(c, 400, 'Missing sessionId')

      const sig = deps.diagnostics.computePromptSig(prompt)
      deps.diagnostics.noteInputPromptSig(sid, sig)
      await deps.diagnostics.append({
        ts: new Date().toISOString(),
        level: 'info',
        scope: 'server',
        event: 'api.message.received',
        sessionId: sid,
        correlation: { modelId, autoLevel, reasoningEffort },
        data: {
          promptSig: sig,
          remoteAddress: c.req.header('x-forwarded-for') || c.req.header('x-real-ip') || '',
        },
      })

      const state = await loadCachedState(c)
      const cwd = state.activeProjectDir || ''
      if (!cwd || !(await isDirectory(cwd)))
        return jsonError(c, 400, 'No active project directory set')
      const machineId = (state as PersistedAppStateV2).machineId
      if (!machineId) return jsonError(c, 500, 'Missing machineId')

      const resumeSessionId = sid

      const env: Record<string, string | undefined> = { ...process.env }
      if (!deps.execManager.hasSession(sid)) {
        if (state.apiKey) {
          env['FACTORY_API_KEY'] = state.apiKey
        } else {
          const activeKey = await deps.keyStore.getActiveKey()
          if (activeKey) {
            env['FACTORY_API_KEY'] = activeKey
            if (deps.cachedStateRef.value.apiKey !== activeKey) {
              deps.cachedStateRef.value = { ...deps.cachedStateRef.value, apiKey: activeKey }
            }
          }
        }
      }

      await deps.execManager.send({
        sessionId: sid,
        resumeSessionId,
        machineId,
        prompt,
        cwd,
        modelId,
        interactionMode: toInteractionMode(autoLevel),
        autonomyLevel: toAutonomyLevel(autoLevel),
        reasoningEffort,
        env,
      })

      return c.json({ ok: true })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/exec', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const prompt = typeof body.prompt === 'string' ? body.prompt : ''
      const modelId = typeof body.modelId === 'string' ? body.modelId : undefined
      const autoLevel = typeof body.autoLevel === 'string' ? body.autoLevel : undefined
      const reasoningEffort =
        typeof body.reasoningEffort === 'string' ? body.reasoningEffort : undefined
      const sid = typeof body.sessionId === 'string' ? body.sessionId : ''

      if (!prompt.trim()) return jsonError(c, 400, 'Missing prompt')
      if (!sid) return jsonError(c, 400, 'Missing sessionId')

      const sig = deps.diagnostics.computePromptSig(prompt)
      deps.diagnostics.noteInputPromptSig(sid, sig)
      await deps.diagnostics.append({
        ts: new Date().toISOString(),
        level: 'info',
        scope: 'server',
        event: 'api.exec.stream.start',
        sessionId: sid,
        correlation: { modelId, autoLevel, reasoningEffort },
        data: {
          promptSig: sig,
          remoteAddress: c.req.header('x-forwarded-for') || c.req.header('x-real-ip') || '',
        },
      })

      const state = await loadCachedState(c)
      const cwd = state.activeProjectDir || ''
      if (!cwd || !(await isDirectory(cwd)))
        return jsonError(c, 400, 'No active project directory set')
      const machineId = (state as PersistedAppStateV2).machineId
      if (!machineId) return jsonError(c, 500, 'Missing machineId')

      const resumeSessionId = sid

      const env: Record<string, string | undefined> = { ...process.env }
      if (!deps.execManager.hasSession(sid)) {
        if (state.apiKey) {
          env['FACTORY_API_KEY'] = state.apiKey
        } else {
          const activeKey2 = await deps.keyStore.getActiveKey()
          if (activeKey2) {
            env['FACTORY_API_KEY'] = activeKey2
            if (deps.cachedStateRef.value.apiKey !== activeKey2) {
              deps.cachedStateRef.value = { ...deps.cachedStateRef.value, apiKey: activeKey2 }
            }
          }
        }
      }

      const encoder = new TextEncoder()
      let unsubscribe: (() => void) | null = null
      let abortHandler: (() => void) | null = null
      let closed = false

      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          const write = (value: string) => {
            if (closed) return
            controller.enqueue(encoder.encode(value))
          }

          const close = () => {
            if (closed) return
            closed = true
            try {
              unsubscribe?.()
              if (abortHandler) c.req.raw.signal.removeEventListener('abort', abortHandler)
            } catch {
              // ignore
            }
            try {
              controller.close()
            } catch {
              // ignore
            }
            void deps.diagnostics.append({
              ts: new Date().toISOString(),
              level: 'info',
              scope: 'server',
              event: 'api.exec.stream.end',
              sessionId: sid,
            })
          }

          const writeErrorAndClose = (message: string) => {
            write('event: error\n')
            write(`data:${JSON.stringify({ type: 'error', message })}\n\n`)
            write('event: turn-end\n')
            write(`data:${JSON.stringify({ type: 'turn-end', code: 1 })}\n\n`)
            close()
          }

          write(':ok\n\n')

          const listenIds = new Set<string>([sid])

          unsubscribe = deps.execManager.onEvent((ev) => {
            if (closed) return
            if (ev.type === 'session-id-replaced') {
              if (!listenIds.has(ev.oldSessionId)) return
              listenIds.add(ev.newSessionId)
              write('event: session-id-replaced\n')
              write(
                `data:${JSON.stringify({ type: 'session-id-replaced', oldSessionId: ev.oldSessionId, newSessionId: ev.newSessionId, reason: ev.reason })}\n\n`,
              )
              return
            }

            if (!listenIds.has(ev.sessionId)) return
            if (ev.type === 'stdout') {
              write('event: stdout\n')
              write(`data:${JSON.stringify({ type: 'stdout', data: ev.data })}\n\n`)
            } else if (ev.type === 'stderr') {
              write('event: stderr\n')
              write(`data:${JSON.stringify({ type: 'stderr', data: ev.data })}\n\n`)
            } else if (ev.type === 'error') {
              write('event: error\n')
              write(`data:${JSON.stringify({ type: 'error', message: ev.message })}\n\n`)
            } else if (ev.type === 'debug') {
              write('event: debug\n')
              write(`data:${JSON.stringify({ type: 'debug', message: ev.message })}\n\n`)
            } else if (ev.type === 'rpc-notification') {
              write('event: rpc-notification\n')
              write(`data:${JSON.stringify({ type: 'rpc-notification', message: ev.message })}\n\n`)
            } else if (ev.type === 'rpc-request') {
              write('event: rpc-request\n')
              write(`data:${JSON.stringify({ type: 'rpc-request', message: ev.message })}\n\n`)
            } else if (ev.type === 'turn-end') {
              write('event: turn-end\n')
              write(`data:${JSON.stringify({ type: 'turn-end', code: ev.code })}\n\n`)
              close()
            }
          })

          abortHandler = () => {
            if (closed) return
            closed = true
            unsubscribe?.()
          }
          c.req.raw.signal.addEventListener('abort', abortHandler, { once: true })

          void deps.execManager
            .send({
              sessionId: sid,
              resumeSessionId,
              machineId,
              prompt,
              cwd,
              modelId,
              interactionMode: toInteractionMode(autoLevel),
              autonomyLevel: toAutonomyLevel(autoLevel),
              reasoningEffort,
              env,
            })
            .catch((err) => {
              writeErrorAndClose((err as Error)?.message || 'Execution failed')
            })
        },
        cancel() {
          if (closed) return
          closed = true
          unsubscribe?.()
          if (abortHandler) c.req.raw.signal.removeEventListener('abort', abortHandler)
        },
      })

      return new Response(stream, {
        status: 200,
        headers: {
          'Content-Type': 'text/event-stream',
          'Cache-Control': 'no-cache',
          Connection: 'keep-alive',
        },
      })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/rpc/permission-response', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sessionId = typeof body.sessionId === 'string' ? body.sessionId : ''
      const requestId = typeof body.requestId === 'string' ? body.requestId : ''
      const selectedOption = body.selectedOption as DroidPermissionOption
      if (!sessionId || !requestId || typeof selectedOption !== 'string') {
        return jsonError(c, 400, 'Invalid payload')
      }
      deps.execManager.respondPermission({ sessionId, requestId, selectedOption })
      return c.json({ ok: true })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/rpc/session-settings', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sessionId = typeof body.sessionId === 'string' ? body.sessionId : ''
      const modelId = typeof body.modelId === 'string' ? body.modelId : undefined
      const autoLevel = typeof body.autoLevel === 'string' ? body.autoLevel : undefined
      const reasoningEffort =
        typeof body.reasoningEffort === 'string' ? body.reasoningEffort : undefined
      if (!sessionId) return jsonError(c, 400, 'Invalid payload')

      await deps.execManager.updateSessionSettings({
        sessionId,
        modelId,
        autonomyLevel: typeof autoLevel === 'string' ? toAutonomyLevel(autoLevel) : undefined,
        reasoningEffort,
      })

      return c.json({ ok: true } as const)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/rpc/askuser-response', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sessionId = typeof body.sessionId === 'string' ? body.sessionId : ''
      const requestId = typeof body.requestId === 'string' ? body.requestId : ''
      const cancelled = typeof body.cancelled === 'boolean' ? body.cancelled : undefined
      const answers = Array.isArray(body.answers) ? body.answers : null
      if (!sessionId || !requestId || !answers) return jsonError(c, 400, 'Invalid payload')

      deps.execManager.respondAskUser({ sessionId, requestId, cancelled, answers: answers as any })
      return c.json({ ok: true })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/session/setup/run', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sessionId = typeof body.sessionId === 'string' ? body.sessionId.trim() : ''
      const projectDir = typeof body.projectDir === 'string' ? body.projectDir.trim() : ''
      const script = typeof body.script === 'string' ? body.script.trim() : ''
      if (!sessionId || !projectDir || !script)
        return jsonError(c, 400, 'Missing sessionId/projectDir/script')
      if (!(await isDirectory(projectDir))) return jsonError(c, 400, 'Invalid projectDir')

      await deps.setupScriptRunner.run({ sessionId, projectDir, script })
      return c.json({ ok: true } as const)
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to run setup script')
    }
  })

  api.post('/session/setup/cancel', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sessionId = typeof body.sessionId === 'string' ? body.sessionId.trim() : ''
      if (!sessionId) return jsonError(c, 400, 'Missing sessionId')
      deps.setupScriptRunner.cancel(sessionId)
      return c.json({ ok: true } as const)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/session/save', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<SaveSessionRequest>(c)
      const meta = await deps.sessionStore.save(body)
      if (!meta) return jsonError(c, 400, 'Invalid session payload')
      return c.json(meta)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.get('/session/load', async (c) => {
    const deps = c.get('deps')
    const id = c.req.query('id') || ''
    const data = await deps.sessionStore.load(id)
    return c.json(data)
  })

  api.post('/session/create', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const cwd = typeof body.cwd === 'string' ? body.cwd.trim() : ''
      const modelId = typeof body.modelId === 'string' ? body.modelId : undefined
      const autoLevel = typeof body.autoLevel === 'string' ? body.autoLevel : undefined
      const reasoningEffort =
        typeof body.reasoningEffort === 'string' ? body.reasoningEffort : undefined
      if (!cwd) return jsonError(c, 400, 'Missing cwd')
      if (!(await isDirectory(cwd))) return jsonError(c, 400, 'Invalid cwd')

      const state = await loadCachedState(c)
      const machineId = (state as PersistedAppStateV2).machineId
      if (!machineId) return jsonError(c, 500, 'Missing machineId')

      const env: Record<string, string | undefined> = { ...process.env }
      const activeKey = await deps.keyStore.getActiveKey()
      if (activeKey) {
        env['FACTORY_API_KEY'] = activeKey
        if (deps.cachedStateRef.value.apiKey !== activeKey) {
          deps.cachedStateRef.value = { ...deps.cachedStateRef.value, apiKey: activeKey }
        }
      } else if (state.apiKey) env['FACTORY_API_KEY'] = state.apiKey

      const res = await deps.execManager.createSession({
        machineId,
        cwd,
        modelId,
        interactionMode: toInteractionMode(autoLevel),
        autonomyLevel: toAutonomyLevel(autoLevel),
        reasoningEffort,
        env,
      })
      return c.json(res)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/session/restart', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const sessionId = typeof body.sessionId === 'string' ? body.sessionId.trim() : ''
      if (!sessionId) return jsonError(c, 400, 'Missing sessionId')

      deps.execManager.disposeSession(sessionId)

      const state = await loadCachedState(c)
      const key = state.apiKey || ''
      return c.json({ ok: true, apiKeyFingerprint: key ? apiKeyFingerprint(key) : '' })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/session/clear', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const id = typeof body.id === 'string' ? body.id.trim() : ''
      if (!id) return jsonError(c, 400, 'Missing id')

      const existing = await deps.sessionStore.load(id)
      const state = await loadCachedState(c)
      const cwd = (existing?.projectDir || '').trim() || (state.activeProjectDir || '').trim()
      if (!cwd || !(await isDirectory(cwd)))
        return jsonError(c, 400, 'No active project directory set')
      const machineId = (state as PersistedAppStateV2).machineId
      if (!machineId) return jsonError(c, 500, 'Missing machineId')

      deps.execManager.disposeSession(id)

      const env: Record<string, string | undefined> = { ...process.env }
      const activeKey = await deps.keyStore.getActiveKey()
      if (activeKey) {
        env['FACTORY_API_KEY'] = activeKey
        if (deps.cachedStateRef.value.apiKey !== activeKey) {
          deps.cachedStateRef.value = { ...deps.cachedStateRef.value, apiKey: activeKey }
        }
      } else if (state.apiKey) env['FACTORY_API_KEY'] = state.apiKey

      const created = await deps.execManager.createSession({
        machineId,
        cwd,
        modelId: existing?.model || undefined,
        interactionMode: toInteractionMode(existing?.autoLevel),
        autonomyLevel: existing?.autoLevel ? toAutonomyLevel(existing.autoLevel) : undefined,
        reasoningEffort: existing?.reasoningEffort || undefined,
        env,
      })

      const meta = await deps.sessionStore.replaceSessionId(id, created.sessionId)
      if (!meta) return jsonError(c, 400, 'Invalid session id')
      return c.json(meta)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.get('/session/list', async (c) => {
    const deps = c.get('deps')
    const list = await deps.sessionStore.list()
    return c.json(list)
  })

  api.post('/session/delete', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const id = typeof body.id === 'string' ? body.id : ''
      const ok = await deps.sessionStore.delete(id)
      return c.json({ ok })
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.post('/git-status', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      if (!dir) return c.json([])

      const files = await new Promise<
        Array<{ status: string; path: string; additions: number; deletions: number }>
      >((resolvePromise) => {
        execFile(
          'git',
          ['status', '--porcelain', '-uall'],
          { cwd: dir, timeout: 5000 },
          (err, stdout) => {
            if (err) {
              resolvePromise([])
              return
            }

            const statusMap = new Map<string, string>()
            for (const line of stdout.split('\n').filter((line2) => line2.trim())) {
              const status = line.substring(0, 2).trim()
              const filePath = line.substring(3)
              statusMap.set(filePath, status)
            }

            if (statusMap.size === 0) {
              resolvePromise([])
              return
            }

            execFile(
              'git',
              ['diff', '--numstat', 'HEAD'],
              { cwd: dir, timeout: 5000 },
              (err2, diffOut) => {
                const diffMap = new Map<string, { additions: number; deletions: number }>()
                if (!err2 && diffOut) {
                  for (const line of diffOut.split('\n').filter((line2) => line2.trim())) {
                    const parts = line.split('\t')
                    if (parts.length >= 3) {
                      const add = parts[0] === '-' ? 0 : parseInt(parts[0], 10) || 0
                      const del = parts[1] === '-' ? 0 : parseInt(parts[1], 10) || 0
                      diffMap.set(parts[2], { additions: add, deletions: del })
                    }
                  }
                }

                execFile(
                  'git',
                  ['diff', '--numstat'],
                  { cwd: dir, timeout: 5000 },
                  (_err3, unstagedOut) => {
                    if (unstagedOut) {
                      for (const line of unstagedOut.split('\n').filter((line2) => line2.trim())) {
                        const parts = line.split('\t')
                        if (parts.length >= 3 && !diffMap.has(parts[2])) {
                          const add = parts[0] === '-' ? 0 : parseInt(parts[0], 10) || 0
                          const del = parts[1] === '-' ? 0 : parseInt(parts[1], 10) || 0
                          diffMap.set(parts[2], { additions: add, deletions: del })
                        }
                      }
                    }

                    const result = Array.from(statusMap.entries()).map(([filePath, status]) => {
                      const diff = diffMap.get(filePath) || { additions: 0, deletions: 0 }
                      return {
                        status,
                        path: filePath,
                        additions: diff.additions,
                        deletions: diff.deletions,
                      }
                    })
                    resolvePromise(result)
                  },
                )
              },
            )
          },
        )
      })

      return c.json(files)
    } catch {
      return c.json([])
    }
  })

  api.post('/git-branch', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      if (!dir) return c.json({ branch: '' })

      const branch = await new Promise<string>((resolvePromise) => {
        execFile(
          'git',
          ['rev-parse', '--abbrev-ref', 'HEAD'],
          { cwd: dir, timeout: 5000 },
          (err, stdout) => {
            if (err) {
              resolvePromise('')
              return
            }
            resolvePromise(stdout.trim())
          },
        )
      })
      return c.json({ branch })
    } catch {
      return c.json({ branch: '' })
    }
  })

  api.post('/git-branches', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      if (!dir) return c.json({ branches: [] })
      const branches = await listBranches(dir)
      return c.json({ branches })
    } catch {
      return c.json({ branches: [] })
    }
  })

  api.post('/git-workspace-info', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      if (!dir) return c.json(null)
      const info = await getWorkspaceInfo(dir)
      return c.json(info)
    } catch (err) {
      if (isNotGitRepoError(err)) return c.json(null)
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to resolve workspace info')
    }
  })

  api.post('/git-switch-workspace', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      const branch = typeof body.branch === 'string' ? body.branch.trim() : ''
      if (!dir || !branch) return c.json(null)

      return c.json(await switchWorkspaceBranch({ projectDir: dir, branch }))
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to switch workspace')
    }
  })

  api.post('/git-create-workspace', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      const mode = body.mode === 'worktree' ? 'worktree' : 'branch'
      const branch = typeof body.branch === 'string' ? body.branch.trim() : ''
      const baseBranch = typeof body.baseBranch === 'string' ? body.baseBranch.trim() : undefined
      const useExistingBranch = Boolean(body.useExistingBranch)
      if (!dir || !branch) return c.json(null)

      return c.json(
        await createWorkspace({ projectDir: dir, mode, branch, baseBranch, useExistingBranch }),
      )
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to create workspace')
    }
  })

  api.post('/git-worktree-branches-in-use', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const repoRoot =
        typeof body.repoRoot === 'string'
          ? body.repoRoot.trim()
          : deps.cachedStateRef.value.activeProjectDir || ''
      if (!repoRoot) return c.json([])
      return c.json(await listWorktreeBranchesInUse({ repoRoot }))
    } catch {
      return c.json([])
    }
  })

  api.post('/git-remove-worktree', async (c) => {
    try {
      const body = await readJsonBody<Record<string, unknown>>(c)
      const repoRoot = typeof body.repoRoot === 'string' ? body.repoRoot.trim() : ''
      const worktreeDir = typeof body.worktreeDir === 'string' ? body.worktreeDir.trim() : ''
      const force = Boolean(body.force)
      if (!repoRoot || !worktreeDir) return jsonError(c, 400, 'Missing repoRoot/worktreeDir')

      const deleteBranch = Boolean((body as any).deleteBranch)
      const branch =
        typeof (body as any).branch === 'string' ? String((body as any).branch).trim() : ''
      await removeWorktree({ repoRoot, worktreeDir, force, deleteBranch, branch })
      return c.json({ ok: true })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to remove worktree')
    }
  })

  api.post('/git-push-branch', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const dir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      const remote = typeof body.remote === 'string' ? body.remote.trim() : undefined
      const branch = typeof body.branch === 'string' ? body.branch.trim() : undefined
      if (!dir) return jsonError(c, 400, 'Missing projectDir')

      const result = await pushBranch({ projectDir: dir, remote, branch })
      return c.json({ ok: true, remote: result.remote, branch: result.branch })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to push branch')
    }
  })

  api.post('/git-detect-tools', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const projectDir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      if (!projectDir) return jsonError(c, 400, 'Missing projectDir')
      return c.json(await detectGitTools({ projectDir }))
    } catch {
      return c.json({ hasGh: false, hasFlow: false, prTool: null })
    }
  })

  api.post('/git-generate-commit-meta', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const projectDir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      const includeUnstaged = Boolean(body.includeUnstaged)
      const wantPrMeta = Boolean(body.wantPrMeta)
      const prBaseBranch =
        typeof body.prBaseBranch === 'string' ? body.prBaseBranch.trim() : undefined
      if (!projectDir) return jsonError(c, 400, 'Missing projectDir')

      const state = await loadCachedState(c)
      const req: GenerateCommitMetaRequest = {
        projectDir,
        includeUnstaged,
        wantPrMeta,
        prBaseBranch,
      }
      const result = await generateCommitMeta({
        req,
        state,
        execManager: deps.execManager,
        keyStore: deps.keyStore,
      })
      return c.json(result)
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to generate commit metadata')
    }
  })

  api.post('/git-commit-workflow', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const projectDir =
        typeof body.projectDir === 'string'
          ? body.projectDir
          : deps.cachedStateRef.value.activeProjectDir || ''
      if (!projectDir) return jsonError(c, 400, 'Missing projectDir')
      const req: CommitWorkflowRequest = {
        projectDir,
        includeUnstaged: Boolean(body.includeUnstaged),
        commitMessage: typeof body.commitMessage === 'string' ? body.commitMessage : '',
        workflow:
          body.workflow === 'commit_push' || body.workflow === 'commit_push_pr'
            ? body.workflow
            : 'commit',
        prBaseBranch: typeof body.prBaseBranch === 'string' ? body.prBaseBranch.trim() : undefined,
        prTitle: typeof body.prTitle === 'string' ? body.prTitle : undefined,
        prBody: typeof body.prBody === 'string' ? body.prBody : undefined,
        mergeEnabled: Boolean(body.mergeEnabled),
        mergeBranch: typeof body.mergeBranch === 'string' ? body.mergeBranch.trim() : undefined,
      }
      const result = await commitWorkflow(req)
      return c.json(result)
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      return jsonError(c, 500, msg || 'Failed to run commit workflow')
    }
  })

  api.get('/slash/commands', async (c) => {
    try {
      const deps = c.get('deps')
      const projectDir = deps.cachedStateRef.value.activeProjectDir || ''
      const commands = await scanSlashCommands({ projectDir })
      const defs = Array.from(commands.values()).map(({ body: _, ...def }) => def)
      return c.json(defs)
    } catch {
      return c.json([])
    }
  })

  api.get('/skills', async (c) => {
    try {
      const deps = c.get('deps')
      const projectDir = deps.cachedStateRef.value.activeProjectDir || ''
      const skillList = await scanSkills({ projectDir })
      return c.json(skillList)
    } catch {
      return c.json([])
    }
  })

  api.post('/slash/resolve', async (c) => {
    try {
      const deps = c.get('deps')
      const body = await readJsonBody<Record<string, unknown>>(c)
      const text = typeof body.text === 'string' ? body.text : ''
      const projectDir = deps.cachedStateRef.value.activeProjectDir || ''
      const commands = await scanSlashCommands({ projectDir })
      const result = resolveSlashCommandText({ text, commands, projectDir })
      return c.json(result)
    } catch {
      return jsonError(c, 400, 'Invalid JSON')
    }
  })

  api.get('/custom-models', async (c) => {
    try {
      const settingsPath = join(homedir(), '.factory', 'settings.json')
      const raw = JSON.parse(await readFile(settingsPath, 'utf-8'))
      const models = Array.isArray(raw?.customModels) ? raw.customModels : []
      const result = models
        .filter((m: any) => typeof m?.id === 'string' && typeof m?.displayName === 'string')
        .map((m: any) => ({
          id: m.id,
          displayName: m.displayName,
          model: String(m.model || ''),
          provider: String(m.provider || 'custom'),
        }))
      return c.json(result)
    } catch {
      return c.json([])
    }
  })

  api.get('/file', async (c) => {
    const filePath = c.req.query('path') || ''
    if (!filePath) return jsonError(c, 400, 'Missing path')

    const resolvedPath = resolve(filePath)
    try {
      const s = await stat(resolvedPath)
      if (!s.isFile()) return jsonError(c, 404, 'Not a file')

      const passthrough = new PassThrough()
      void pipelineNode(createReadStream(resolvedPath), passthrough).catch((err) => {
        passthrough.destroy(err as Error)
      })

      return new Response(nodeToWebReadable(passthrough), {
        status: 200,
        headers: {
          'Content-Type': getContentType(resolvedPath),
          'Cache-Control': 'public, max-age=3600',
        },
      })
    } catch {
      return jsonError(c, 404, 'File not found')
    }
  })

  api.post('/upload', async (c) => {
    try {
      const deps = c.get('deps')
      const projectDir =
        c.req.query('projectDir') || deps.cachedStateRef.value.activeProjectDir || ''
      if (!projectDir) return jsonError(c, 400, 'No project directory')

      const attachDir = join(projectDir, '.attachment')
      const files = await saveMultipartFiles({ request: c.req.raw, attachDir })
      return c.json(files)
    } catch (err) {
      if (err instanceof UploadError) return jsonError(c, err.status, err.message)
      return jsonError(c, 400, 'Upload failed')
    }
  })

  return api
}
