// The open Session as the Kernel shows it: its transcript, how far it is loaded, and what
// it is doing. Mounting it opens the conversation on the Kernel (kernel-conversations.ts),
// and the Kernel's pushes keep it current; nothing here copies entries into a store of its own.
import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { useKernelConnection } from './connection-context'
import { conversationStore, type ConversationSnapshot } from './kernel-conversations'
import { kernelStore, type KernelSnapshot } from './kernel-state'
import { conversationIdOf, type ConversationId, type LiveDoc } from './kernel-wire'
import { LOAD_STATE, type LoadState } from './sdk-enums'
import { buildTranscript, reuseUnchanged, type TranscriptEntry } from './transcript'

export interface SessionView {
  /** What the transcript shows; an unchanged entry keeps its object from one snapshot to the next. */
  transcript: readonly TranscriptEntry[]
  /** How many entries are held, every kind. */
  entryCount: number
  loadState: LoadState
  /** The Session was loaded once, so it can take a message while it opens again. */
  hasLoaded: boolean
  /** idle, thinking, streaming_assistant_message, executing_tool, waiting_for_tool_confirmation or compacting_conversation. */
  workingState: string
  hasOlderMessages: boolean
  loadError: string | null
  /** The folder the Session works in, as its agent has it. */
  cwd: string | null
}

const EMPTY: SessionView = {
  transcript: [],
  entryCount: 0,
  loadState: LOAD_STATE.notLoaded,
  hasLoaded: false,
  workingState: 'idle',
  hasOlderMessages: false,
  loadError: null,
  cwd: null,
}

const NO_IDS: readonly string[] = []

/** What a conversation is doing, in the words the activity row and the alerts read. */
export function workingStateOf(
  id: ConversationId,
  docs: Readonly<Record<string, unknown>>,
  kernel: KernelSnapshot,
): string {
  for (const request of kernel.approvals.values())
    if (request.id === id) return 'waiting_for_tool_confirmation'
  for (const request of kernel.asks.values()) {
    if (request.id === id && request.blocking !== false) return 'waiting_for_tool_confirmation'
  }
  const live = docs['pi.live'] as LiveDoc | undefined
  if (live?.compactions?.some((c) => c.blocking)) return 'compacting_conversation'
  if (!live?.run && !kernel.busy.has(id)) return 'idle'
  if (live?.tools?.some((t) => t.status !== 'done')) return 'executing_tool'
  if ((live?.generation as { message?: unknown } | undefined)?.message)
    return 'streaming_assistant_message'
  return 'thinking'
}

const LOAD_STATES: Record<ConversationSnapshot['loadState'], LoadState> = {
  'not-loaded': LOAD_STATE.notLoaded,
  loading: LOAD_STATE.loading,
  loaded: LOAD_STATE.loaded,
}

export function useSession(sessionId: string | null): SessionView {
  return useSessions(sessionId ? [sessionId] : NO_IDS)[0] ?? EMPTY
}

/** useSession for several Sessions at once, in the given order. */
export function useSessions(sessionIds: readonly string[]): readonly SessionView[] {
  const connection = useKernelConnection()
  const store = conversationStore(connection)
  const kernel = kernelStore(connection)
  // Callers pass a fresh array each render; the ids themselves are the identity.
  const key = sessionIds.join('\n')
  const ids = useMemo(() => (key ? key.split('\n') : NO_IDS), [key])

  useEffect(() => {
    const releases = ids.flatMap((id) => {
      const conversation = conversationIdOf(id)
      return conversation === null ? [] : [store.watch(conversation)]
    })
    return () => {
      for (const release of releases) release()
    }
  }, [store, ids])

  const subscribe = useCallback(
    (listener: () => void) => {
      const offs = ids.flatMap((id) => {
        const conversation = conversationIdOf(id)
        return conversation === null ? [] : [store.subscribe(conversation, listener)]
      })
      offs.push(kernel.subscribe(listener))
      return () => {
        for (const off of offs) off()
      }
    },
    [store, kernel, ids],
  )

  const cache = useRef<{ inputs: unknown[]; views: readonly SessionView[] } | null>(null)
  const getSnapshot = useCallback((): readonly SessionView[] => {
    const k = kernel.snapshot()
    const snaps = ids.map((id) => {
      const conversation = conversationIdOf(id)
      return conversation === null ? null : store.snapshot(conversation)
    })
    const inputs = [k, ...snaps]
    const previous = cache.current
    if (
      previous &&
      previous.inputs.length === inputs.length &&
      previous.inputs.every((v, i) => v === inputs[i])
    ) {
      return previous.views
    }
    const views = ids.map((id, index): SessionView => {
      const snap = snaps[index]
      const conversation = conversationIdOf(id)
      if (!snap || conversation === null) return { ...EMPTY, loadError: 'Session not found' }
      const live = snap.docs['pi.live'] as LiveDoc | undefined
      const built = buildTranscript(snap.entries, live)
      for (const send of snap.sending) {
        built.push({
          id: `sending:${send.requestId}`,
          role: 'user',
          blocks: [
            ...(send.text
              ? [{ kind: 'text' as const, id: `sending:${send.requestId}:t`, text: send.text }]
              : []),
            ...send.images.map((image, i) => ({
              kind: 'image' as const,
              id: `sending:${send.requestId}:${i}`,
              src: `data:${image.mediaType};base64,${image.data}`,
            })),
          ],
          createdAt: Date.now(),
          isError: false,
        })
      }
      const agent = snap.docs['pi.agent'] as { cwd?: unknown } | undefined
      return {
        transcript: reuseUnchanged(previous?.views[index]?.transcript ?? [], built),
        entryCount: snap.entries.length,
        loadState: LOAD_STATES[snap.loadState],
        hasLoaded: snap.hasLoaded,
        workingState:
          snap.sending.length > 0 && !live?.run
            ? 'thinking'
            : workingStateOf(conversation, snap.docs, k),
        hasOlderMessages: snap.hasOlder,
        loadError: snap.error,
        cwd: typeof agent?.cwd === 'string' ? agent.cwd : null,
      }
    })
    cache.current = { inputs, views }
    return views
  }, [store, kernel, ids])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}
