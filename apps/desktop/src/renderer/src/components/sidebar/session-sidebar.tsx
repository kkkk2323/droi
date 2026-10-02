import { useRef, useState, type DragEvent, type RefObject } from 'react'
import { ContextMenu } from '@base-ui/react/context-menu'
import { Menu } from '@base-ui/react/menu'
import {
  Archive,
  ArchiveRestore,
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Copy,
  Fingerprint,
  Folder,
  FolderOpen,
  ListFilter,
  MessageSquare,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Settings,
  SquarePen,
} from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import { relativeTime } from './relative-time'
import { SessionSearchBox, SessionSearchResults } from './session-search'
import { Button } from '@/components/ui/button'
import {
  foldedWorkspaces,
  pinnedSessions,
  pinnedWorkspaces,
  toggleListed,
  usePreference,
} from '@droi/daemon-layer/local-preference'
import { cn } from '@/lib/utils'
import type { CopyWhat } from '@/lib/copy-session'

const MENU =
  'min-w-44 rounded-lg border bg-popover p-1 text-sm text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none'
const MENU_ITEM =
  'flex items-center gap-2 rounded-md py-1.5 pl-2.5 pr-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground'
// Folded like a Workspace group, in the same preference; no Workspace key has this prefix.
const PINNED_SECTION_KEY = 'droi:pinned'
const WORKSPACES_SECTION_KEY = 'droi:workspaces'
import { countBusy, type Busy, type SessionActivity } from '@droi/daemon-layer/use-session-activity'
import type { SessionSortControls } from '@droi/daemon-layer/use-session-sort'
import {
  OLDER_BATCH,
  SESSION_SORT_LABELS,
  WORKSPACE_SORT_LABELS,
  moveWorkspace,
  visibleSessions,
  type SessionSort,
  type SessionSummary,
  type WorkspaceGroup,
  type WorkspaceSort,
} from '@droi/daemon-layer/sessions'

const WORKSPACE_DRAG_TYPE = 'application/x-droi-workspace'

/** Moving Workspaces by hand: the order shown, and where a drag would land. */
interface Reorder {
  shown: string[]
  move: (key: string, target: string, place: 'before' | 'after') => void
  over: { key: string; place: 'before' | 'after' } | null
  setOver: (over: { key: string; place: 'before' | 'after' } | null) => void
}

