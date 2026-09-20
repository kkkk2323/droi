// Messages the Daemon is holding for a running Session (see use-turn.ts), read
// from the SDK store, plus removing one before it goes out.
import type { MultiSessionStateManager } from '@factory/droid-sdk'
import { useCallback, useRef, useState, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'

export type QueuedMessage = ReturnType<
  NonNullable<ReturnType<MultiSessionStateManager['getSessionManager']>>['getQueuedMessages']
>[number]

const NONE: QueuedMessage[] = []

export function useQueuedMessages(sessionId: string): QueuedMessage[] {
  const { sessionState } = useDaemonConnection()
  const version = useRef(0)
  const snapshot = useRef<{ version: number; value: QueuedMessage[] }>({
    version: -1,
    value: NONE,
  })

  const subscribe = useCallback(
    (listener: () => void) =>
      sessionState.subscribeToSessionEvents(
        [SESSION_EVENT.queuedMessagesUpdated, SESSION_EVENT.loadStateChanged],
        (_event, payload) => {
          if (payload.sessionId !== sessionId) return
          version.current += 1
          listener()
        },
      ),
    [sessionState, sessionId],
  )

  const getSnapshot = useCallback((): QueuedMessage[] => {
    if (snapshot.current.version === version.current) return snapshot.current.value
    const manager = sessionState.getSessionManager(sessionId)
    const value = manager?.getQueuedMessages() ?? NONE
    snapshot.current = { version: version.current, value: value.length === 0 ? NONE : value }
    return snapshot.current.value
  }, [sessionState, sessionId])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

export function useQueuedMessageActions(sessionId: string): {
  remove(requestId: string): Promise<void>
  error: string | null
} {
  const { controller } = useDaemonConnection()
  const [error, setError] = useState<string | null>(null)
  const remove = useCallback(
    async (requestId: string) => {
      setError(null)
      try {
        await controller.resolveQueuedUserMessage(sessionId, {
          requestId,
          action: 'delete' as never,
        })
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
      }
    },
    [controller, sessionId],
  )
  return { remove, error }
}

/** Plain text of a queued message, for the strip above the composer. */
export function queuedText(message: QueuedMessage): string {
  return message.content
    .map((block) => (block.type === 'text' ? block.text : block.type === 'image' ? '[image]' : ''))
    .filter(Boolean)
    .join(' ')
}
