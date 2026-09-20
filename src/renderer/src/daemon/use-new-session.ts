// Create a Session in a Workspace. Recent Workspaces come from the Sessions the
// Daemon already knows; a typed path is validated by the Daemon before use.
import { LOCAL_MACHINE_ID } from '@factory/droid-sdk'
import { GATEWAY_API_KEY_PLACEHOLDER } from '@shared/gateway'
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { uuid } from '@/lib/uuid'
import { useDaemonConnection } from './connection-context'
import { SESSIONS_QUERY_KEY, workspaceLabel, type SessionSummary } from './sessions'

export interface RecentWorkspace {
  path: string
  label: string
  lastUsedAt: number
}

/** Most recent first, one entry per Workspace path. */
export function recentWorkspaces(sessions: readonly SessionSummary[]): RecentWorkspace[] {
  const byPath = new Map<string, RecentWorkspace>()
  for (const session of sessions) {
    const path = session.repoRoot ?? session.cwd
    if (!path) continue
    const existing = byPath.get(path)
    if (!existing || existing.lastUsedAt < session.updatedAt) {
      byPath.set(path, { path, label: workspaceLabel(path), lastUsedAt: session.updatedAt })
    }
  }
  return [...byPath.values()].sort((a, b) => b.lastUsedAt - a.lastUsedAt)
}

export interface NewSessionSettings {
  modelId: string | null
  reasoningEffort: string | null
  autonomyLevel: string | null
}

export interface NewSessionActions {
  create(path: string, settings?: NewSessionSettings): Promise<string | null>
  isCreating: boolean
  error: string | null
}

export function useNewSession(): NewSessionActions {
  const { controller, sessionState } = useDaemonConnection()
  const queryClient = useQueryClient()
  const [isCreating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const create = useCallback(
    async (path: string, settings?: NewSessionSettings): Promise<string | null> => {
      const trimmed = path.trim()
      if (!trimmed) {
        setError('Enter a directory path.')
        return null
      }
      setCreating(true)
      setError(null)
      try {
        const check = await controller.validateWorkingDirectory(trimmed)
        if (!check.isValid) {
          setError(check.error ?? `${trimmed} is not a usable directory.`)
          return null
        }
        // The SDK wants the Session registered as loading before it asks the
        // Daemon to create it, so the id is chosen here.
        const sessionId = uuid()
        sessionState.markSessionLoading(sessionId, LOCAL_MACHINE_ID)
        try {
          const result = await controller.initializeSession({
            sessionId,
            machineId: LOCAL_MACHINE_ID,
            // Spawn credential; the Gateway swaps the placeholder for the real key.
            token: GATEWAY_API_KEY_PLACEHOLDER,
            cwd: check.resolvedPath ?? trimmed,
            sessionOriginHint: undefined,
            sessionSource: undefined,
            ...(settings?.modelId ? { modelId: settings.modelId } : {}),
            ...(settings?.reasoningEffort
              ? { reasoningEffort: settings.reasoningEffort as never }
              : {}),
            ...(settings?.autonomyLevel ? { autonomyLevel: settings.autonomyLevel as never } : {}),
          })
          await queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
          return result.sessionId
        } catch (cause) {
          sessionState.removeSession(sessionId)
          throw cause
        }
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
        return null
      } finally {
        setCreating(false)
      }
    },
    [controller, sessionState, queryClient],
  )

  return { create, isCreating, error }
}
