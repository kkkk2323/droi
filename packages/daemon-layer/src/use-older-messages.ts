// A Session opens with its newest entries (the window); the rest come a page at a time
// from `conversation.entries`, newest first, and join the transcript at the front.
import { useCallback, useState } from 'react'
import { useKernelConnection } from './connection-context'
import { conversationStore, OLDER_PAGE } from './kernel-conversations'
import { conversationIdOf } from './kernel-wire'

export const OLDER_MESSAGES_PAGE = OLDER_PAGE

export interface OlderMessages {
  isLoading: boolean
  error: string | null
  /** Brings the next page in; nothing when every entry is loaded or a page is on its way. */
  load(): Promise<void>
  /** Brings pages in until one holds the entry, and says whether it is loaded now. */
  loadUntil(entryId: string): Promise<boolean>
}

export function useOlderMessages(sessionId: string): OlderMessages {
  const store = conversationStore(useKernelConnection())
  const [isLoading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const id = conversationIdOf(sessionId)

  const loadPages = useCallback(
    async (enough: () => boolean): Promise<boolean> => {
      if (id === null) return false
      setLoading(true)
      setError(null)
      try {
        while (!enough()) {
          if (!(await store.loadOlder(id))) return enough()
        }
        return true
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
        return false
      } finally {
        setLoading(false)
      }
    },
    [store, id],
  )
  const load = useCallback(async () => {
    if (id === null) return
    let once = false
    await loadPages(() => {
      const done = once
      once = true
      return done
    })
  }, [loadPages, id])
  const loadUntil = useCallback(
    (entryId: string) =>
      loadPages(
        () =>
          id !== null &&
          store.snapshot(id).entries.some((e) => String(e.id) === entryId.split(':')[0]),
      ),
    [loadPages, store, id],
  )
  return { isLoading, error, load, loadUntil }
}
