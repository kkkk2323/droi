import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getDroidClient } from '@/droidClient'

const droid = getDroidClient()

export const keysQueryKey = ['keys'] as const

export function useKeysQuery(sessionId?: string) {
  return useQuery({
    queryKey: [...keysQueryKey, sessionId || 'global'],
    queryFn: () => droid.listKeys(sessionId),
  })
}

export function useRefreshKeysMutation(sessionId?: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => droid.refreshKeys(sessionId),
    onSuccess: (data) => {
      queryClient.setQueryData([...keysQueryKey, sessionId || 'global'], data)
    },
  })
}

export function useAddKeysMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (keys: string[]) => droid.addKeys(keys),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keysQueryKey })
    },
  })
}

export function useRemoveKeyMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (index: number) => droid.removeKeyByIndex(index),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keysQueryKey })
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
