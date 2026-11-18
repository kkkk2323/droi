// Messages the Daemon is holding for a running Session (see use-turn.ts), read
// from the SDK store, plus removing one before it goes out or taking it back
// into the composer.
//
// Cancelling a turn makes the Daemon drop what it held; the SDK keeps those
// messages on this Client as `local_paused_after_esc`, so nothing typed is
// lost. A paused message exists only here: it is removed locally and goes out
// again only when sent from the composer.
import type { MultiSessionStateManager } from '@factory/droid-sdk'
import { useCallback, useRef, useState, useSyncExternalStore } from 'react'
import { isImageMediaType, type ImageAttachment } from './attachments'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'
import { uuid } from './uuid'

export type QueuedMessage = ReturnType<
  NonNullable<ReturnType<MultiSessionStateManager['getSessionManager']>>['getQueuedMessages']
>[number]

const NONE: QueuedMessage[] = []

/** Kinds the Daemon holds; the rest live on this Client only. */
const HELD_BY_DAEMON = new Set<string>(['daemon_queued_discardable', 'daemon_queued_end_of_loop'])
/** Kinds handed to the running turn (⌘↩) rather than waiting for it to end. */
const STEERING = new Set<string>(['daemon_queued_discardable', 'local_deferred_after_esc'])

export function isHeldByDaemon(message: QueuedMessage): boolean {
  return HELD_BY_DAEMON.has(message.kind)
}

/** Kept back on this Client since a cancelled turn; sending it again is up to the user. */
export function isPaused(message: QueuedMessage): boolean {
  return message.kind === 'local_paused_after_esc'
}

export function isSteering(message: QueuedMessage): boolean {
  return STEERING.has(message.kind)
}

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

/** What a queued message holds, in the composer's terms. */
export interface QueuedContent {
  text: string
  images: ImageAttachment[]
}

export function useQueuedMessageActions(sessionId: string): {
  remove(requestId: string): Promise<void>
  /** Removes the message and hands back its content, for the composer to send again. */
  take(requestId: string): Promise<QueuedContent | null>
  error: string | null
} {
  const { controller, sessionState } = useDaemonConnection()
  const [error, setError] = useState<string | null>(null)
  const remove = useCallback(
    async (requestId: string) => {
      setError(null)
      const manager = sessionState.getSessionManager(sessionId)
      const message = manager?.getQueuedMessages().find((m) => m.requestId === requestId)
      try {
        if (message && !isHeldByDaemon(message)) {
          manager!.clearQueuedMessage(requestId)
          return
        }
        await controller.resolveQueuedUserMessage(sessionId, {
          requestId,
          action: 'delete' as never,
        })
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
      }
    },
    [controller, sessionState, sessionId],
  )
  const take = useCallback(
    async (requestId: string): Promise<QueuedContent | null> => {
      const manager = sessionState.getSessionManager(sessionId)
      const message = manager?.getQueuedMessages().find((m) => m.requestId === requestId)
      if (!message) return null
      await remove(requestId)
      return queuedContent(message)
    },
    [sessionState, sessionId, remove],
  )
  return { remove, take, error }
}

/** Plain text of a queued message, for the strip above the composer. */
export function queuedText(message: QueuedMessage): string {
  return message.content
    .map((block) => (block.type === 'text' ? block.text : block.type === 'image' ? '[image]' : ''))
    .filter(Boolean)
    .join(' ')
}

export function queuedContent(message: QueuedMessage): QueuedContent {
  const text = message.content
    .map((block) => (block.type === 'text' ? block.text : ''))
    .filter(Boolean)
    .join('\n')
  const images: ImageAttachment[] = []
  for (const block of message.content) {
    if (block.type !== 'image' || block.source.type !== 'base64') continue
    if (!isImageMediaType(block.source.mediaType)) continue
    images.push({
      id: uuid(),
      name: 'image',
      mediaType: block.source.mediaType,
      data: block.source.data,
    })
  }
  return { text, images }
}
