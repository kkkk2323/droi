// Search across every Session on the computer, from the sidebar. The Daemon
// does the searching (use-session-search.ts); while a query is typed the
// results take the place of the Workspace groups.
import type { KeyboardEvent } from 'react'
import { Search, X } from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import { useSessionSearch, type SessionSearchHit } from '@droi/daemon-layer/use-session-search'
import { cn } from '@/lib/utils'
import { relativeTime } from './relative-time'

export function SessionSearchBox({
  query,
  onChange,
}: {
  query: string
  onChange: (query: string) => void
}) {
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape' && query) {
      event.preventDefault()
      onChange('')
    }
  }
  return (
    <div className="relative flex h-8 items-center">
      <Search
        aria-hidden
        className="pointer-events-none absolute left-2 size-4 text-muted-foreground"
      />
      <input
        type="search"
        role="searchbox"
        aria-label="Search sessions"
        placeholder="Search sessions"
        value={query}
        spellCheck={false}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKey}
        className="h-8 w-full rounded-lg border border-transparent bg-sidebar-accent/50 pl-8 pr-7 text-[13px] text-foreground outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring/60 focus-visible:bg-background [&::-webkit-search-cancel-button]:hidden"
      />
      {query ? (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => onChange('')}
          className="absolute right-1 grid size-6 place-items-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring"
        >
          <X aria-hidden className="size-3.5" />
        </button>
      ) : null}
    </div>
  )
}

export function SessionSearchResults({
  query,
  selectedSessionId,
  onSelect,
}: {
  query: string
  selectedSessionId: string | null
  onSelect: (sessionId: string) => void
}) {
  const search = useSessionSearch(query)
  if (!search.active) {
    return <p className="px-2 py-1 text-xs text-muted-foreground">Type a little more to search.</p>
  }
  if (search.error) {
    return (
      <p role="alert" className="px-2 py-1 text-xs text-destructive-foreground">
        {search.error.message}
      </p>
    )
  }
  if (!search.hits) {
    return (
      <p className="flex items-center gap-2 px-2 py-1 text-xs text-muted-foreground">
        <Spinner aria-hidden className="size-3" />
        Searching…
      </p>
    )
  }
  if (search.hits.length === 0) {
    return <p className="px-2 py-1 text-xs text-muted-foreground">No sessions match.</p>
  }
  return (
    <section aria-label="Search results" aria-busy={search.isSearching}>
      <ul className="flex flex-col gap-px">
        {search.hits.map((hit) => (
          <li key={hit.sessionId}>
            <SearchHitRow
              hit={hit}
              selected={hit.sessionId === selectedSessionId}
              onSelect={() => onSelect(hit.sessionId)}
            />
          </li>
        ))}
      </ul>
    </section>
  )
}

function SearchHitRow({
  hit,
  selected,
  onSelect,
}: {
  hit: SessionSearchHit
  selected: boolean
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      aria-current={selected ? 'page' : undefined}
      onClick={onSelect}
      title={hit.title}
      className={cn(
        'flex w-full flex-col gap-0.5 rounded-lg px-2 py-1.5 text-left outline-none transition-colors duration-150',
        'hover:bg-sidebar-accent/60 focus-visible:ring-2 focus-visible:ring-sidebar-ring',
        selected && 'bg-sidebar-accent',
      )}
    >
      <span className="flex items-center gap-1.5 text-[13px] text-foreground">
        <span className="flex-1 truncate">{hit.title}</span>
        {hit.updatedAt !== null ? (
          <time
            dateTime={new Date(hit.updatedAt * 1000).toISOString()}
            className="shrink-0 text-[11px] tabular-nums text-muted-foreground"
          >
            {relativeTime(hit.updatedAt * 1000)}
          </time>
        ) : null}
      </span>
      {hit.snippet.length > 0 ? (
        <span className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">
          {hit.snippet.map((run) =>
            run.match ? (
              <mark key={run.offset} className="rounded-sm bg-highlight text-foreground">
                {run.text}
              </mark>
            ) : (
              <span key={run.offset}>{run.text}</span>
            ),
          )}
        </span>
      ) : null}
    </button>
  )
}
