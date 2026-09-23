// Create a Session in a Workspace. Recent Workspaces come from the Sessions the
// Daemon already knows; a typed path is validated by the Daemon before use.
import { LOCAL_MACHINE_ID } from '@factory/droid-sdk'
import { GATEWAY_API_KEY_PLACEHOLDER } from './gateway'
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { uuid } from './uuid'
import type { DaemonConnection } from './connection'
import { useDaemonConnection } from './connection-context'
import {
  SESSIONS_QUERY_KEY,
  workspaceLabel,
  type SessionSummary,
  type SessionTag,
} from './sessions'

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

type Connection = Pick<DaemonConnection, 'controller' | 'sessionState'>

/**
 * Validates the directory and asks the Daemon for a Session there. Resolves
 * to the Session id, or to the message to show when the directory is unusable.
 */
export async function openSession(
  { controller, sessionState }: Connection,
  path: string,
  options: { settings?: NewSessionSettings; tags?: SessionTag[] } = {},
): Promise<{ sessionId: string } | { error: string }> {
  const check = await controller.validateWorkingDirectory(path)
  if (!check.isValid) return { error: check.error ?? `${path} is not a usable directory.` }
  const { settings, tags } = options
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
      cwd: check.resolvedPath ?? path,
      sessionOriginHint: undefined,
      sessionSource: undefined,
      ...(settings?.modelId ? { modelId: settings.modelId } : {}),
      ...(settings?.reasoningEffort ? { reasoningEffort: settings.reasoningEffort as never } : {}),
      ...(settings?.autonomyLevel ? { autonomyLevel: settings.autonomyLevel as never } : {}),
      ...(tags ? { tags } : {}),
    })
    return { sessionId: result.sessionId }
  } catch (cause) {
    sessionState.removeSession(sessionId)
    throw cause
  }
}

export function useNewSession(): NewSessionActions {
  const connection = useDaemonConnection()
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
        const opened = await openSession(connection, trimmed, settings ? { settings } : {})
        if ('error' in opened) {
          setError(opened.error)
          return null
        }
        await queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
        return opened.sessionId
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
        return null
      } finally {
        setCreating(false)
      }
    },
    [connection, queryClient],
  )

  return { create, isCreating, error }
}
