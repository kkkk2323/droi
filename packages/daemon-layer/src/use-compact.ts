// `/compact` in older Daemons is a handoff: it summarises the conversation into
// a new Session whose file records the old one as its parent, and answers with
// the new id. The Daemon does not list that link, so the Client tags the child
// itself; every Client then chains the two (see sessions.ts). Newer Daemons
// compact in place instead: they answer with the same id and send
// `session_compacted`, as an automatic compaction does, so there is nothing to link.
// Either way the run is logged in compaction.ts, which is where the sidebar's
// mark and the finished alert come from.
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { compactions, useCompactions } from './compaction'
import { useDaemonConnection } from './connection-context'
import { SESSIONS_QUERY_KEY, continuationTags, type SessionTag } from './sessions'

export const COMPACT_COMMAND = /^\/compact(?:\s+([\s\S]*))?$/

export interface CompactActions {
  /**
   * Resolves to the child of a handoff while this Session is still on screen,
   * the one case where the view should move; null when it compacted in place,
   * the Daemon refused, or the user has gone elsewhere since.
   */
  compact(instructions?: string): Promise<string | null>
  isCompacting: boolean
  error: string | null
}

export function useCompact(sessionId: string, tags: readonly SessionTag[]): CompactActions {
  const { controller } = useDaemonConnection()
  const queryClient = useQueryClient()
  const isCompacting = useCompactions().pending.has(sessionId)
  const [error, setError] = useState<string | null>(null)
  // A `/compact` can take minutes; by then the user may be reading another Session.
  const shown = useRef(false)
  useEffect(() => {
    shown.current = true
    return () => {
      shown.current = false
    }
  }, [])

  const compact = useCallback(
    async (instructions?: string) => {
      compactions.start(sessionId)
      setError(null)
      try {
        const result = await controller.compactSession(sessionId, instructions?.trim() || undefined)
        if (result.newSessionId !== sessionId) {
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
        }
        compactions.finish(sessionId, result.newSessionId, result.removedCount)
        return result.newSessionId !== sessionId && shown.current ? result.newSessionId : null
      } catch (cause) {
        compactions.fail(sessionId)
        setError(cause instanceof Error ? cause.message : String(cause))
        return null
      }
    },
    [controller, queryClient, sessionId, tags],
  )

  return { compact, isCompacting, error }
}