export function SessionSidebar({
  groups,
  sort,
  selectedSessionId,
  activity,
  subagentsRunning,
  unread,
  onSelect,
  onArchiveToggle,
  onRename,
  onCopy,
  isLoading,
  error,
  older,
  onNewSession,
  onNewSessionIn,
  onSettings,
  insetTop,
}: {
  groups: WorkspaceGroup[]
  sort: SessionSortControls
  selectedSessionId: string | null
  /** What the Daemon is doing in each busy Session: a spinner, or a call for an answer. */
  activity: ReadonlyMap<string, SessionActivity>
  /** How many subagents each row's Session has running. */
  subagentsRunning: ReadonlyMap<string, number>
  /** Sessions that finished or started waiting while another one was open. */
  unread: ReadonlySet<string>
  onSelect: (sessionId: string) => void
  /** From the row's context menu: archive, or unarchive when already archived. */
  onArchiveToggle: (session: SessionSummary) => void
  /** From the row's context menu, once the new title is entered in the row. */
  onRename: (session: SessionSummary, title: string) => void
  /** From the row's context menu: the Session's id, or its details for another Session to read. */
  onCopy: (session: SessionSummary, what: CopyWhat) => void
  isLoading: boolean
  error: string | null
  /** Sessions older than the ones listed wait on the Daemon; null when the list is complete. */
  older: { loading: boolean; load: () => void } | null
  onNewSession: () => void
  /** From a group's header: a new Session in that Workspace, or with None from Recents. */
  onNewSessionIn: (workspace: string | null) => void
  onSettings: () => void
  insetTop: boolean
}) {
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  const [query, setQuery] = useState('')
  const recents = groups.find((group) => group.scratch)
  const pinned = groups.filter((group) => !group.scratch && pinnedGroups.includes(group.key))
  const rest = groups.filter((group) => !group.scratch && !pinnedGroups.includes(group.key))
  const [over, setOver] = useState<Reorder['over']>(null)
  const reorder: Reorder | null =
    sort.order.workspaces === 'manual'
      ? {
          shown: groups.filter((g) => !g.scratch).map((g) => g.key),
          move: (key, target, place) => {
            sort.setManual(
              moveWorkspace(
                groups.filter((g) => !g.scratch).map((g) => g.key),
                key,
                target,
                place,
              ),
            )
          },
          over,
          setOver,
        }
      : null
  const busy = (list: readonly WorkspaceGroup[]) =>
    countBusy(
      list.flatMap((g) => g.sessions),
      activity,
      subagentsRunning,
    )
  const section = (group: WorkspaceGroup) => (
    <WorkspaceSection
      key={group.key}
      group={group}
      reorder={group.scratch ? null : reorder}
      selectedSessionId={selectedSessionId}
      activity={activity}
      subagentsRunning={subagentsRunning}
      unread={unread}
      onSelect={onSelect}
      onArchiveToggle={onArchiveToggle}
      onRename={onRename}
      onCopy={onCopy}
      onNewSessionIn={onNewSessionIn}
    />
  )
  return (
    <nav
      aria-label="Sessions"
      data-surface="sidebar"
      className="flex h-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground"
    >
      {/* Drag strip; on wide screens the pinned sidebar toggle sits over it. */}
      <div
        className={cn(
          'app-drag h-[calc(env(safe-area-inset-top)+2.75rem)] shrink-0',
          insetTop && 'pl-[76px]',
        )}
      />

      <div className="flex flex-col gap-1 px-2 pb-2">
        <SidebarRow icon={<Plus aria-hidden />} onClick={onNewSession}>
          New session
        </SidebarRow>
        <div className="flex items-center gap-1">
          <div className="min-w-0 flex-1">
            <SessionSearchBox query={query} onChange={setQuery} />
          </div>
          <SortMenu
            sort={sort}
            // Manual starts from what is on screen, unless the user already placed some.
            onWorkspaces={(value) => {
              if (value === 'manual' && sort.order.manual.length === 0) {
                sort.setManual(groups.filter((g) => !g.scratch).map((g) => g.key))
              }
              sort.setWorkspaces(value)
            }}
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-2 pb-2">
        {query.trim() ? (
          <SessionSearchResults
            query={query}
            selectedSessionId={selectedSessionId}
            onSelect={onSelect}
          />
        ) : (
          <>
            {error ? (
              <p role="alert" className="px-2 py-1 text-xs text-destructive-foreground">
                {error}
              </p>
            ) : null}
            {isLoading && groups.length === 0 ? <SidebarSkeleton /> : null}
            {!isLoading && groups.length === 0 && !error ? (
              <p className="px-2 py-1 text-xs text-muted-foreground">No sessions yet.</p>
            ) : null}
            {/* The Workspaces label only appears when there is another section to tell it from. */}
            {pinned.length > 0 ? (
              <FoldableSection
                label="Pinned"
                id="sidebar-pinned"
                foldKey={PINNED_SECTION_KEY}
                busy={busy(pinned)}
              >
                {pinned.map(section)}
              </FoldableSection>
            ) : null}
            {rest.length > 0 && (pinned.length > 0 || recents) ? (
              <FoldableSection
                label="Workspaces"
                id="sidebar-workspaces"
                foldKey={WORKSPACES_SECTION_KEY}
                busy={busy(rest)}
              >
                {rest.map(section)}
              </FoldableSection>
            ) : (
              rest.map(section)
            )}
            {recents ? section(recents) : null}
            {older ? (
              <button
                type="button"
                disabled={older.loading}
                onClick={older.load}
                className="mt-1 flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-60"
              >
                {older.loading ? (
                  <Spinner aria-hidden className="size-3" />
                ) : (
                  <ChevronDown aria-hidden className="size-3.5" />
                )}
                {older.loading ? 'Loading older sessions…' : 'Load older sessions'}
              </button>
            ) : null}
          </>
        )}
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
  reorder,
  selectedSessionId,
  activity,
  subagentsRunning,
  unread,
  onSelect,
  onArchiveToggle,
  onRename,
  onCopy,
  onNewSessionIn,
}: {
  group: WorkspaceGroup
  /** Set while the Workspaces are in the manual order. */
  reorder: Reorder | null
  selectedSessionId: string | null
  activity: ReadonlyMap<string, SessionActivity>
  subagentsRunning: ReadonlyMap<string, number>
  unread: ReadonlySet<string>
  onSelect: (sessionId: string) => void
  onArchiveToggle: (session: SessionSummary) => void
  onRename: (session: SessionSummary, title: string) => void
  onCopy: (session: SessionSummary, what: CopyWhat) => void
  onNewSessionIn: (workspace: string | null) => void
}) {
  const [renaming, setRenaming] = useState<string | null>(null)
  const renameInput = useRef<HTMLInputElement>(null)
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
  const newHere = () => onNewSessionIn(group.scratch ? null : group.path)
  const toggle = () => toggleListed(foldedWorkspaces, group.key)
  // Shows on hover and when focused, so the keyboard reaches it too.
  const newButton = (
    <button
      type="button"
      aria-label={`New session in ${group.label}`}
      title={`New session in ${group.label}`}
      onClick={newHere}
      className="mr-1 grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground opacity-0 outline-none transition-[opacity,color,background-color] hover:bg-sidebar-accent hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring group-hover/ws:opacity-100"
    >
      <SquarePen aria-hidden className="size-3.5" />
    </button>
  )
  // A folded group still says what its Sessions are doing.
  const foldedBusy = open ? null : countBusy(group.sessions, activity, subagentsRunning)
  const FolderIcon = open ? FolderOpen : Folder
  const at = reorder ? reorder.shown.indexOf(group.key) : -1
  const above = reorder && at > 0 ? reorder.shown[at - 1] : undefined
  const below = reorder && at >= 0 ? reorder.shown[at + 1] : undefined
  const dropping = reorder?.over?.key === group.key ? reorder.over.place : null
  const dragProps = reorder
    ? {
        draggable: true,
        onDragStart: (event: DragEvent) => {
          event.dataTransfer.effectAllowed = 'move'
          event.dataTransfer.setData(WORKSPACE_DRAG_TYPE, group.key)
        },
        onDragEnd: () => reorder.setOver(null),
      }
    : {}
  const dropProps = reorder
    ? {
        onDragOver: (event: DragEvent) => {
          if (!event.dataTransfer.types.includes(WORKSPACE_DRAG_TYPE)) return
          event.preventDefault()
          event.dataTransfer.dropEffect = 'move'
          const box = event.currentTarget.getBoundingClientRect()
          const place = event.clientY < box.top + box.height / 2 ? 'before' : 'after'
          if (reorder.over?.key !== group.key || reorder.over.place !== place) {
            reorder.setOver({ key: group.key, place })
          }
        },
        onDragLeave: (event: DragEvent) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
            reorder.setOver(null)
          }
        },
        onDrop: (event: DragEvent) => {
          const key = event.dataTransfer.getData(WORKSPACE_DRAG_TYPE)
          event.preventDefault()
          const place = reorder.over?.key === group.key ? reorder.over.place : 'before'
          reorder.setOver(null)
          if (key) reorder.move(key, group.key, place)
        },
      }
    : {}
  return (
    <section
      aria-label={group.label}
      className={cn(
        'relative mb-2',
        // Where a dragged Workspace would land.
        dropping === 'before' &&
          'before:absolute before:inset-x-2 before:-top-1 before:h-0.5 before:rounded-full before:bg-primary',
        dropping === 'after' &&
          'after:absolute after:inset-x-2 after:-bottom-1 after:h-0.5 after:rounded-full after:bg-primary',
      )}
      {...dropProps}
    >
      <ContextMenu.Root>
        {group.scratch ? (
          // Recents is a section of its own, like Pinned and Workspaces, not a folder.
          <ContextMenu.Trigger render={<div />}>
            <SectionHeader
              label={group.label}
              title="Sessions without a workspace"
              open={open}
              controls={listId}
              onToggle={toggle}
              action={newButton}
              busy={foldedBusy}
            />
          </ContextMenu.Trigger>
        ) : (
          <ContextMenu.Trigger
            render={
              <h2
                className={cn(
                  'group/ws flex h-8 items-center rounded-lg text-[13px] font-medium text-foreground transition-colors hover:bg-sidebar-accent/60',
                  reorder && 'cursor-grab active:cursor-grabbing',
                )}
                {...dragProps}
              />
            }
          >
            <button
              type="button"
              aria-expanded={open}
              aria-controls={listId}
              title={group.path}
              onClick={toggle}
              className="flex h-full min-w-0 flex-1 items-center gap-2 rounded-lg px-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring"
            >
              <span className="relative grid size-4 shrink-0 place-items-center text-muted-foreground">
                <FolderIcon
                  aria-hidden
                  strokeWidth={1.75}
                  className="size-3.5 transition-opacity group-hover/ws:opacity-0"
                />
                <ChevronRight
                  aria-hidden
                  className={cn(
                    'absolute size-3.5 opacity-0 transition-[opacity,transform] duration-150 group-hover/ws:opacity-100',
                    open && 'rotate-90',
                  )}
                />
              </span>
              <span className="truncate">{group.label}</span>
              {foldedBusy ? <BusyMark busy={foldedBusy} /> : null}
            </button>
            {newButton}
          </ContextMenu.Trigger>
        )}
        <ContextMenu.Portal>
          <ContextMenu.Positioner className="z-50 outline-none">
            <ContextMenu.Popup aria-label={`Actions for ${group.label}`} className={MENU}>
              {reorder && (above || below) ? (
                <>
                  <ContextMenu.Item
                    disabled={!above}
                    onClick={() => above && reorder.move(group.key, above, 'before')}
                    className={cn(MENU_ITEM, 'data-[disabled]:opacity-50')}
                  >
                    <ArrowUp aria-hidden className="size-4 text-muted-foreground" />
                    Move up
                  </ContextMenu.Item>
                  <ContextMenu.Item
                    disabled={!below}
                    onClick={() => below && reorder.move(group.key, below, 'after')}
                    className={cn(MENU_ITEM, 'data-[disabled]:opacity-50')}
                  >
                    <ArrowDown aria-hidden className="size-4 text-muted-foreground" />
                    Move down
                  </ContextMenu.Item>
                  <ContextMenu.Separator className="my-1 h-px bg-border" />
                </>
              ) : null}
              {/* Recents always sits at the bottom. */}
              {group.scratch ? null : (
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
              )}
              <ContextMenu.Item onClick={newHere} className={MENU_ITEM}>
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
            const doing = activity.get(session.sessionId)
            const subagents = subagentsRunning.get(session.sessionId) ?? 0
            const isUnread = unread.has(session.sessionId)
            const sessionPinned = pinnedIds.includes(session.sessionId)
            return (
              <ContextMenu.Root key={session.sessionId}>
                <ContextMenu.Trigger render={<li />}>
                  {renaming === session.sessionId ? (
                    <RenameField
                      title={session.title}
                      inputRef={renameInput}
                      className={group.scratch ? 'pl-2' : 'pl-8'}
                      onDone={(title) => {
                        setRenaming(null)
                        if (title) onRename(session, title)
                      }}
                    />
                  ) : (
                    <button
                      type="button"
                      aria-current={selected ? 'page' : undefined}
                      onClick={() => onSelect(session.sessionId)}
                      title={session.title}
                      className={cn(
                        'flex w-full flex-col gap-0.5 rounded-lg py-1.5 pr-2 text-left outline-none transition-colors duration-150',
                        group.scratch ? 'pl-2' : 'pl-8',
                        'hover:bg-sidebar-accent/60 focus-visible:ring-2 focus-visible:ring-sidebar-ring',
                        selected && 'bg-sidebar-accent',
                      )}
                    >
                      <span className="flex items-center gap-1.5 text-[13px] text-foreground">
                        <span className={cn('flex-1 truncate', isUnread && 'font-medium')}>
                          {session.title}
                        </span>
                        {isUnread ? (
                          <span
                            role="img"
                            aria-label="Unread"
                            className="size-1.5 shrink-0 rounded-full bg-info"
                          />
                        ) : null}
                        {sessionPinned ? (
                          <Pin aria-label="Pinned" className="size-3 shrink-0 opacity-70" />
                        ) : null}
                        {session.archivedAt ? (
                          <Archive aria-label="Archived" className="size-3 shrink-0 opacity-70" />
                        ) : null}
                      </span>
                      <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                        {doing === 'needs-input' ? (
                          <span
                            role="status"
                            aria-label="Needs input"
                            className="flex min-w-0 items-center gap-1 text-attention"
                          >
                            <CircleAlert aria-hidden className="size-3 shrink-0" />
                            <span className="truncate">Needs input</span>
                          </span>
                        ) : doing === 'compacting' ? (
                          <span
                            role="status"
                            aria-label="Compacting"
                            className="flex min-w-0 items-center gap-1 text-info"
                          >
                            <Spinner aria-hidden className="size-3" />
                            <span className="truncate">Compacting</span>
                          </span>
                        ) : subagents > 0 ? (
                          <span role="status" className="flex min-w-0 items-center gap-1 text-info">
                            <Spinner aria-hidden className="size-3" />
                            <span className="truncate">
                              {subagents} {subagents === 1 ? 'subagent' : 'subagents'} running
                            </span>
                          </span>
                        ) : doing === 'working' ? (
                          <span
                            role="status"
                            aria-label="Working"
                            className="flex min-w-0 items-center gap-1 text-info"
                          >
                            <Spinner aria-hidden className="size-3" />
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
                  )}
                </ContextMenu.Trigger>
                <ContextMenu.Portal>
                  <ContextMenu.Positioner className="z-50 outline-none">
                    <ContextMenu.Popup
                      aria-label={`Actions for ${session.title}`}
                      // Closing the menu would otherwise take the focus back from the rename field.
                      finalFocus={() => renameInput.current ?? true}
                      className={MENU}
                    >
                      <ContextMenu.Item
                        onClick={() => setRenaming(session.sessionId)}
                        className={MENU_ITEM}
                      >
                        <Pencil aria-hidden className="size-4 text-muted-foreground" />
                        Rename
                      </ContextMenu.Item>
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
                      <ContextMenu.Separator className="my-1 h-px bg-border" />
                      <ContextMenu.Item onClick={() => onCopy(session, 'id')} className={MENU_ITEM}>
                        <Fingerprint aria-hidden className="size-4 text-muted-foreground" />
                        Copy session ID
                      </ContextMenu.Item>
                      <ContextMenu.Item
                        onClick={() => onCopy(session, 'details')}
                        className={MENU_ITEM}
                      >
                        <Copy aria-hidden className="size-4 text-muted-foreground" />
                        Copy session details
                      </ContextMenu.Item>
                      <ContextMenu.Separator className="my-1 h-px bg-border" />
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
            className={cn(
              'flex h-7 w-full items-center rounded-lg pr-2 text-left text-[11px] text-muted-foreground outline-none transition-colors hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring',
              group.scratch ? 'pl-2' : 'pl-8',
            )}
          >
            Show {hidden} older
          </button>
        ) : null}
      </div>
    </section>
  )
}

