// How full the Session's context window is, from the Daemon's context
// breakdown. It only moves when a turn ends or the model changes, so it is
// fetched then rather than polled.
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useDaemonConnection } from './connection-context'

export interface ContextUsage {
  usedTokens: number
  budgetTokens: number
  /** 0..1 */
  ratio: number
}

export function useContextUsage(
  sessionId: string,
  deps: { loaded: boolean; idle: boolean; modelId: string | null },
): ContextUsage | null {
  const { controller } = useDaemonConnection()
  const query = useQuery({
    queryKey: ['context-usage', sessionId, deps.modelId],
    enabled: deps.loaded,
    staleTime: Infinity,
    queryFn: async (): Promise<ContextUsage> => {
      const breakdown = await controller.getContextBreakdown(sessionId)
      const budget = breakdown.contextBudget
      return {
        usedTokens: breakdown.usedTokens,
        budgetTokens: budget,
        ratio: budget > 0 ? Math.min(1, breakdown.usedTokens / budget) : 0,
      }
    },
  })
  const refetch = query.refetch
  useEffect(() => {
    if (deps.loaded && deps.idle) void refetch()
  }, [deps.loaded, deps.idle, refetch])
  return query.data ?? null
}

export function formatTokens(count: number): string {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 10_000) return `${Math.round(count / 1_000)}k`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}k`
  return String(count)
}
