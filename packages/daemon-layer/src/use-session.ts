// Read the open Session's transcript and working state from the SDK state
// manager. React subscribes to the SDK's own change events; nothing here
// copies messages into a second store.
import type { DroidWorkingState, FactoryDroidMessage } from '@factory/droid-sdk'
import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { useConnectionState, useDaemonConnection } from './connection-context'
import { LOAD_STATE, SESSION_EVENT, type LoadState } from './sdk-enums'
import { buildTranscript, reuseUnchanged, type TranscriptEntry } from './transcript'

export interface SessionView {
  messages: FactoryDroidMessage[]
  /** What the transcript shows; an unchanged entry keeps its object from one snapshot to the next. */
  transcript: readonly TranscriptEntry[]
  loadState: LoadState
  workingState: DroidWorkingState
  hasOlderMessages: boolean
  loadError: string | null
}

const EMPTY: SessionView = {
  messages: [],
  transcript: [],
  loadState: LOAD_STATE.notLoaded,
  workingState: 'idle' as DroidWorkingState,
  hasOlderMessages: false,
  loadError: null,
}

const NO_IDS: readonly string[] = []

const MIN_RENDER_INTERVAL_MS = 32

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
  return useSessions(sessionId ? [sessionId] : NO_IDS)[0] ?? EMPTY
}

/** useSession for several Sessions at once (a compaction chain), in the given order. */
export function useSessions(sessionIds: readonly string[]): readonly SessionView[] {
  const connection = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const { controller, sessionState } = connection
  // Callers pass a fresh array each render; the ids themselves are the identity.
  const key = sessionIds.join('\n')
  const ids = useMemo(() => (key ? key.split('\n') : NO_IDS), [key])
  const snapshot = useRef<{ key: string; views: readonly SessionView[] } | null>(null)
  const loadErrors = useRef(new Map<string, string>())
  const version = useRef(0)
  // Lets the load effect wake the useSyncExternalStore subscriber; the SDK
  // emits nothing when loadSession rejects.
  const notify = useRef<() => void>(() => {})

  // Runs again after every reconnect: the connection marks Sessions NotLoaded
  // when the transport drops, and loading again re-subscribes to their
  // notifications on the new socket.
  useEffect(() => {
    if (ids.length === 0 || !connected) return
    let cancelled = false
    for (const sessionId of ids) {
      loadErrors.current.delete(sessionId)
      const manager = sessionState.getSessionManager(sessionId)
      if ((manager?.getLoadState() as string | undefined) === LOAD_STATE.loaded) continue
      // StrictMode runs this effect twice on mount; one load is enough.
      if (controller.isSessionLoadInFlight(sessionId)) continue
      controller
        .loadSession({
          sessionId,
          sessionOriginHint: undefined,
          sessionSource: undefined,
        })
        .catch((error: unknown) => {
          if (cancelled) return
          loadErrors.current.set(sessionId, error instanceof Error ? error.message : String(error))
          version.current += 1
          notify.current()
        })
    }
    return () => {
      cancelled = true
    }
  }, [controller, sessionState, ids, connected])

  const subscribe = useCallback(
    (listener: () => void) => {
      // A streaming turn fires several events per delta, and each render
      // rebuilds the whole transcript; render at most once per animation frame
      // and no more than about 30 times a second (a 120 Hz display would
      // otherwise draw every token).
      let frame: number | null = null
      let timer: ReturnType<typeof setTimeout> | null = null
      let lastRender = 0
      const render = () => {
        frame = requestAnimationFrame(() => {
          frame = null
          lastRender = Date.now()
          listener()
        })
      }
      const scheduleRender = () => {
        version.current += 1
        if (frame !== null || timer !== null) return
        const wait = lastRender + MIN_RENDER_INTERVAL_MS - Date.now()
        if (wait <= 0) return render()
        timer = setTimeout(() => {
          timer = null
          render()
        }, wait)
      }
      notify.current = scheduleRender
      const bump = (_event: unknown, payload: { sessionId: string }) => {
        if (ids.includes(payload.sessionId)) scheduleRender()
      }
      const unsubscribe = sessionState.subscribeToSessionEvents(WATCHED_EVENTS, bump)
      const onFailed = (id: string) => {
        if (!ids.includes(id)) return
        loadErrors.current.set(id, 'Session not found')
        scheduleRender()
      }
      controller.on('sessionNotFound', onFailed)
      return () => {
        notify.current = () => {}
        unsubscribe()
        controller.off('sessionNotFound', onFailed)
        if (frame !== null) cancelAnimationFrame(frame)
        if (timer !== null) clearTimeout(timer)
      }
    },
    [controller, sessionState, ids],
  )

  const getSnapshot = useCallback((): readonly SessionView[] => {
    const snapshotKey = `${key}:${version.current}`
    if (snapshot.current?.key === snapshotKey) return snapshot.current.views
    const previous = snapshot.current?.views ?? []
    const views = ids.map((sessionId, index): SessionView => {
      const loadError = loadErrors.current.get(sessionId) ?? null
      const manager = sessionState.getSessionManager(sessionId)
      if (!manager) return { ...EMPTY, loadError }
      const messages = manager.getDisplayMessages()
      return {
        messages,
        transcript: reuseUnchanged(previous[index]?.transcript ?? [], buildTranscript(messages)),
        loadState: manager.getLoadState() as unknown as LoadState,
        workingState: manager.getDroidWorkingState(),
        hasOlderMessages: manager.getHasOlderMessages(),
        loadError,
      }
    })
    snapshot.current = { key: snapshotKey, views }
    return views
  }, [sessionState, ids, key])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}
