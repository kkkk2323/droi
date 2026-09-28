// What the Sessions are doing right now, for the sidebar's activity marks:
// working, or stopped until the human answers (a permission request or a
// question). A Session this Client has loaded reports through the SDK's state
// manager. For the rest, the Daemon's list of open Sessions gives the state at
// connect and its working-state notifications keep it current, so a Session
// driven from the phone shows as busy on the computer too, the way the
// Factory App does it. A `/compact` this Client started shows as compacting
// from its own log (compaction.ts), since the Daemon reports no state for it.
import { useCallback, useMemo, useSyncExternalStore } from 'react'
import { useCompactions } from './compaction'
import { useDaemonConnection } from './connection-context'
import type { DaemonConnection } from './connection'
import { LOAD_STATE, SESSION_EVENT } from './sdk-enums'

export type SessionActivity = 'working' | 'needs-input' | 'compacting'

const NONE: ReadonlyMap<string, SessionActivity> = new Map()

export function activityOf(workingState: string | null | undefined): SessionActivity | null {
  if (!workingState || workingState === 'idle') return null
  if (workingState === 'waiting_for_tool_confirmation') return 'needs-input'
  if (workingState === 'compacting_conversation') return 'compacting'
  return 'working'
}

/** How many of some Sessions are busy, for a folded group's header. */
export interface Busy {
  working: number
  needsInput: number
}

/**
 * Counts the Sessions working (subagents running count as working) and the
 * ones waiting for an answer; null when none is doing anything.
 */
export function countBusy(
  sessions: readonly { sessionId: string }[],
  activity: ReadonlyMap<string, SessionActivity>,
  subagentsRunning: ReadonlyMap<string, number> = new Map(),
): Busy | null {
  let working = 0
  let needsInput = 0
  for (const { sessionId } of sessions) {
    const doing = activity.get(sessionId)
    if (doing === 'needs-input') needsInput += 1
    else if (doing || (subagentsRunning.get(sessionId) ?? 0) > 0) working += 1
  }
  return working + needsInput > 0 ? { working, needsInput } : null
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
  const reported = useSyncExternalStore(subscribe, getSnapshot, () => NONE)
  const { pending } = useCompactions()
  return useMemo(() => {
    if (pending.size === 0) return reported
    const merged = new Map(reported)
    for (const id of pending) merged.set(id, 'compacting')
    return merged
  }, [reported, pending])
}
