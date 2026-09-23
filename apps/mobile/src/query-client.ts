// App-wide queries (the signature, Pairing Tokens). Each connection to a
// Paired Computer gets its own QueryClient, so no computer sees another's.
import { QueryClient } from '@tanstack/react-query'

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
  })
}

export const appQueryClient = createQueryClient()
