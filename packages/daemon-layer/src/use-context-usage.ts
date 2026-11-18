// How full the Session's context window is. "Used" is what the last model
// call actually sent (input + cache-read tokens), which the Daemon reports after
// every call; the budget comes from its context breakdown once per model. The
// breakdown's own `usedTokens` is a chars/4 guess over the stored history and
// counts base64 images at full length, so it only stands in before the first
// call.
import { useQuery } from '@tanstack/react-query'
import { useCallback, useRef, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'

export interface ContextUsage {
  usedTokens: number
  budgetTokens: number
  /** 0..1 */
  ratio: number
}

export function useContextUsage(
  sessionId: string,
  deps: { loaded: boolean; modelId: string | null },
): ContextUsage | null {
  const { controller, sessionState } = useDaemonConnection()
  const budget = useQuery({
    queryKey: ['context-budget', sessionId, deps.modelId],
    enabled: deps.loaded,
    staleTime: Infinity,
    queryFn: async () => {
      const breakdown = await controller.getContextBreakdown(sessionId)
      return { budgetTokens: breakdown.contextBudget, estimatedTokens: breakdown.usedTokens }
    },
  })

  const version = useRef(0)
  const snapshot = useRef<{ version: number; value: number | null }>({ version: -1, value: null })
  const subscribe = useCallback(
    (listener: () => void) => {
      const bump = (payload: { sessionId: string }) => {
        if (payload.sessionId !== sessionId) return
        version.current += 1
        listener()
      }
      controller.on('sessionTokenUsageChanged', bump)
      // load_session carries the last call's usage too; the store announces it
      // as metadata, after the load state flips.
      const unsubscribe = sessionState.subscribeToSessionEvents(
        [SESSION_EVENT.loadStateChanged, SESSION_EVENT.metadataUpdated],
        (_event, payload) => bump(payload),
      )
      return () => {
        controller.off('sessionTokenUsageChanged', bump)
        unsubscribe()
      }
    },
    [controller, sessionState, sessionId],
  )
  const getLastCall = useCallback((): number | null => {
    if (snapshot.current.version === version.current) return snapshot.current.value
    // Only the store keeps the last-call figure; the manager does not proxy it.
    const last =
      sessionState.getSessionManager(sessionId)?.getStore().getLastCallTokenUsage() ?? null
    // The Daemon reports exactly what its own compaction meter uses.
    const value = last ? last.inputTokens + last.cacheReadTokens : null
    snapshot.current = { version: version.current, value }
    return value
  }, [sessionState, sessionId])
  const lastCallTokens = useSyncExternalStore(subscribe, getLastCall, getLastCall)

  if (!budget.data) return null
  const usedTokens = lastCallTokens ?? budget.data.estimatedTokens
  const budgetTokens = budget.data.budgetTokens
  return {
    usedTokens,
    budgetTokens,
    ratio: budgetTokens > 0 ? Math.min(1, usedTokens / budgetTokens) : 0,
  }
}

export function formatTokens(count: number): string {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 10_000) return `${Math.round(count / 1_000)}k`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}k`
  return String(count)
}