/** A row's title as a field: Enter or clicking away saves, Escape keeps the title. */
function RenameField({
  title,
  inputRef,
  className,
  onDone,
}: {
  title: string
  inputRef: RefObject<HTMLInputElement | null>
  className: string
  /** The new title, or null when it is unchanged or the rename was abandoned. */
  onDone: (title: string | null) => void
}) {
  const [draft, setDraft] = useState(title)
  // Escape and Enter unmount the field, which may blur it once more.
  const done = useRef(false)
  const finish = (next: string | null) => {
    if (done.current) return
    done.current = true
    const trimmed = next?.trim()
    onDone(trimmed && trimmed !== title ? trimmed : null)
  }
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        finish(draft)
      }}
      className={cn('flex rounded-lg bg-sidebar-accent py-1.5 pr-2', className)}
    >
      <input
        ref={inputRef}
        aria-label="Session title"
        autoFocus
        value={draft}
        onFocus={(event) => event.currentTarget.select()}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== 'Escape') return
          // Only the rename is abandoned, not the drawer the sidebar sits in on a phone.
          event.preventDefault()
          event.stopPropagation()
          finish(null)
        }}
        onBlur={() => finish(draft)}
        className="h-8 min-w-0 flex-1 rounded-md border bg-background px-1.5 text-[13px] text-foreground outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring"
      />
    </form>
  )
}

