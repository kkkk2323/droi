// A Session opens with its most recent messages (LOADED_MESSAGE_LIMIT); the
// rest come a page at a time from `daemon.get_session_messages`, as the
// Factory App does it. The Daemon's cursor is a message id and the page holds
// the messages before it, newest first; the SDK merges them into the Session
// by id, so the transcript grows at the front.
import { LOCAL_MACHINE_ID } from '@factory/droid-sdk'
import { useCallback, useState } from 'react'
import { useDaemonConnection } from './connection-context'

export const OLDER_MESSAGES_PAGE = 100

export interface OlderMessages {
  isLoading: boolean
  error: string | null
  /** Brings the next page in; nothing when every message is loaded or a page is on its way. */
  load(): Promise<void>
}

export function useOlderMessages(sessionId: string): OlderMessages {
  const { controller, sessionState } = useDaemonConnection()
  const [isLoading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const load = useCallback(async () => {
    const manager = sessionState.getSessionManager(sessionId)
    const cursor = manager?.getMessages()[0]?.id
    if (!manager?.getHasOlderMessages() || !cursor || isLoading) return
    setLoading(true)
    setError(null)
    try {
      const page = await controller.getSessionMessages({
        sessionId,
        cursor,
        limit: OLDER_MESSAGES_PAGE,
      })
      sessionState.loadSession(sessionId, LOCAL_MACHINE_ID, [...page.messages].reverse(), {
        hasOlderMessages: page.hasMore,
      })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setLoading(false)
    }
  }, [controller, sessionState, sessionId, isLoading])
  return { isLoading, error, load }
}
