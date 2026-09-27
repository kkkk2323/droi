// Full-text search over every Session on the computer, done by the Daemon
// (`daemon.search_sessions`), which indexes the session files; no Client keeps
// an index of its own. A hit comes back with snippets in which the Daemon
// wraps the matched words in <mark> tags.
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useConnectionState, useDaemonConnection } from './connection-context'

export interface SessionSearchHit {
  sessionId: string
  title: string
  /** Unix epoch seconds, like SessionSummary. */
  updatedAt: number | null
  /** The best snippet, cut into plain runs and matched runs. */
  snippet: SnippetRun[]
}

export interface SnippetRun {
  text: string
  match: boolean
  /** Where the run starts in the snippet; a stable key for rendering. */
  offset: number
}

/** Fewer characters and the Daemon scans thousands of files for nothing much. */
export const SEARCH_MIN_LENGTH = 2
export const SEARCH_LIMIT = 20
const DEBOUNCE_MS = 250

export function useSessionSearch(query: string): {
  hits: SessionSearchHit[] | undefined
  isSearching: boolean
  error: Error | null
  /** True when the query is long enough to be searched. */
  active: boolean
} {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const trimmed = query.trim()
  const active = trimmed.length >= SEARCH_MIN_LENGTH
  const settled = useDebounced(trimmed, DEBOUNCE_MS)
  const result = useQuery({
    queryKey: ['session-search', settled],
    enabled: connected && settled.length >= SEARCH_MIN_LENGTH,
    staleTime: 30_000,
    queryFn: async (): Promise<SessionSearchHit[]> => {
      const found = await controller.searchSessions({
        query: settled,
        kind: 'message_text' as never,
        limitSessions: SEARCH_LIMIT,
        limitHitsPerSession: 1,
        contextChars: 80,
      })
      return found.sessions.map((s) => ({
        sessionId: s.sessionId,
        title: s.title?.trim() || 'Untitled session',
        updatedAt: s.updatedAt ? Math.floor(s.updatedAt / 1000) : null,
        snippet: snippetRuns(s.hits[0]?.snippets[0] ?? ''),
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

/** Splits a Daemon snippet on its <mark> tags; any other tag is dropped. */
export function snippetRuns(snippet: string): SnippetRun[] {
  const runs: SnippetRun[] = []
  const parts = snippet.split(/<mark>|<\/mark>/)
  let offset = 0
  parts.forEach((part, index) => {
    const text = part.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ')
    if (text) runs.push({ text, match: index % 2 === 1, offset })
    offset += text.length
  })
  return runs
}
