// Messages waiting for the running turn (pi.inbox), and those a stop took back (paused,
// kept on this Client only); removing one before it goes out, or taking it back into the
// composer.
import { useCallback, useMemo, useState, useSyncExternalStore } from 'react'
import { isImageMediaType, type ImageAttachment } from './attachments'
import { useKernelConnection } from './connection-context'
import { conversationStore } from './kernel-conversations'
import { conversationIdOf, type InboxItem } from './kernel-wire'
import { contentOf } from './use-turn'
import { uuid } from './uuid'

export interface QueuedMessage {
  /** `kernel:<submissionId>` for one the Kernel holds, `paused:<…>` for one kept here. */
  requestId: string
  kind: 'steer' | 'followUp' | 'paused'
  text: string
  images: Array<{ data: string; mediaType: string }>
}

const NONE: QueuedMessage[] = []
const KERNEL_PREFIX = 'kernel:'

export function isHeldByKernel(message: QueuedMessage): boolean {
  return message.kind !== 'paused'
}

/** Kept back on this Client since a stopped turn; sending it again is up to the user. */
export function isPaused(message: QueuedMessage): boolean {
  return message.kind === 'paused'
}

/** Handed to the running turn rather than waiting for it to end. */
export function isSteering(message: QueuedMessage): boolean {
  return message.kind === 'steer'
}

export function useQueuedMessages(sessionId: string): QueuedMessage[] {
  const store = conversationStore(useKernelConnection())
  const id = conversationIdOf(sessionId)
  const subscribe = useCallback(
    (listener: () => void) => (id === null ? () => {} : store.subscribe(id, listener)),
    [store, id],
  )
  const get = useCallback(() => (id === null ? null : store.snapshot(id)), [store, id])
  const snap = useSyncExternalStore(subscribe, get, get)
  return useMemo(() => {
    if (!snap) return NONE
    const inbox = (snap.docs['pi.inbox'] as { items?: InboxItem[] } | undefined)?.items ?? []
    const queued: QueuedMessage[] = inbox
      .filter((item) => item.mode === 'steer' || item.mode === 'followUp')
      .map((item) => ({
        requestId: `${KERNEL_PREFIX}${item.id}`,
        kind: item.mode as 'steer' | 'followUp',
        ...contentOf(item.content),
      }))
    for (const p of snap.paused) queued.push({ ...p, kind: 'paused' })
    return queued.length === 0 ? NONE : queued
  }, [snap])
}

/** What a queued message holds, in the composer's terms. */
export interface QueuedContent {
  text: string
  images: ImageAttachment[]
}

export function useQueuedMessageActions(sessionId: string): {
  remove(requestId: string): Promise<void>
  /** Removes the message and hands back its content, for the composer to send again. */
  take(message: QueuedMessage): Promise<QueuedContent | null>
  error: string | null
} {
  const connection = useKernelConnection()
  const store = conversationStore(connection)
  const [error, setError] = useState<string | null>(null)
  const id = conversationIdOf(sessionId)
  const remove = useCallback(
    async (requestId: string) => {
      if (id === null) return
      setError(null)
      if (!requestId.startsWith(KERNEL_PREFIX)) {
        store.dropPaused(id, requestId)
        return
      }
      try {
        await connection.call('submission.abort', {
          id,
          submissionId: Number(requestId.slice(KERNEL_PREFIX.length)),
        })
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
        throw cause
      }
    },
    [connection, store, id],
  )
  const take = useCallback(
    async (message: QueuedMessage): Promise<QueuedContent | null> => {
      try {
        await remove(message.requestId)
      } catch {
        // A turn took it meanwhile: it is sent, and nothing comes back to the composer.
        return null
      }
      return queuedContent(message)
    },
    [remove],
  )
  return { remove: (requestId) => remove(requestId).catch(() => {}), take, error }
}

/** Plain text of a queued message, for the strip above the composer. */
export function queuedText(message: QueuedMessage): string {
  return [message.text, ...message.images.map(() => '[image]')].filter(Boolean).join(' ')
}

export function queuedContent(message: QueuedMessage): QueuedContent {
  const images: ImageAttachment[] = []
  for (const image of message.images) {
    if (!isImageMediaType(image.mediaType)) continue
    images.push({ id: uuid(), name: 'image', mediaType: image.mediaType, data: image.data })
  }
  return { text: message.text, images }
}
