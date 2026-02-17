import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getDroidClient } from '@/droidClient'
import { useAppStore } from '@/store'

const droid = getDroidClient()

async function applyActiveKeyToIdleSession(): Promise<void> {
  const state = useAppStore.getState()
  const sid = state.activeSessionId
  if (!sid) return
  const buf = state.sessionBuffers.get(sid)
  if (!buf) return

  let fp = ''
  try {
    const info = await droid.getActiveKeyInfo()
    fp = String((info as any)?.apiKeyFingerprint || '')
  } catch {
    return
  }
  if (!fp) return

  if (buf.isRunning || buf.isSetupRunning) {
    state._setSessionBuffers((prev) => {
      const cur = prev.get(sid)
      if (!cur) return prev
      const next = new Map(prev)
      next.set(sid, { ...cur, pendingApiKeyFingerprint: fp })
      return next
    })
    state.appendUiDebugTrace(`api-key-rotation-deferred: fp=${fp}`)
    return
  }

  if (buf.apiKeyFingerprint === fp) return

  try {
    await droid.restartSessionWithActiveKey({ sessionId: sid })
    state._setSessionBuffers((prev) => {
      const cur = prev.get(sid)
      if (!cur) return prev
      const next = new Map(prev)
      next.set(sid, { ...cur, apiKeyFingerprint: fp, pendingApiKeyFingerprint: undefined })
      return next
    })
    state.appendUiDebugTrace(`api-key-restarted: fp=${fp}`)
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    state.appendUiDebugTrace(`api-key-restart-failed: ${msg}`)
  }
}

export const keysQueryKey = ['keys'] as const

export function useKeysQuery() {
  return useQuery({
    queryKey: keysQueryKey,
    queryFn: () => droid.listKeys(),
  })
}

export function useRefreshKeysMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => droid.refreshKeys(),
    onSuccess: (data) => {
      queryClient.setQueryData(keysQueryKey, data)
      void applyActiveKeyToIdleSession()
    },
  })
}

export function useAddKeysMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (keys: string[]) => droid.addKeys(keys),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keysQueryKey })
      void applyActiveKeyToIdleSession()
    },
  })
}

export function useRemoveKeyMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (index: number) => droid.removeKeyByIndex(index),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keysQueryKey })
      void applyActiveKeyToIdleSession()
    },
  })
}

export function useUpdateKeyNoteMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ index, note }: { index: number; note: string }) =>
      droid.updateKeyNote(index, note),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keysQueryKey })
    },
  })
}
