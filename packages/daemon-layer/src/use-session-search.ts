// Full-text search over every Session on the computer, done by the Kernel: `conversation.list`
// with a `query` matches titles and the text of every message. The Kernel has no excerpt of the
// match, so a hit's second line is the conversation's newest message, with the query marked
// where it shows.
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useConnectionState, useKernelConnection } from './connection-context'
import type { ListItem } from './kernel-wire'
import { sessionIdOf } from './kernel-wire'

export interface SessionSearchHit {
  sessionId: string
  title: string
  /** Unix epoch seconds, like SessionSummary. */
  updatedAt: number | null
  /** The newest message, cut into plain runs and matched runs. */
  snippet: SnippetRun[]
}

export interface SnippetRun {
  text: string
  match: boolean
  /** Where the run starts in the snippet; a stable key for rendering. */
  offset: number
}

export const SEARCH_MIN_LENGTH = 2
export const SEARCH_LIMIT = 20
const DEBOUNCE_MS = 250

/** Conversations the Host runs for itself, which no list shows. */
const HIDDEN_KINDS = new Set(['memory'])

export function useSessionSearch(query: string): {
  hits: SessionSearchHit[] | undefined
  isSearching: boolean
  error: Error | null
  /** True when the query is long enough to be searched. */
  active: boolean
} {
  const connection = useKernelConnection()
  const connected = useConnectionState().status === 'connected'
  const trimmed = query.trim()
  const active = trimmed.length >= SEARCH_MIN_LENGTH
  const settled = useDebounced(trimmed, DEBOUNCE_MS)
  const result = useQuery({
    queryKey: ['session-search', settled],
    enabled: connected && settled.length >= SEARCH_MIN_LENGTH,
    staleTime: 30_000,
    queryFn: async (): Promise<SessionSearchHit[]> => {
      const found = await connection.call<{ items: ListItem[] }>('conversation.list', {
        query: settled,
        limit: SEARCH_LIMIT,
      })
      return found.items
        .filter((item) => !HIDDEN_KINDS.has(item.kind ?? ''))
        .map((item) => ({
          sessionId: sessionIdOf(item.id),
          title: item.title?.trim() || 'Untitled session',
          updatedAt: Math.floor(item.lastActivityAt / 1000),
          snippet: markedRuns(item.preview ?? '', settled),
        }))
    },
  })
  return {
    hits: active ? result.data : undefined,
    isSearching: active && (result.isPending || settled !== trimmed),
    error: result.error,
    active,
  }
}

function useDebounced<T>(value: T, delayMs: number): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), delayMs)
    return () => clearTimeout(timer)
  }, [value, delayMs])
  return settled
}

/** The text cut into runs, each place it has the query, in any case, marked. */
export function markedRuns(text: string, query: string): SnippetRun[] {
  const plain = text.trim().replace(/\s+/g, ' ')
  if (!plain) return []
  const lower = plain.toLowerCase()
  const q = query.toLowerCase()
  // Lowering a few letters changes their length, and the places found in lower are then not text's.
  if (!q || lower.length !== plain.length) return [{ text: plain, match: false, offset: 0 }]
  const runs: SnippetRun[] = []
  let from = 0
  for (let at = lower.indexOf(q); at !== -1; at = lower.indexOf(q, from)) {
    if (at > from) runs.push({ text: plain.slice(from, at), match: false, offset: from })
    runs.push({ text: plain.slice(at, at + q.length), match: true, offset: at })
    from = at + q.length
  }
  if (from < plain.length) runs.push({ text: plain.slice(from), match: false, offset: from })
  return runs
}
