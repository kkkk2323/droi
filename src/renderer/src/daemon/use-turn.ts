// Send a prompt into the open Session and cancel a running turn. The SDK owns
// the in-flight state: we register an optimistic user message under the
// request id, and the Daemon's `create_message` carrying that id confirms it.
//
// While a turn runs the message still goes to the Daemon, which holds it:
// `end_of_loop` waits until the agent is done (a queue), `end_of_turn` lands
// after the current model call, steering the work in progress. The SDK lists
// both under the Session's queued messages until the Daemon takes them.
import { LOCAL_MACHINE_ID, type MultiSessionStateManager } from '@factory/droid-sdk'
import { useCallback, useState } from 'react'
import type { ImageAttachment } from '@/lib/attachments'
import { uuid } from '@/lib/uuid'
import { useDaemonConnection } from './connection-context'

type OptimisticUserMessage = Parameters<
  MultiSessionStateManager['registerOptimisticSubmit']
>[0]['userMessage']

export type QueuePlacement = 'end_of_turn' | 'end_of_loop'

export interface SendOptions {
  images?: ImageAttachment[]
  /** Set while a turn is running; omitted for a plain send into an idle Session. */
  placement?: QueuePlacement
}

export interface TurnActions {
  send(text: string, options?: SendOptions): Promise<void>
  cancel(): Promise<void>
  sendError: string | null
}

export function useTurn(sessionId: string): TurnActions {
  const { controller, sessionState } = useDaemonConnection()
  const [sendError, setSendError] = useState<string | null>(null)

  const send = useCallback(
    async (text: string, options: SendOptions = {}) => {
      const trimmed = text.trim()
      const images = options.images ?? []
      if (!trimmed && images.length === 0) return
      setSendError(null)
      const requestId = uuid()
      const messageId = uuid()
      const content = [
        ...(trimmed ? [{ type: 'text' as const, text: trimmed }] : []),
        ...images.map((image) => ({
          type: 'image' as const,
          source: { type: 'base64' as const, data: image.data, mediaType: image.mediaType },
        })),
      ]
      // A queued message is shown from the SDK's queue, not as a sent message.
      if (!options.placement) {
        const now = Date.now()
        const userMessage = {
          id: messageId,
          role: 'user',
          content,
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
      }
      try {
        await controller.addUserMessage(
          sessionId,
          {
            text: trimmed,
            messageId,
            // The SDK types block kinds as enums it does not export at runtime.
            ...(images.length > 0 ? { content: content as never } : {}),
            ...(options.placement ? { queuePlacement: options.placement as never } : {}),
          },
          requestId,
        )
      } catch (error) {
        if (!options.placement) sessionState.cancelOptimisticSubmit(requestId)
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
