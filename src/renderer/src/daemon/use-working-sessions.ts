// Which loaded Sessions are busy right now, for the sidebar's activity marks.
// Only Sessions the SDK has loaded report a working state; the rest are idle
// as far as this Client can tell.
import { useCallback, useRef, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'

const NONE: ReadonlySet<string> = new Set()

export function useWorkingSessionIds(): ReadonlySet<string> {
  const { sessionState } = useDaemonConnection()
  const version = useRef(0)
  const snapshot = useRef<{ version: number; value: ReadonlySet<string> }>({
    version: -1,
    value: NONE,
  })

  const subscribe = useCallback(
    (listener: () => void) =>
      sessionState.subscribeToSessionEvents(
        [SESSION_EVENT.workingStateChanged, SESSION_EVENT.loadStateChanged],
        () => {
          version.current += 1
          listener()
        },
      ),
    [sessionState],
  )

  const getSnapshot = useCallback((): ReadonlySet<string> => {
    if (snapshot.current.version === version.current) return snapshot.current.value
    const busy = new Set<string>()
    for (const id of sessionState.getAllSessionIds()) {
      const state = sessionState.getSessionManager(id)?.getDroidWorkingState()
      if (state && state !== 'idle') busy.add(id)
    }
    snapshot.current = { version: version.current, value: busy.size === 0 ? NONE : busy }
    return snapshot.current.value
  }, [sessionState])

  return useSyncExternalStore(subscribe, getSnapshot, () => NONE)
}
