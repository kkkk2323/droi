// `/compact` in older Daemons is a handoff: it summarises the conversation into
// a new Session whose file records the old one as its parent, and answers with
// the new id. The Daemon does not list that link, so the Client tags the child
// itself; every Client then chains the two (see sessions.ts). Newer Daemons
// compact in place instead: they answer with the same id and send
// `session_compacted`, as an automatic compaction does, so there is nothing to link.
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSIONS_QUERY_KEY, continuationTags, type SessionTag } from './sessions'

export const COMPACT_COMMAND = /^\/compact(?:\s+([\s\S]*))?$/

export interface CompactActions {
  /** Resolves to the Session to show (the child after a handoff), or null when the Daemon refused. */
  compact(instructions?: string): Promise<string | null>
  isCompacting: boolean
  error: string | null
}

export function useCompact(sessionId: string, tags: readonly SessionTag[]): CompactActions {
  const { controller } = useDaemonConnection()
  const queryClient = useQueryClient()
  const [isCompacting, setCompacting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const compact = useCallback(
    async (instructions?: string) => {
      setCompacting(true)
      setError(null)
      try {
        const result = await controller.compactSession(sessionId, instructions?.trim() || undefined)
        if (result.newSessionId === sessionId) return sessionId
        // The handoff creates the child inactive; settings only apply to a loaded Session.
        await controller.loadSession({
          sessionId: result.newSessionId,
          sessionOriginHint: undefined,
          sessionSource: undefined,
        })
        await controller.updateSessionSettings(result.newSessionId, {
          tags: continuationTags(sessionId, tags),
        })
        await queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
        return result.newSessionId
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
        return null
      } finally {
        setCompacting(false)
      }
    },
    [controller, queryClient, sessionId, tags],
  )

  return { compact, isCompacting, error }
}
