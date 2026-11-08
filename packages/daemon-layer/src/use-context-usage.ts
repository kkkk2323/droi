// How full the Session's context window is: `conversation.usage`'s context, which the Kernel
// works out from the conversation's model and messages, compactions included. It is read
// again when either moves: after each model call (which `pi.usage` counts), on another model,
// on a new socket, and once the conversation rests on a new newest entry.
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useCallback, useSyncExternalStore } from 'react'
import { useKernelConnection } from './connection-context'
import { conversationStore } from './kernel-conversations'
import { conversationIdOf, type LiveDoc } from './kernel-wire'

export interface ContextUsage {
  usedTokens: number
  budgetTokens: number
  /** 0..1 */
  ratio: number
}

interface UsageAnswer {
  context?: { used?: number; limit?: number }
}

export function useContextUsage(
  sessionId: string,
  deps: { loaded: boolean; modelId: string | null },
): ContextUsage | null {
  const connection = useKernelConnection()
  const store = conversationStore(connection)
  const id = conversationIdOf(sessionId)
  const subscribe = useCallback(
    (listener: () => void) => (id === null ? () => {} : store.subscribe(id, listener)),
    [store, id],
  )
  const getKey = useCallback(() => {
    if (id === null) return ''
    const snap = store.snapshot(id)
    const live = snap.docs['pi.live'] as LiveDoc | undefined
    const settled = live?.run ? '' : String(snap.entries.at(-1)?.id ?? '')
    return `${JSON.stringify(snap.docs['pi.usage'] ?? null)}|${settled}`
  }, [store, id])
  const key = useSyncExternalStore(subscribe, getKey, getKey)
  const generation = connection.generation()
  const query = useQuery({
    queryKey: ['context-usage', sessionId, deps.modelId, generation, key],
    enabled: deps.loaded && id !== null,
    staleTime: Infinity,
    gcTime: 60_000,
    placeholderData: keepPreviousData,
    queryFn: () => connection.call<UsageAnswer>('conversation.usage', { id }),
  })
  const used = query.data?.context?.used ?? 0
  const limit = query.data?.context?.limit ?? 0
  if (limit <= 0) return null
  return { usedTokens: used, budgetTokens: limit, ratio: Math.min(1, used / limit) }
}

export function formatTokens(count: number): string {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 10_000) return `${Math.round(count / 1_000)}k`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}k`
  return String(Math.round(count))
}
