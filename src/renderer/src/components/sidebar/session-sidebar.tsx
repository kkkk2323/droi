import { useState } from 'react'
import { Archive, ChevronRight, Folder, MessageSquare, Plus, Settings } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { OLDER_BATCH, visibleSessions, type WorkspaceGroup } from '@/daemon/sessions'

export function SessionSidebar({
  groups,
  selectedSessionId,
  onSelect,
  isLoading,
  error,
  onNewSession,
  onSettings,
  insetTop,
}: {
  groups: WorkspaceGroup[]
  selectedSessionId: string | null
  onSelect: (sessionId: string) => void
  isLoading: boolean
  error: string | null
  onNewSession: () => void
  onSettings: () => void
  insetTop: boolean
}) {
  return (
    <nav
      aria-label="Sessions"
      className="flex h-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground"
    >
      {/* Drag strip; on wide screens the pinned sidebar toggle sits over it. */}
      <div
        className={cn(
          'app-drag h-11 shrink-0 pt-[env(safe-area-inset-top)]',
          insetTop && 'pl-[76px]',
        )}
      />

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
          <WorkspaceSection
            key={group.key}
            group={group}
            selectedSessionId={selectedSessionId}
            onSelect={onSelect}
          />
        ))}
      </div>

      <div className="flex shrink-0 items-center px-2 pb-2 pt-1">
        <Button size="icon-sm" variant="ghost" aria-label="Settings" onClick={onSettings}>
          <Settings aria-hidden />
        </Button>
      </div>
    </nav>
  )
}

function WorkspaceSection({
  group,
  selectedSessionId,
  onSelect,
}: {
  group: WorkspaceGroup
  selectedSessionId: string | null
  onSelect: (sessionId: string) => void
}) {
  const [open, setOpen] = useState(true)
  const [revealed, setRevealed] = useState(0)
  const { visible, hidden } = visibleSessions(group.sessions, revealed)
  const listId = `workspace-${group.key.replace(/[^a-zA-Z0-9_-]/g, '_')}`
  return (
    <section aria-label={group.label} className="mb-2">
      <h2 className="group/ws flex h-8 items-center text-[13px] font-medium text-foreground">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={listId}
          title={group.path}
          onClick={() => setOpen(!open)}
          className="flex h-full w-full items-center gap-2 rounded-lg px-2 text-left outline-none transition-colors hover:bg-sidebar-accent/60 focus-visible:ring-2 focus-visible:ring-sidebar-ring"
        >
          <span className="relative size-4 shrink-0 text-muted-foreground">
            <Folder
              aria-hidden
              className="absolute inset-0 size-4 transition-opacity group-hover/ws:opacity-0"
            />
            <ChevronRight
              aria-hidden
              className={cn(
                'absolute inset-0 size-4 opacity-0 transition-[opacity,transform] duration-150 group-hover/ws:opacity-100',
                open && 'rotate-90',
              )}
            />
          </span>
          <span className="truncate">{group.label}</span>
        </button>
      </h2>
      <div id={listId} hidden={!open}>
        <ul className="flex flex-col gap-px">
          {visible.map((session) => {
            const selected = session.sessionId === selectedSessionId
            return (
              <li key={session.sessionId}>
                <button
                  type="button"
                  aria-current={selected ? 'page' : undefined}
                  onClick={() => onSelect(session.sessionId)}
                  title={session.title}
                  className={cn(
                    'flex w-full flex-col gap-0.5 rounded-lg py-1.5 pl-8 pr-2 text-left outline-none transition-colors duration-150',
                    'hover:bg-sidebar-accent/60 focus-visible:ring-2 focus-visible:ring-sidebar-ring',
                    selected && 'bg-sidebar-accent',
                  )}
                >
                  <span className="flex items-center gap-1.5 text-[13px] text-foreground">
                    <span className="flex-1 truncate">{session.title}</span>
                    {session.archivedAt ? (
                      <Archive aria-label="Archived" className="size-3 shrink-0 opacity-70" />
                    ) : null}
                  </span>
                  <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                    {session.messagesCount !== null ? (
                      <span className="flex min-w-0 items-center gap-1">
                        <MessageSquare aria-hidden className="size-3 shrink-0" />
                        <span className="truncate">
                          {session.messagesCount}{' '}
                          {session.messagesCount === 1 ? 'message' : 'messages'}
                        </span>
                      </span>
                    ) : null}
                    <time
                      dateTime={new Date(session.updatedAt * 1000).toISOString()}
                      className="ml-auto shrink-0 tabular-nums"
                    >
                      {relativeTime(session.updatedAt * 1000)}
                    </time>
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
        {hidden > 0 ? (
          <button
            type="button"
            onClick={() => setRevealed(revealed + OLDER_BATCH)}
            className="flex h-7 w-full items-center rounded-lg pl-8 pr-2 text-left text-[11px] text-muted-foreground outline-none transition-colors hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring"
          >
            Show {hidden} older
          </button>
        ) : null}
      </div>
    </section>
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
