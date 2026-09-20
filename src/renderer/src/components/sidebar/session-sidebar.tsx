import { Fragment, useState } from 'react'
import { ContextMenu } from '@base-ui/react/context-menu'
import {
  Archive,
  ArchiveRestore,
  ChevronRight,
  Folder,
  Loader2,
  MessageSquare,
  Pin,
  PinOff,
  Plus,
  Settings,
  SquarePen,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  foldedWorkspaces,
  pinnedSessions,
  pinnedWorkspaces,
  toggleListed,
  usePreference,
} from '@/lib/local-preference'
import { cn } from '@/lib/utils'

const MENU =
  'min-w-44 rounded-lg border bg-popover p-1 text-sm text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none'
const MENU_ITEM =
  'flex items-center gap-2 rounded-md py-1.5 pl-2.5 pr-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground'
import {
  OLDER_BATCH,
  visibleSessions,
  type SessionSummary,
  type WorkspaceGroup,
} from '@/daemon/sessions'

export function SessionSidebar({
  groups,
  selectedSessionId,
  workingSessionIds,
  onSelect,
  onArchiveToggle,
  isLoading,
  error,
  onNewSession,
  onNewSessionIn,
  onSettings,
  insetTop,
}: {
  groups: WorkspaceGroup[]
  selectedSessionId: string | null
  /** Sessions the Daemon is working in right now; they get a spinner. */
  workingSessionIds: ReadonlySet<string>
  onSelect: (sessionId: string) => void
  /** From the row's context menu: archive, or unarchive when already archived. */
  onArchiveToggle: (session: SessionSummary) => void
  isLoading: boolean
  error: string | null
  onNewSession: () => void
  /** From a Workspace group's header: a new Session working in that Workspace. */
  onNewSessionIn: (workspace: string) => void
  onSettings: () => void
  insetTop: boolean
}) {
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  return (
    <nav
      aria-label="Sessions"
      className="flex h-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground"
    >
      {/* Drag strip; on wide screens the pinned sidebar toggle sits over it. */}
      <div
        className={cn(
          'app-drag h-[calc(env(safe-area-inset-top)+2.75rem)] shrink-0',
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
        {/* With a pin in place the list splits into Pinned and Workspaces. */}
        {groups.map((group, index) => {
          const isPinned = pinnedGroups.includes(group.key)
          const firstPinned = index === 0 && isPinned
          const firstRest =
            !isPinned && (index === 0 ? false : pinnedGroups.includes(groups[index - 1]!.key))
          return (
            <Fragment key={group.key}>
              {firstPinned ? <SectionLabel>Pinned</SectionLabel> : null}
              {firstRest ? <SectionLabel>Workspaces</SectionLabel> : null}
              <WorkspaceSection
                group={group}
                selectedSessionId={selectedSessionId}
                workingSessionIds={workingSessionIds}
                onSelect={onSelect}
                onArchiveToggle={onArchiveToggle}
                onNewSessionIn={onNewSessionIn}
              />
            </Fragment>
          )
        })}
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
  workingSessionIds,
  onSelect,
  onArchiveToggle,
  onNewSessionIn,
}: {
  group: WorkspaceGroup
  selectedSessionId: string | null
  workingSessionIds: ReadonlySet<string>
  onSelect: (sessionId: string) => void
  onArchiveToggle: (session: SessionSummary) => void
  onNewSessionIn: (workspace: string) => void
}) {
  // Folds and pins are this Client's; they outlive a restart.
  const [folded] = usePreference(foldedWorkspaces)
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  const [pinnedIds] = usePreference(pinnedSessions)
  const open = !folded.includes(group.key)
  const pinned = pinnedGroups.includes(group.key)
  const [revealed, setRevealed] = useState(0)
  const { visible, hidden } = visibleSessions(
    group.sessions,
    revealed,
    undefined,
    new Set(pinnedIds),
  )
  const listId = `workspace-${group.key.replace(/[^a-zA-Z0-9_-]/g, '_')}`
  return (
    <section aria-label={group.label} className="mb-2">
      <ContextMenu.Root>
        <ContextMenu.Trigger
          render={
            <h2 className="group/ws flex h-8 items-center text-[13px] font-medium text-foreground" />
          }
        >
          <button
            type="button"
            aria-expanded={open}
            aria-controls={listId}
            title={group.path}
            onClick={() => toggleListed(foldedWorkspaces, group.key)}
            className="flex h-full min-w-0 flex-1 items-center gap-2 rounded-lg px-2 text-left outline-none transition-colors hover:bg-sidebar-accent/60 focus-visible:ring-2 focus-visible:ring-sidebar-ring"
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
            {pinned ? (
              <Pin aria-label="Pinned" className="size-3 shrink-0 text-muted-foreground" />
            ) : null}
          </button>
          {/* Shows on hover and when focused, so the keyboard reaches it too. */}
          <button
            type="button"
            aria-label={`New session in ${group.label}`}
            title={`New session in ${group.label}`}
            onClick={() => onNewSessionIn(group.path)}
            className="mr-1 grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground opacity-0 outline-none transition-[opacity,color] hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring group-hover/ws:opacity-100"
          >
            <SquarePen aria-hidden className="size-3.5" />
          </button>
        </ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Positioner className="z-50 outline-none">
            <ContextMenu.Popup aria-label={`Actions for ${group.label}`} className={MENU}>
              <ContextMenu.Item
                onClick={() => toggleListed(pinnedWorkspaces, group.key)}
                className={MENU_ITEM}
              >
                {pinned ? (
                  <>
                    <PinOff aria-hidden className="size-4 text-muted-foreground" />
                    Unpin workspace
                  </>
                ) : (
                  <>
                    <Pin aria-hidden className="size-4 text-muted-foreground" />
                    Pin workspace
                  </>
                )}
              </ContextMenu.Item>
              <ContextMenu.Item onClick={() => onNewSessionIn(group.path)} className={MENU_ITEM}>
                <SquarePen aria-hidden className="size-4 text-muted-foreground" />
                New session here
              </ContextMenu.Item>
            </ContextMenu.Popup>
          </ContextMenu.Positioner>
        </ContextMenu.Portal>
      </ContextMenu.Root>
      <div id={listId} hidden={!open}>
        <ul className="flex flex-col gap-px">
          {visible.map((session) => {
            const selected = session.sessionId === selectedSessionId
            const working = workingSessionIds.has(session.sessionId)
            const sessionPinned = pinnedIds.includes(session.sessionId)
            return (
              <ContextMenu.Root key={session.sessionId}>
                <ContextMenu.Trigger render={<li />}>
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
                      {sessionPinned ? (
                        <Pin aria-label="Pinned" className="size-3 shrink-0 opacity-70" />
                      ) : null}
                      {session.archivedAt ? (
                        <Archive aria-label="Archived" className="size-3 shrink-0 opacity-70" />
                      ) : null}
                    </span>
                    <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                      {working ? (
                        <span
                          role="status"
                          aria-label="Working"
                          className="flex min-w-0 items-center gap-1 text-sky-600 dark:text-sky-400"
                        >
                          <Loader2 aria-hidden className="size-3 shrink-0 animate-spin" />
                          <span className="truncate">Working</span>
                        </span>
                      ) : session.messagesCount !== null ? (
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
                </ContextMenu.Trigger>
                <ContextMenu.Portal>
                  <ContextMenu.Positioner className="z-50 outline-none">
                    <ContextMenu.Popup aria-label={`Actions for ${session.title}`} className={MENU}>
                      <ContextMenu.Item
                        onClick={() => toggleListed(pinnedSessions, session.sessionId)}
                        className={MENU_ITEM}
                      >
                        {sessionPinned ? (
                          <>
                            <PinOff aria-hidden className="size-4 text-muted-foreground" />
                            Unpin
                          </>
                        ) : (
                          <>
                            <Pin aria-hidden className="size-4 text-muted-foreground" />
                            Pin
                          </>
                        )}
                      </ContextMenu.Item>
                      <ContextMenu.Item
                        onClick={() => onArchiveToggle(session)}
                        className={MENU_ITEM}
                      >
                        {session.archivedAt ? (
                          <>
                            <ArchiveRestore aria-hidden className="size-4 text-muted-foreground" />
                            Unarchive
                          </>
                        ) : (
                          <>
                            <Archive aria-hidden className="size-4 text-muted-foreground" />
                            Archive
                          </>
                        )}
                      </ContextMenu.Item>
                    </ContextMenu.Popup>
                  </ContextMenu.Positioner>
                </ContextMenu.Portal>
              </ContextMenu.Root>
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

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="mt-1 mb-1 px-2 text-[11px] font-medium text-muted-foreground/80">{children}</p>
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
