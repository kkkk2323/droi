import React, { useEffect, useRef } from 'react'
import { getDroidClient } from '@/droidClient'
import type { ChatMessage, Project, SessionMeta, ProjectSettings } from '@/types'
import type { CustomModelDef } from '@/types'
import { getMissingDroidHooks } from '@/lib/droidHooks'
import { uuidv4 } from '@/lib/uuid'
import { defaultSessionTitleFromBranch } from '@/lib/sessionWorktree'
import {
  formatNotificationTrace,
  isTraceChainEnabled,
  setTraceChainEnabledOverride,
} from '@/lib/notificationFingerprint'
import {
  DEFAULT_AUTO_LEVEL,
  DEFAULT_MODEL,
  makeBuffer,
  applySetupScriptEvent,
  applyRpcNotification,
  applyRpcRequest,
  appendDebugTrace,
  setDebugTraceMaxLinesOverride,
  applyStdout,
  applyStderr,
  applyTurnEnd,
  applyError,
} from '@/state/appReducer'
import { useAppStore } from './store'
import {
  getRepoKey,
  upsertSessionMeta,
  updateSessionTitle,
  replaceSessionIdInProjects,
} from './store/projectHelpers'

const droid = getDroidClient()

export function AppInitializer({ children }: { children: React.ReactNode }) {
  const initializedRef = useRef(false)

  useEffect(() => {
    if (initializedRef.current) return
    initializedRef.current = true

    const store = useAppStore.getState()

    void (async () => {
      try {
        const [version, appVersion, state, sessionMetas, loadedCustomModels, diagDir] =
          await Promise.all([
            droid.getVersion(),
            droid.getAppVersion(),
            droid.loadAppState(),
            droid.listSessions(),
            droid.getCustomModels().catch(() => [] as CustomModelDef[]),
            typeof (droid as any)?.getDiagnosticsDir === 'function'
              ? (droid as any).getDiagnosticsDir().catch(() => '')
              : Promise.resolve(''),
          ])

        const traceEnabled =
          typeof (state as any).traceChainEnabled === 'boolean'
            ? Boolean((state as any).traceChainEnabled)
            : isTraceChainEnabled()
        setTraceChainEnabledOverride(traceEnabled)
        if (typeof (droid as any)?.setTraceChainEnabled === 'function') {
          ;(droid as any).setTraceChainEnabled(traceEnabled)
        }

        const showDebug =
          typeof (state as any).showDebugTrace === 'boolean'
            ? Boolean((state as any).showDebugTrace)
            : false
        const debugTraceMaxLines =
          typeof (state as any).debugTraceMaxLines === 'number' &&
          Number.isFinite((state as any).debugTraceMaxLines)
            ? Math.min(10_000, Math.max(1, Math.floor((state as any).debugTraceMaxLines)))
            : null
        setDebugTraceMaxLinesOverride(debugTraceMaxLines)
        const diagEnabled =
          typeof (state as any).localDiagnosticsEnabled === 'boolean'
            ? Boolean((state as any).localDiagnosticsEnabled)
            : true
        const retentionDays =
          typeof (state as any).localDiagnosticsRetentionDays === 'number' &&
          Number.isFinite((state as any).localDiagnosticsRetentionDays)
            ? Math.max(1, Math.floor((state as any).localDiagnosticsRetentionDays))
            : 7
        const maxTotalMb =
          typeof (state as any).localDiagnosticsMaxTotalMb === 'number' &&
          Number.isFinite((state as any).localDiagnosticsMaxTotalMb)
            ? Math.max(1, Math.floor((state as any).localDiagnosticsMaxTotalMb))
            : 50
        const commitModel =
          typeof (state as any).commitMessageModelId === 'string'
            ? String((state as any).commitMessageModelId || '').trim()
            : ''
        const lanAccess =
          typeof (state as any).lanAccessEnabled === 'boolean'
            ? Boolean((state as any).lanAccessEnabled)
            : false
        const ps = (state as any)?.projectSettings

        useAppStore.setState({
          appVersion: appVersion,
          droidVersion: version,
          customModels: loadedCustomModels,
          apiKey: state.apiKey || '',
          traceChainEnabled: traceEnabled,
          showDebugTrace: showDebug,
          debugTraceMaxLines,
          localDiagnosticsEnabled: diagEnabled,
          localDiagnosticsRetentionDays: retentionDays,
          localDiagnosticsMaxTotalMb: maxTotalMb,
          diagnosticsDir: typeof diagDir === 'string' ? diagDir : '',
          commitMessageModelId: commitModel || 'minimax-m2.5',
          lanAccessEnabled: lanAccess,
          ...(ps && typeof ps === 'object'
            ? { projectSettingsByRepo: ps as Record<string, ProjectSettings> }
            : {}),
        })

        const persistedProjects = (state.projects || []).map((p) => ({
          dir: p.dir,
          name: p.name,
          sessions: [] as SessionMeta[],
        }))
        const fallbackProjectDir = state.activeProjectDir || persistedProjects[0]?.dir || ''
        const projectsByDir = new Map<string, Project>()
        for (const p of persistedProjects) projectsByDir.set(p.dir, p)

        const normalizedMetas = (
          await Promise.all(
            sessionMetas.map(async (meta): Promise<SessionMeta | null> => {
              const guessedDir = meta.projectDir || fallbackProjectDir
              if (!guessedDir) return null
              const info = await store._resolveWorkspace(guessedDir).catch(() => null)
              const repoRoot = info?.repoRoot || meta.repoRoot || guessedDir
              const projectDir = info?.projectDir || meta.projectDir || guessedDir
              return {
                ...meta,
                repoRoot,
                projectDir,
                branch: meta.branch || info?.branch,
                workspaceType: info?.workspaceType || meta.workspaceType,
                autoLevel: meta.autoLevel || DEFAULT_AUTO_LEVEL,
              }
            }),
          )
        ).filter(Boolean) as SessionMeta[]

        for (const meta of normalizedMetas) {
          const repoRoot = getRepoKey(meta)
          if (!repoRoot) continue
          const existing = projectsByDir.get(repoRoot)
          if (existing) existing.sessions.push(meta)
          else {
            const name = repoRoot.split(/[\\/]/).pop() || repoRoot
            projectsByDir.set(repoRoot, { dir: repoRoot, name, sessions: [meta] })
          }
        }
        for (const p of projectsByDir.values())
          p.sessions.sort((a, b) => (b.lastMessageAt ?? b.savedAt) - (a.lastMessageAt ?? a.savedAt))

        const nextProjects = Array.from(projectsByDir.values())

        const matchedByActiveDir = normalizedMetas
          .filter((m) => m.projectDir === state.activeProjectDir)
          .sort((a, b) => (b.lastMessageAt ?? b.savedAt) - (a.lastMessageAt ?? a.savedAt))[0]
        const fallbackLatest = normalizedMetas.sort(
          (a, b) => (b.lastMessageAt ?? b.savedAt) - (a.lastMessageAt ?? a.savedAt),
        )[0]
        const activeMeta = matchedByActiveDir || fallbackLatest

        const restoredProjectDir = activeMeta?.projectDir || state.activeProjectDir || ''
        droid.setProjectDir(restoredProjectDir || null)

        if (restoredProjectDir && activeMeta) {
          const data = await droid.loadSession(activeMeta.id)
          const loaded = (data?.messages as ChatMessage[]) ?? []
          const newBuffers = new Map(useAppStore.getState().sessionBuffers)
          const base = makeBuffer(restoredProjectDir, {
            repoRoot: activeMeta.repoRoot,
            branch: (data as any)?.branch || activeMeta.branch,
            workspaceType: (data as any)?.workspaceType || activeMeta.workspaceType,
            baseBranch: (data as any)?.baseBranch || activeMeta.baseBranch,
          })
          newBuffers.set(activeMeta.id, {
            ...base,
            messages: loaded,
            model: data?.model || activeMeta.model || DEFAULT_MODEL,
            autoLevel: data?.autoLevel || activeMeta.autoLevel || DEFAULT_AUTO_LEVEL,
            reasoningEffort: (data as any)?.reasoningEffort || '',
            apiKeyFingerprint: (data as any)?.apiKeyFingerprint || activeMeta.apiKeyFingerprint,
          })
          useAppStore.setState({
            projects: nextProjects,
            activeProjectDir: restoredProjectDir,
            activeSessionId: activeMeta.id,
            sessionBuffers: newBuffers,
            _initialLoadDone: true,
          })
        } else {
          const restoredInfo = restoredProjectDir
            ? await store._resolveWorkspace(restoredProjectDir).catch(() => null)
            : null
          const newId = restoredProjectDir
            ? (
                await droid.createSession({
                  cwd: restoredProjectDir,
                  modelId: DEFAULT_MODEL,
                  autoLevel: DEFAULT_AUTO_LEVEL,
                })
              ).sessionId
            : uuidv4()
          const initialBuffer = makeBuffer(restoredProjectDir || '', {
            repoRoot: restoredInfo?.repoRoot,
            branch: restoredInfo?.branch,
            workspaceType: restoredInfo?.workspaceType,
            baseBranch: restoredInfo?.baseBranch,
          })
          const newBuffers = new Map(useAppStore.getState().sessionBuffers)
          newBuffers.set(newId, initialBuffer)

          const projectsWithInitialSession = restoredProjectDir
            ? upsertSessionMeta(nextProjects, {
                id: newId,
                projectDir: restoredProjectDir,
                repoRoot: restoredInfo?.repoRoot || restoredProjectDir,
                branch: restoredInfo?.branch,
                workspaceType: restoredInfo?.workspaceType,
                baseBranch: restoredInfo?.baseBranch,
                title: defaultSessionTitleFromBranch(restoredInfo?.branch || ''),
                savedAt: Date.now(),
                messageCount: 0,
                model: DEFAULT_MODEL,
                autoLevel: DEFAULT_AUTO_LEVEL,
              })
            : nextProjects

          if (restoredProjectDir) {
            void droid.saveSession({
              id: newId,
              projectDir: restoredProjectDir,
              repoRoot: restoredInfo?.repoRoot,
              branch: restoredInfo?.branch,
              workspaceType: restoredInfo?.workspaceType,
              baseBranch: restoredInfo?.baseBranch,
              model: DEFAULT_MODEL,
              autoLevel: DEFAULT_AUTO_LEVEL,
              messages: [],
            })
          }

          useAppStore.setState({
            projects: projectsWithInitialSession,
            activeProjectDir: restoredProjectDir,
            activeSessionId: newId,
            sessionBuffers: newBuffers,
            _initialLoadDone: true,
          })
        }
      } finally {
        useAppStore.setState({ _initialLoadDone: true })
        try {
          const cur = useAppStore.getState().projects
          if (Array.isArray(cur) && cur.length > 0) {
            droid.saveProjects(cur.map((p) => ({ dir: p.dir, name: p.name })))
          }
        } catch {
          // ignore
        }
      }
    })()
  }, [])

  useEffect(() => {
    let prevProjects = useAppStore.getState().projects
    return useAppStore.subscribe((state) => {
      if (!state._initialLoadDone) return
      if (state.projects === prevProjects) return
      prevProjects = state.projects
      droid.saveProjects(state.projects.map((p) => ({ dir: p.dir, name: p.name })))
    })
  }, [])

  useEffect(() => {
    let prevSessionId = useAppStore.getState().activeSessionId
    return useAppStore.subscribe((state) => {
      if (state.activeSessionId === prevSessionId) return
      prevSessionId = state.activeSessionId
      try {
        droid.setActiveSession({ sessionId: state.activeSessionId || null })
      } catch {
        // ignore
      }
    })
  }, [])

  useEffect(() => {
    const missingHooks = getMissingDroidHooks(droid)
    if (missingHooks.length > 0) {
      const s = useAppStore.getState()
      if (s.activeSessionId && s.activeProjectDir) {
        s._reportHookMismatch(s.activeSessionId, s.activeProjectDir, missingHooks)
      }
      return
    }

    const onDebug = (droid as any)?.onDebug

    const unsubNotif = droid.onRpcNotification(({ message, sessionId: sid }) => {
      if (!sid) return
      const t =
        (message as any)?.method === 'droid.session_notification'
          ? String((message as any)?.params?.notification?.type || '')
          : ''
      const label = t
        ? `rpc-notification: ${message.method} type=${t}`
        : `rpc-notification: ${message.method}`
      useAppStore.getState()._setSessionBuffers((prev) => {
        let next = prev
        if (isTraceChainEnabled())
          next = appendDebugTrace(next, sid, formatNotificationTrace('renderer-in', message))
        next = appendDebugTrace(next, sid, label)
        return applyRpcNotification(next, sid, message)
      })

      if (t === 'session_title_updated') {
        const title = String((message as any)?.params?.notification?.title || '').trim()
        if (title) {
          useAppStore.getState()._setProjects((prev) => updateSessionTitle(prev, sid, title))
        }
      }
    })

    const unsubReq = droid.onRpcRequest(({ message, sessionId: sid }) => {
      if (!sid) return
      useAppStore
        .getState()
        ._setSessionBuffers((prev) =>
          applyRpcRequest(
            appendDebugTrace(prev, sid, `rpc-request: ${message.method} id=${message.id}`),
            sid,
            message,
          ),
        )
    })

    const unsubStdout = droid.onStdout(({ data, sessionId: sid }) => {
      if (!sid) return
      useAppStore
        .getState()
        ._setSessionBuffers((prev) =>
          applyStdout(
            appendDebugTrace(prev, sid, `stdout: ${String(data || '').slice(0, 200)}`),
            sid,
            data,
          ),
        )
    })

    const unsubStderr = droid.onStderr(({ data, sessionId: sid }) => {
      if (!sid) return
      useAppStore
        .getState()
        ._setSessionBuffers((prev) =>
          applyStderr(
            appendDebugTrace(prev, sid, `stderr: ${String(data || '').slice(0, 200)}`),
            sid,
            data,
          ),
        )
    })

    const unsubTurnEnd = droid.onTurnEnd(({ sessionId: sid }) => {
      if (!sid) return
      useAppStore
        .getState()
        ._setSessionBuffers((prev) => applyTurnEnd(appendDebugTrace(prev, sid, 'turn-end'), sid))

      const state = useAppStore.getState()
      const snapshot = state.sessionBuffers.get(sid)
      if (snapshot) void state._saveSessionToDisk(sid, snapshot)

      if (
        snapshot?.pendingApiKeyFingerprint &&
        !snapshot.isSetupRunning &&
        sid === state.activeSessionId
      ) {
        const targetFp = snapshot.pendingApiKeyFingerprint
        void (async () => {
          try {
            await droid.restartSessionWithActiveKey({ sessionId: sid })
            state._setSessionBuffers((prev) => {
              const session = prev.get(sid)
              if (!session) return prev
              const next = new Map(prev)
              next.set(sid, {
                ...session,
                apiKeyFingerprint: targetFp,
                pendingApiKeyFingerprint: undefined,
              })
              return appendDebugTrace(next, sid, `api-key-restarted-after-turn: fp=${targetFp}`)
            })
          } catch (err) {
            const msg = err instanceof Error ? err.message : String(err)
            state._setSessionBuffers((prev) =>
              appendDebugTrace(prev, sid, `api-key-restart-after-turn-failed: ${msg}`),
            )
          }
        })()
      }
    })

    const unsubError = droid.onError(({ message, sessionId: sid }) => {
      if (!sid) return
      useAppStore
        .getState()
        ._setSessionBuffers((prev) =>
          applyError(appendDebugTrace(prev, sid, `error: ${message}`), sid, message),
        )
    })

    const unsubSetup = droid.onSetupScriptEvent(({ event, sessionId: sid }) => {
      if (!sid) return
      useAppStore.getState()._setSessionBuffers((prev) => {
        let next = prev
        if (event.type === 'output') {
          const content = String(event.data || '')
            .replace(/\s+/g, ' ')
            .trim()
            .slice(0, 200)
          if (content) next = appendDebugTrace(next, sid, `setup-${event.stream}: ${content}`)
        } else if (event.type === 'started') {
          next = appendDebugTrace(next, sid, `setup-started: cwd=${event.projectDir}`)
        } else {
          next = appendDebugTrace(
            next,
            sid,
            `setup-finished: success=${event.success} code=${event.exitCode}`,
          )
        }
        return applySetupScriptEvent(next, sid, event)
      })

      if (event.type === 'finished') {
        useAppStore.getState()._maybeFlushPendingInitialSend()
      }
    })

    const unsubSessionReplace =
      typeof (droid as any)?.onSessionIdReplaced === 'function'
        ? (droid as any).onSessionIdReplaced(
            ({
              oldSessionId,
              newSessionId,
              reason,
            }: {
              oldSessionId: string
              newSessionId: string
              reason?: string
            }) => {
              const oldId = String(oldSessionId || '').trim()
              const newId = String(newSessionId || '').trim()
              if (!oldId || !newId || oldId === newId) return

              const state = useAppStore.getState()
              const meta = state.projects.flatMap((p) => p.sessions).find((x) => x.id === oldId)
              if (!meta) return

              const now = Date.now()
              const nextMeta: SessionMeta = {
                ...meta,
                id: newId,
                savedAt: now,
                lastMessageAt: undefined,
                messageCount: 0,
              }

              state._setProjects((prev) => replaceSessionIdInProjects(prev, oldId, nextMeta))
              state._setSessionBuffers((prev) => {
                const buf = prev.get(oldId)
                if (!buf) return prev
                let next = new Map(prev)
                next.delete(oldId)
                next.set(newId, {
                  ...buf,
                  isRunning: false,
                  isCancelling: false,
                  pendingSendMessageIds: {},
                  pendingPermissionRequests: [],
                  pendingAskUserRequests: [],
                  messages: [],
                  debugTrace: [
                    `session-id-replaced: ${oldId} -> ${newId} reason=${String(reason || '')}`,
                  ],
                })
                return next
              })

              useAppStore.setState((prev) => ({
                activeSessionId: prev.activeSessionId === oldId ? newId : prev.activeSessionId,
              }))
              useAppStore.setState((prev) => {
                const gens = prev._sessionGenerations
                if (!gens.has(oldId)) return prev
                const next = new Map(gens)
                next.set(newId, next.get(oldId) || 0)
                next.delete(oldId)
                return { _sessionGenerations: next }
              })
            },
          )
        : () => {}

    const unsubDebug =
      typeof onDebug === 'function'
        ? droid.onDebug(({ message, sessionId: sid }) => {
            if (!sid) return
            useAppStore
              .getState()
              ._setSessionBuffers((prev) =>
                appendDebugTrace(prev, sid, `debug: ${String(message || '')}`),
              )
          })
        : () => {}

    return () => {
      unsubNotif()
      unsubReq()
      unsubStdout()
      unsubStderr()
      unsubTurnEnd()
      unsubError()
      unsubSetup()
      unsubSessionReplace()
      unsubDebug()
    }
  }, [])

  return <>{children}</>
}
