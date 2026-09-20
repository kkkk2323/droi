import { Archive, Folder, Plus, Settings } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { WorkspaceGroup } from '@/daemon/sessions'

export function SessionSidebar({
  groups,
  selectedSessionId,
  onSelect,
  isLoading,
  error,
  showArchived,
  onToggleArchived,
  onNewSession,
  onSettings,
  insetTop,
}: {
  groups: WorkspaceGroup[]
  selectedSessionId: string | null
  onSelect: (sessionId: string) => void
  isLoading: boolean
  error: string | null
  showArchived: boolean
  onToggleArchived: (show: boolean) => void
  onNewSession: () => void
  onSettings: (() => void) | null
  insetTop: boolean
}) {
  return (
    <nav
      aria-label="Sessions"
      className="flex h-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground"
    >
      <div
        className={cn(
          'app-drag flex h-11 shrink-0 items-center px-4 pt-[env(safe-area-inset-top)]',
          insetTop && 'pl-[76px]',
        )}
      >
        <h1 className="text-[13px] font-semibold tracking-tight text-foreground">Droi</h1>
      </div>

      <div className="flex flex-col gap-px px-2 pb-2">
        <SidebarRow icon={<Plus aria-hidden />} onClick={onNewSession}>
          New session
        </SidebarRow>
      </div>

      <div className="flex-1 overflow-y-auto px-2 pb-2">
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
          <section key={group.key} aria-label={group.label} className="mb-2">
            <h2
              title={group.path}
              className="flex h-8 items-center gap-2 px-2 text-[13px] font-medium text-foreground"
            >
              <Folder aria-hidden className="size-4 shrink-0 text-muted-foreground" />
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
                        'flex h-8 w-full items-center gap-2 rounded-lg pl-8 pr-2 text-left text-[13px] text-muted-foreground outline-none transition-colors duration-150',
                        'hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring',
                        selected && 'bg-sidebar-accent text-sidebar-accent-foreground',
                      )}
                    >
                      <span className="flex-1 truncate">{session.title}</span>
                      {session.archivedAt ? (
                        <Archive aria-label="Archived" className="size-3 shrink-0 opacity-70" />
                      ) : null}
                      <time
                        dateTime={new Date(session.updatedAt * 1000).toISOString()}
                        className="shrink-0 text-[11px] tabular-nums opacity-70"
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

      <div className="flex shrink-0 items-center gap-1 px-2 pb-2 pt-1">
        {onSettings ? (
          <Button size="icon-sm" variant="ghost" aria-label="Settings" onClick={onSettings}>
            <Settings aria-hidden />
          </Button>
        ) : null}
        <label
          className={cn(
            'relative ml-auto flex h-8 select-none items-center gap-1.5 rounded-lg px-2 text-xs text-muted-foreground transition-colors hover:bg-sidebar-accent/60 hover:text-foreground has-[input:focus-visible]:ring-2 has-[input:focus-visible]:ring-sidebar-ring',
            showArchived && 'text-foreground',
          )}
        >
          <input
            type="checkbox"
            aria-label="Show archived"
            checked={showArchived}
            onChange={(event) => onToggleArchived(event.target.checked)}
            className="absolute inset-0 cursor-pointer opacity-0"
          />
          <Archive aria-hidden className="size-3.5" />
          Show archived
        </label>
      </div>
    </nav>
  )
}

function SidebarRow({
  icon,
  onClick,
  children,
}: {
  icon: React.ReactNode
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex h-8 w-full items-center gap-2 rounded-lg px-2 text-left text-[13px] text-foreground outline-none transition-colors duration-150 hover:bg-sidebar-accent/60 focus-visible:ring-2 focus-visible:ring-sidebar-ring [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-muted-foreground"
    >
      {icon}
      {children}
    </button>
  )
}

function SidebarSkeleton() {
  return (
    <div aria-hidden className="flex flex-col gap-2 px-2 py-1">
      {[0, 1, 2, 3].map((i) => (
        <div
          key={i}
          className="h-6 animate-pulse rounded-md bg-sidebar-accent/70"
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
