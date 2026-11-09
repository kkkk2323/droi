// Send a message into the open Session and stop a running turn. A message sent while a
// turn runs goes to the Kernel all the same: `followUp` waits until the run is done (the
// queue), `steer` reaches the run after its current step. Both show under the Session's
// queued messages (pi.inbox) until a turn takes them. Stopping takes the queued ones back:
// they stay here, paused, and go again only when sent again.
import { useCallback, useState } from 'react'
import type { ImageAttachment } from './attachments'
import { useKernelConnection } from './connection-context'
import { conversationStore, plainText, type Paused } from './kernel-conversations'
import { conversationIdOf, type Entry, type InboxItem } from './kernel-wire'
import { uuid } from './uuid'

export type QueuePlacement = 'end_of_turn' | 'end_of_loop'

export interface SendOptions {
  images?: ImageAttachment[]
  /** Set while a turn is running: `end_of_turn` steers it, `end_of_loop` queues after it. */
  placement?: QueuePlacement
}

export interface TurnActions {
  send(text: string, options?: SendOptions): Promise<void>
  cancel(): Promise<void>
  sendError: string | null
}

/** A message as the Kernel takes it: the text alone, or text and pictures as blocks. */
export function submitInput(
  text: string,
  images: readonly { data: string; mediaType: string }[],
): unknown {
  if (images.length === 0) return text
  return [
    ...(text ? [{ type: 'text', text }] : []),
    ...images.map((image) => ({ type: 'image', data: image.data, mimeType: image.mediaType })),
  ]
}

export function useTurn(sessionId: string): TurnActions {
  const connection = useKernelConnection()
  const store = conversationStore(connection)
  const [sendError, setSendError] = useState<string | null>(null)
  const id = conversationIdOf(sessionId)

  const send = useCallback(
    async (text: string, options: SendOptions = {}) => {
      const trimmed = text.trim()
      const images = options.images ?? []
      if ((!trimmed && images.length === 0) || id === null) return
      setSendError(null)
      const requestId = uuid()
      const entries = store.snapshot(id).entries
      // A queued message shows in the queue, not as one sent.
      if (!options.placement) {
        store.addSending(id, {
          requestId,
          text: trimmed,
          images: images.map((i) => ({ data: i.data, mediaType: i.mediaType })),
          since: entries[entries.length - 1]?.id ?? 0,
        })
      }
      try {
        await connection.call('conversation.submit', {
          id,
          input: submitInput(trimmed, images),
          whenBusy: options.placement === 'end_of_turn' ? 'steer' : 'followUp',
          requestId,
        })
      } catch (error) {
        store.dropSending(id, requestId)
        setSendError(error instanceof Error ? error.message : String(error))
      }
    },
    [connection, store, id],
  )

  const cancel = useCallback(async () => {
    if (id === null) return
    const inbox =
      (store.snapshot(id).docs['pi.inbox'] as { items?: InboxItem[] } | undefined)?.items ?? []
    const queued = inbox.filter((item) => item.mode !== 'write')
    try {
      await connection.call('conversation.abort', { id })
    } catch (error) {
      setSendError(error instanceof Error ? error.message : String(error))
      return
    }
    // The Kernel withdrew what it held; those that never entered the transcript are kept here.
    const paused: Paused[] = []
    for (const item of queued) {
      try {
        const record = await connection.call<{ status: string; entry?: Entry }>('submission.get', {
          id,
          submissionId: item.id,
        })
        if (record.status !== 'unanswered' || record.entry) continue
      } catch {
        continue
      }
      paused.push({ requestId: `paused:${item.id}`, ...contentOf(item.content) })
    }
    if (paused.length > 0) store.setPaused(id, paused)
  }, [connection, store, id])

  return { send, cancel, sendError }
}

/** A queued input's text and pictures. */
export function contentOf(content: unknown): {
  text: string
  images: Array<{ data: string; mediaType: string }>
} {
  const images: Array<{ data: string; mediaType: string }> = []
  if (Array.isArray(content)) {
    for (const block of content) {
      const b = block as { type?: unknown; data?: unknown; mimeType?: unknown }
      if (b.type === 'image' && typeof b.data === 'string' && typeof b.mimeType === 'string') {
        images.push({ data: b.data, mediaType: b.mimeType })
      }
    }
  }
  return { text: plainText(content), images }
}
