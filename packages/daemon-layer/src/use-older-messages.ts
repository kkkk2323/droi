// A Session opens with its most recent messages (LOADED_MESSAGE_LIMIT); the
// rest come a page at a time from `daemon.get_session_messages`, as the
// Factory App does it. The Daemon's cursor is a message id and the page holds
// the messages before it, newest first; the SDK merges them into the Session
// by id, so the transcript grows at the front.
import { LOCAL_MACHINE_ID, type DaemonSessionController } from '@factory/droid-sdk'
import { useCallback, useState } from 'react'
import { useDaemonConnection } from './connection-context'

export const OLDER_MESSAGES_PAGE = 100

type PageMessage = Awaited<
  ReturnType<DaemonSessionController['getSessionMessages']>
>['messages'][number]

export interface OlderMessages {
  isLoading: boolean
  error: string | null
  /** Brings the next page in; nothing when every message is loaded or a page is on its way. */
  load(): Promise<void>
  /**
   * Brings pages in until one holds the message, and says whether it is
   * loaded now. The pages land together, so a long way back renders once.
   */
  loadUntil(messageId: string): Promise<boolean>
}

export function useOlderMessages(sessionId: string): OlderMessages {
  const { controller, sessionState } = useDaemonConnection()
  const [isLoading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const loadPages = useCallback(
    async (enough: (page: readonly PageMessage[]) => boolean): Promise<boolean> => {
      const manager = sessionState.getSessionManager(sessionId)
      let cursor = manager?.getMessages()[0]?.id
      if (!manager?.getHasOlderMessages() || !cursor || isLoading) return false
      setLoading(true)
      setError(null)
      try {
        const pages: PageMessage[][] = []
        let hasMore = true
        let found = false
        while (cursor) {
          const page = await controller.getSessionMessages({
            sessionId,
            cursor,
            limit: OLDER_MESSAGES_PAGE,
          })
          pages.push(page.messages)
          hasMore = page.hasMore
          found = enough(page.messages)
          cursor = hasMore && !found ? (page.nextCursor ?? page.messages.at(-1)?.id) : undefined
        }
        // The SDK's merge clears the Session's state and restores its settings,
        // but not the Daemon's model list, which only load_session sends.
        const models = manager.getAvailableModels()
        sessionState.loadSession(sessionId, LOCAL_MACHINE_ID, pages.flat().reverse(), {
          hasOlderMessages: hasMore,
        })
        if (models) manager.getStore().setAvailableModels(models)
        return found
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
        return false
      } finally {
        setLoading(false)
      }
    },
    [controller, sessionState, sessionId, isLoading],
  )
  const load = useCallback(async () => {
    await loadPages(() => true)
  }, [loadPages])
  const loadUntil = useCallback(
    (messageId: string) => loadPages((page) => page.some((message) => message.id === messageId)),
    [loadPages],
  )
  return { isLoading, error, load, loadUntil }
}
