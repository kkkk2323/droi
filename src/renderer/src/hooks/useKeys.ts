import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getDroidClient } from '@/droidClient'

const droid = getDroidClient()

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