/** A section's label row: click folds the section; the chevron shows on hover and focus. */
function SectionHeader({
  label,
  title,
  open,
  controls,
  onToggle,
  action,
  busy = null,
}: {
  label: string
  title?: string
  open: boolean
  controls: string
  onToggle: () => void
  action?: React.ReactNode
  /** What the folded section's Sessions are doing; null when open or idle. */
  busy?: Busy | null
}) {
  return (
    <div className="group/ws mt-1 flex h-7 items-center rounded-lg transition-colors hover:bg-sidebar-accent/60">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={controls}
        title={title}
        onClick={onToggle}
        className="flex h-full min-w-0 flex-1 items-center gap-1 rounded-lg px-2 text-left text-[11px] font-medium text-muted-foreground/80 outline-none transition-colors group-hover/ws:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring"
      >
        <span className="truncate">{label}</span>
        <ChevronRight
          aria-hidden
          className={cn(
            'size-3 shrink-0 opacity-0 transition-[opacity,transform] duration-150 group-focus-within/ws:opacity-100 group-hover/ws:opacity-100',
            open && 'rotate-90',
          )}
        />
        {busy && !open ? <BusyMark busy={busy} /> : null}
      </button>
      {action}
    </div>
  )
}

/** Pinned and Workspaces: a foldable run of Workspace groups under one label. */
function FoldableSection({
  label,
  id,
  foldKey,
  busy,
  children,
}: {
  label: string
  id: string
  foldKey: string
  busy: Busy | null
  children: React.ReactNode
}) {
  const [folded] = usePreference(foldedWorkspaces)
  const open = !folded.includes(foldKey)
  return (
    <div>
      <SectionHeader
        label={label}
        open={open}
        controls={id}
        onToggle={() => toggleListed(foldedWorkspaces, foldKey)}
        busy={busy}
      />
      <div id={id} hidden={!open}>
        {children}
      </div>
    </div>
  )
}

