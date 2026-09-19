import { useQuery } from '@tanstack/react-query'
import { getDroidClient } from '@/droidClient'

export function useEditorsQuery() {
  return useQuery({
    queryKey: ['editors'],
    queryFn: () => getDroidClient().detectEditors(),
    staleTime: 1000 * 60 * 5,
  })
}
