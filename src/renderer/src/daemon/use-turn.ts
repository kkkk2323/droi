// Send a prompt into the open Session and cancel a running turn. The SDK owns
// the in-flight state: we register an optimistic user message under the
// request id, and the Daemon's `create_message` carrying that id confirms it.
import { LOCAL_MACHINE_ID, type MultiSessionStateManager } from '@factory/droid-sdk'
import { useCallback, useState } from 'react'
import { uuid } from '@/lib/uuid'
import { useDaemonConnection } from './connection-context'

type OptimisticUserMessage = Parameters<
  MultiSessionStateManager['registerOptimisticSubmit']
>[0]['userMessage']

export interface TurnActions {
  send(text: string): Promise<void>
  cancel(): Promise<void>
  sendError: string | null
}

export function useTurn(sessionId: string): TurnActions {
  const { controller, sessionState } = useDaemonConnection()
  const [sendError, setSendError] = useState<string | null>(null)

  const send = useCallback(
    async (text: string) => {
      const trimmed = text.trim()
      if (!trimmed) return
      setSendError(null)
      const requestId = uuid()
      const messageId = uuid()
      const now = Date.now()
      const userMessage = {
        id: messageId,
        role: 'user',
        content: [{ type: 'text', text: trimmed }],
        createdAt: now,
        updatedAt: now,
      } as unknown as OptimisticUserMessage
      sessionState.registerOptimisticSubmit({
        sessionId,
        machineId: LOCAL_MACHINE_ID,
        externalKey: requestId,
        userMessage,
        assistantBubbleId: uuid(),
        onError: (error) => setSendError(error.message),
      })
      try {
        await controller.addUserMessage(sessionId, { text: trimmed, messageId }, requestId)
      } catch (error) {
        sessionState.cancelOptimisticSubmit(requestId)
        setSendError(error instanceof Error ? error.message : String(error))
      }
    },
    [controller, sessionState, sessionId],
  )

  const cancel = useCallback(async () => {
    try {
      await controller.interruptSession(sessionId)
    } catch (error) {
      setSendError(error instanceof Error ? error.message : String(error))
    }
  }, [controller, sessionId])

  return { send, cancel, sendError }
}
