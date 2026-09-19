// Per-Session settings (model, reasoning effort, autonomy) read from the SDK
// store and written through the controller. The Daemon confirms a change with
// a settings_updated notification, which the SDK applies and we re-render on.
import { useCallback, useRef, useState, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'

export interface ModelChoice {
  id: string
  label: string
  reasoningEfforts: string[]
  disabled: boolean
}

export interface SessionSettingsView {
  modelId: string | null
  reasoningEffort: string | null
  autonomyLevel: string | null
  models: ModelChoice[]
}

const EMPTY: SessionSettingsView = {
  modelId: null,
  reasoningEffort: null,
  autonomyLevel: null,
  models: [],
}

export const AUTONOMY_LEVELS = ['off', 'low', 'medium', 'high'] as const

export function useSessionSettings(sessionId: string): SessionSettingsView {
  const { sessionState } = useDaemonConnection()
  const version = useRef(0)
  const snapshot = useRef<{ version: number; value: SessionSettingsView }>({
    version: -1,
    value: EMPTY,
  })

  const subscribe = useCallback(
    (listener: () => void) =>
      sessionState.subscribeToSessionEvents(
        [
          SESSION_EVENT.settingsUpdated,
          SESSION_EVENT.loadStateChanged,
          SESSION_EVENT.metadataUpdated,
        ],
        (_event, payload) => {
          if (payload.sessionId !== sessionId) return
          version.current += 1
          listener()
        },
      ),
    [sessionState, sessionId],
  )

  const getSnapshot = useCallback((): SessionSettingsView => {
    if (snapshot.current.version === version.current) return snapshot.current.value
    const manager = sessionState.getSessionManager(sessionId)
    const value: SessionSettingsView = manager
      ? {
          modelId: manager.getModelId(),
          reasoningEffort: manager.getReasoningEffort(),
          autonomyLevel: manager.getAutonomyLevel(),
          models: (manager.getAvailableModels() ?? []).map((model) => ({
            id: model.id,
            label: model.displayName,
            reasoningEfforts: model.supportedReasoningEfforts,
            disabled: 'disabled' in model && model.disabled === true,
          })),
        }
      : EMPTY
    snapshot.current = { version: version.current, value }
    return value
  }, [sessionState, sessionId])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

export interface SessionSettingsActions {
  setModel(modelId: string): Promise<void>
  setReasoningEffort(effort: string): Promise<void>
  setAutonomyLevel(level: string): Promise<void>
  rename(title: string): Promise<void>
  archive(): Promise<void>
  unarchive(): Promise<void>
  error: string | null
}

export function useSessionSettingsActions(sessionId: string): SessionSettingsActions {
  const { controller } = useDaemonConnection()
  const [error, setError] = useState<string | null>(null)

  const run = useCallback(async (action: () => Promise<unknown>) => {
    setError(null)
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }, [])

  return {
    setModel: (modelId) => run(() => controller.updateSessionSettings(sessionId, { modelId })),
    setReasoningEffort: (effort) =>
      run(() => controller.updateSessionSettings(sessionId, { reasoningEffort: effort as never })),
    setAutonomyLevel: (level) =>
      run(() => controller.updateSessionSettings(sessionId, { autonomyLevel: level as never })),
    rename: (title) => run(() => controller.renameSession(sessionId, title)),
    archive: () => run(() => controller.archiveSession(sessionId)),
    unarchive: () => run(() => controller.unarchiveSession(sessionId)),
    error,
  }
}
