// The Session's current task list: the SDK derives it from the latest
// successful TodoWrite tool call and re-derives it as tool calls land.
import type { MultiSessionStateManager } from '@factory/droid-sdk'
import { useCallback, useRef, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'

export type TodoItem = NonNullable<
  ReturnType<
    NonNullable<ReturnType<MultiSessionStateManager['getSessionManager']>>['getCurrentTodos']
  >
>['todos'][number]

const NONE: TodoItem[] = []

export function useTodos(sessionId: string): TodoItem[] {
  const { sessionState } = useDaemonConnection()
  const version = useRef(0)
  const snapshot = useRef<{ version: number; value: TodoItem[] }>({ version: -1, value: NONE })

  const subscribe = useCallback(
    (listener: () => void) =>
      sessionState.subscribeToSessionEvents(
        [SESSION_EVENT.todoListUpdated, SESSION_EVENT.loadStateChanged],
        (_event, payload) => {
          if (payload.sessionId !== sessionId) return
          version.current += 1
          listener()
        },
      ),
    [sessionState, sessionId],
  )

  const getSnapshot = useCallback((): TodoItem[] => {
    if (snapshot.current.version === version.current) return snapshot.current.value
    const todos = sessionState.getSessionManager(sessionId)?.getCurrentTodos()?.todos ?? NONE
    snapshot.current = { version: version.current, value: todos.length === 0 ? NONE : todos }
    return snapshot.current.value
  }, [sessionState, sessionId])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}