/** A folded group's summary: how many of its Sessions work or wait for an answer. */
function BusyMark({ busy }: { busy: Busy }) {
  const parts = [
    busy.needsInput > 0 ? `${busy.needsInput} needs input` : null,
    busy.working > 0 ? `${busy.working} working` : null,
  ].filter(Boolean)
  return (
    <span
      role="status"
      aria-label={parts.join(', ')}
      className="ml-auto flex shrink-0 items-center gap-2 pl-1 text-[11px] font-normal tabular-nums"
    >
      {busy.needsInput > 0 ? (
        <span className="flex items-center gap-1 text-attention">
          <CircleAlert aria-hidden className="size-3" />
          {busy.needsInput}
        </span>
      ) : null}
      {busy.working > 0 ? (
        <span className="flex items-center gap-1 text-info">
          <Spinner aria-hidden className="size-3" />
          {busy.working}
        </span>
      ) : null}
    </span>
  )
}

const SORT_ITEM =
  'grid grid-cols-[1fr_1rem] items-center gap-3 rounded-md py-1.5 pl-2.5 pr-2 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground'

/** How Workspaces and the Sessions in them (Recents too) are ordered. */
function SortMenu({
  sort,
  onWorkspaces,
}: {
  sort: SessionSortControls
  onWorkspaces: (sort: WorkspaceSort) => void
}) {
  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label="Sort"
        title="Sort"
        className="grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground outline-none transition-colors hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring data-[popup-open]:bg-sidebar-accent data-[popup-open]:text-foreground"
      >
        <ListFilter aria-hidden className="size-4" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner side="bottom" align="end" sideOffset={4} className="z-50 outline-none">
          <Menu.Popup className={MENU}>
            <Menu.Group>
              <Menu.GroupLabel className="px-2.5 pb-1 pt-1 text-xs text-muted-foreground">
                Sort workspaces
              </Menu.GroupLabel>
              <Menu.RadioGroup
                value={sort.order.workspaces}
                onValueChange={(value) => onWorkspaces(value as WorkspaceSort)}
              >
                {(Object.keys(WORKSPACE_SORT_LABELS) as WorkspaceSort[]).map((value) => (
                  <Menu.RadioItem key={value} value={value} closeOnClick className={SORT_ITEM}>
                    {WORKSPACE_SORT_LABELS[value]}
                    <Menu.RadioItemIndicator className="flex items-center justify-center">
                      <Check aria-hidden className="size-3.5" />
                    </Menu.RadioItemIndicator>
                  </Menu.RadioItem>
                ))}
              </Menu.RadioGroup>
            </Menu.Group>
            <Menu.Separator className="my-1 h-px bg-border" />
            <Menu.Group>
              <Menu.GroupLabel className="px-2.5 pb-1 pt-1 text-xs text-muted-foreground">
                Sort sessions
              </Menu.GroupLabel>
              <Menu.RadioGroup
                value={sort.order.sessions}
                onValueChange={(value) => sort.setSessions(value as SessionSort)}
              >
                {(Object.keys(SESSION_SORT_LABELS) as SessionSort[]).map((value) => (
                  <Menu.RadioItem key={value} value={value} closeOnClick className={SORT_ITEM}>
                    {SESSION_SORT_LABELS[value]}
                    <Menu.RadioItemIndicator className="flex items-center justify-center">
                      <Check aria-hidden className="size-3.5" />
                    </Menu.RadioItemIndicator>
                  </Menu.RadioItem>
                ))}
              </Menu.RadioGroup>
            </Menu.Group>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
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
