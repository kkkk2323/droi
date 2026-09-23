// What the loaded Sessions are doing right now, for the sidebar's activity
// marks: working, or stopped until the human answers (a permission request or
// a question). Only Sessions the SDK has loaded report a working state; the
// rest are idle as far as this Client can tell.
import { useCallback, useRef, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'

export type SessionActivity = 'working' | 'needs-input'

const NONE: ReadonlyMap<string, SessionActivity> = new Map()

export function activityOf(workingState: string | null | undefined): SessionActivity | null {
  if (!workingState || workingState === 'idle') return null
  return workingState === 'waiting_for_tool_confirmation' ? 'needs-input' : 'working'
}

export function useSessionActivity(): ReadonlyMap<string, SessionActivity> {
  const { sessionState } = useDaemonConnection()
  const version = useRef(0)
  const snapshot = useRef<{ version: number; value: ReadonlyMap<string, SessionActivity> }>({
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

  const getSnapshot = useCallback((): ReadonlyMap<string, SessionActivity> => {
    if (snapshot.current.version === version.current) return snapshot.current.value
    const busy = new Map<string, SessionActivity>()
    for (const id of sessionState.getAllSessionIds()) {
      const activity = activityOf(sessionState.getSessionManager(id)?.getDroidWorkingState())
      if (activity) busy.set(id, activity)
    }
    snapshot.current = { version: version.current, value: busy.size === 0 ? NONE : busy }
    return snapshot.current.value
  }, [sessionState])

  return useSyncExternalStore(subscribe, getSnapshot, () => NONE)
}
