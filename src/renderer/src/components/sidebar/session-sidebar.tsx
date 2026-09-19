import { FolderGit2, MessageSquare } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { WorkspaceGroup } from '@/daemon/sessions'

export function SessionSidebar({
  groups,
  selectedSessionId,
  onSelect,
  isLoading,
  error,
}: {
  groups: WorkspaceGroup[]
  selectedSessionId: string | null
  onSelect: (sessionId: string) => void
  isLoading: boolean
  error: string | null
}) {
  return (
    <nav
      aria-label="Sessions"
      className="flex h-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground"
    >
      <div className="flex-1 overflow-y-auto px-2 py-2">
        {error ? (
          <p role="alert" className="px-2 py-1 text-xs text-destructive-foreground">
            {error}
          </p>
        ) : null}
        {isLoading && groups.length === 0 ? <SidebarSkeleton /> : null}
        {!isLoading && groups.length === 0 && !error ? (
          <p className="px-2 py-1 text-xs text-muted-foreground">No sessions yet.</p>
        ) : null}
        {groups.map((group) => (
          <section key={group.key} aria-label={group.label} className="mb-3">
            <h2
              title={group.path}
              className="flex items-center gap-1.5 px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground"
            >
              <FolderGit2 aria-hidden className="size-3" />
              <span className="truncate">{group.label}</span>
            </h2>
            <ul className="flex flex-col gap-px">
              {group.sessions.map((session) => {
                const selected = session.sessionId === selectedSessionId
                return (
                  <li key={session.sessionId}>
                    <button
                      type="button"
                      aria-current={selected ? 'page' : undefined}
                      onClick={() => onSelect(session.sessionId)}
                      title={session.title}
                      className={cn(
                        'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm outline-none transition-colors',
                        'hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring',
                        selected && 'bg-sidebar-accent text-sidebar-accent-foreground',
                      )}
                    >
                      <MessageSquare
                        aria-hidden
                        className="size-3.5 shrink-0 text-muted-foreground"
                      />
                      <span className="flex-1 truncate">{session.title}</span>
                      <time
                        dateTime={new Date(session.updatedAt * 1000).toISOString()}
                        className="shrink-0 text-[11px] text-muted-foreground"
                      >
                        {relativeTime(session.updatedAt * 1000)}
                      </time>
                    </button>
                  </li>
                )
              })}
            </ul>
          </section>
        ))}
      </div>
    </nav>
  )
}

function SidebarSkeleton() {
  return (
    <div aria-hidden className="flex flex-col gap-2 px-2 py-1">
      {[0, 1, 2, 3].map((i) => (
        <div
          key={i}
          className="h-6 animate-pulse rounded-md bg-muted/60"
          style={{ width: `${70 + (i % 3) * 10}%` }}
        />
      ))}
    </div>
  )
}

export function relativeTime(ms: number, now = Date.now()): string {
  const diff = Math.max(0, now - ms)
  const minutes = Math.floor(diff / 60_000)
  if (minutes < 1) return 'now'
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d`
  return new Date(ms).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}
