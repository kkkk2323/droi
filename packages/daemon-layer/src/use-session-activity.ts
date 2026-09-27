// What the Sessions are doing right now, for the sidebar's activity marks:
// working, or stopped until the human answers (a permission request or a
// question). A Session this Client has loaded reports through the SDK's state
// manager. For the rest, the Daemon's list of open Sessions gives the state at
// connect and its working-state notifications keep it current, so a Session
// driven from the phone shows as busy on the computer too, the way the
// Factory App does it.
import { useCallback, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import type { DaemonConnection } from './connection'
import { LOAD_STATE, SESSION_EVENT } from './sdk-enums'

export type SessionActivity = 'working' | 'needs-input'

const NONE: ReadonlyMap<string, SessionActivity> = new Map()

export function activityOf(workingState: string | null | undefined): SessionActivity | null {
  if (!workingState || workingState === 'idle') return null
  return workingState === 'waiting_for_tool_confirmation' ? 'needs-input' : 'working'
}

interface ActivityStore {
  subscribe(listener: () => void): () => void
  getSnapshot(): ReadonlyMap<string, SessionActivity>
}

const stores = new WeakMap<DaemonConnection, ActivityStore>()

function activityStore(connection: DaemonConnection): ActivityStore {
  let store = stores.get(connection)
  if (!store) {
    store = createActivityStore(connection)
    stores.set(connection, store)
  }
  return store
}

function createActivityStore(connection: DaemonConnection): ActivityStore {
  const { controller, sessionState } = connection
  /** Working states the Daemon reported for Sessions, loaded here or not. */
  const reported = new Map<string, string>()
  const listeners = new Set<() => void>()
  let snapshot: ReadonlyMap<string, SessionActivity> | null = null

  const changed = () => {
    snapshot = null
    for (const listener of listeners) listener()
  }
  const onWorkingState = ({ sessionId, newState }: { sessionId: string; newState: string }) => {
    reported.set(sessionId, newState)
    changed()
  }
  // Events missed while disconnected are made up for by asking what is open now.
  const onConnected = () => {
    void controller.listOpenedSessions().then((opened) => {
      reported.clear()
      for (const session of opened) reported.set(session.sessionId, session.workingState)
      changed()
    }, console.error)
  }

  let detach: (() => void) | null = null
  const attach = () => {
    controller.on('droidWorkingStateChanged', onWorkingState)
    controller.on('connected', onConnected)
    const unsubscribe = sessionState.subscribeToSessionEvents(
      [SESSION_EVENT.workingStateChanged, SESSION_EVENT.loadStateChanged],
      changed,
    )
    if (connection.getState().status === 'connected') onConnected()
    return () => {
      controller.off('droidWorkingStateChanged', onWorkingState)
      controller.off('connected', onConnected)
      unsubscribe()
    }
  }

  return {
    subscribe(listener) {
      if (listeners.size === 0) detach = attach()
      listeners.add(listener)
      return () => {
        listeners.delete(listener)
        if (listeners.size === 0) {
          detach?.()
          detach = null
        }
      }
    },
    getSnapshot() {
      if (snapshot) return snapshot
      const busy = new Map<string, SessionActivity>()
      for (const [id, state] of reported) {
        const activity = activityOf(state)
        if (activity) busy.set(id, activity)
      }
      // A loaded Session knows best; its manager has the state after any local step.
      for (const id of sessionState.getAllSessionIds()) {
        const manager = sessionState.getSessionManager(id)
        if (!manager || sessionState.getSessionLoadState(id) !== LOAD_STATE.loaded) continue
        const activity = activityOf(manager.getDroidWorkingState())
        if (activity) busy.set(id, activity)
        else busy.delete(id)
      }
      snapshot = busy.size === 0 ? NONE : busy
      return snapshot
    },
  }
}

export function useSessionActivity(): ReadonlyMap<string, SessionActivity> {
  const connection = useDaemonConnection()
  const store = activityStore(connection)
  const subscribe = useCallback((listener: () => void) => store.subscribe(listener), [store])
  const getSnapshot = useCallback(() => store.getSnapshot(), [store])
  return useSyncExternalStore(subscribe, getSnapshot, () => NONE)
}
