// Read the open Session's transcript and working state from the SDK state
// manager. React subscribes to the SDK's own change events; nothing here
// copies messages into a second store.
import type { DroidWorkingState, FactoryDroidMessage } from '@factory/droid-sdk'
import { useCallback, useEffect, useRef, useSyncExternalStore } from 'react'
import { useConnectionState, useDaemonConnection } from './connection-context'
import { LOAD_STATE, SESSION_EVENT, type LoadState } from './sdk-enums'

export interface SessionView {
  messages: FactoryDroidMessage[]
  loadState: LoadState
  workingState: DroidWorkingState
  streamingMessageIds: ReadonlySet<string>
  hasOlderMessages: boolean
  loadError: string | null
}

const EMPTY: SessionView = {
  messages: [],
  loadState: LOAD_STATE.notLoaded,
  workingState: 'idle' as DroidWorkingState,
  streamingMessageIds: new Set(),
  hasOlderMessages: false,
  loadError: null,
}

const WATCHED_EVENTS = [
  SESSION_EVENT.loadStateChanged,
  SESSION_EVENT.messageThreadUpdated,
  SESSION_EVENT.workingStateChanged,
  SESSION_EVENT.streamingPlaceholderUpdated,
  SESSION_EVENT.metadataUpdated,
]

/**
 * Loads the Session into the SDK (once per Session per connection) and
 * returns a stable snapshot that changes only when the SDK reports a change.
 */
export function useSession(sessionId: string | null): SessionView {
  const connection = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const { controller, sessionState } = connection
  const snapshot = useRef<{ key: string; view: SessionView } | null>(null)
  const loadError = useRef<string | null>(null)
  const version = useRef(0)

  // Runs again after every reconnect: the SDK marks Sessions NotLoaded when the
  // transport drops, and loading again re-subscribes to their notifications.
  useEffect(() => {
    if (!sessionId || !connected) return
    loadError.current = null
    const manager = sessionState.getSessionManager(sessionId)
    if ((manager?.getLoadState() as string | undefined) === LOAD_STATE.loaded) return
    let cancelled = false
    controller
      .loadSession({
        sessionId,
        sessionOriginHint: undefined,
        sessionSource: undefined,
      })
      .catch((error: unknown) => {
        if (cancelled) return
        loadError.current = error instanceof Error ? error.message : String(error)
        version.current += 1
        snapshot.current = null
      })
    return () => {
      cancelled = true
    }
  }, [controller, sessionState, sessionId, connected])

  const subscribe = useCallback(
    (listener: () => void) => {
      const bump = (_event: unknown, payload: { sessionId: string }) => {
        if (payload.sessionId !== sessionId) return
        version.current += 1
        listener()
      }
      const unsubscribe = sessionState.subscribeToSessionEvents(WATCHED_EVENTS, bump)
      const onFailed = (id: string) => {
        if (id !== sessionId) return
        loadError.current = 'Session not found'
        version.current += 1
        listener()
      }
      controller.on('sessionNotFound', onFailed)
      return () => {
        unsubscribe()
        controller.off('sessionNotFound', onFailed)
      }
    },
    [controller, sessionState, sessionId],
  )

  const getSnapshot = useCallback((): SessionView => {
    if (!sessionId) return EMPTY
    const key = `${sessionId}:${version.current}`
    if (snapshot.current?.key === key) return snapshot.current.view
    const manager = sessionState.getSessionManager(sessionId)
    const view: SessionView = manager
      ? {
          messages: manager.getDisplayMessages(),
          loadState: manager.getLoadState() as unknown as LoadState,
          workingState: manager.getDroidWorkingState(),
          streamingMessageIds: new Set(manager.getStreamingMessageIds()),
          hasOlderMessages: manager.getHasOlderMessages(),
          loadError: loadError.current,
        }
      : { ...EMPTY, loadError: loadError.current }
    snapshot.current = { key, view }
    return view
  }, [sessionState, sessionId])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}
