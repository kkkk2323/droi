// Session list and Workspace grouping. The list is the Kernel's conversation.list, a
// collection fetched on demand, so it lives in TanStack Query; index.changed keeps its
// rows current. The open Session's transcript lives in kernel-conversations.ts.
import { useInfiniteQuery, useQueryClient, type InfiniteData } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { useConnectionState, useKernelConnection } from './connection-context'
import type { SessionTimes } from './gateway'
import { kernelStore } from './kernel-state'
import { sessionIdOf, type ListItem } from './kernel-wire'
import type { SessionWorktree } from './worktree'

export interface SessionSummary {
  sessionId: string
  title: string
  cwd: string | null
  /** Grouping key the Daemon provides for checkouts; falls back to cwd. */
  repoRoot: string | null
  /** The Daemon-managed worktree the Session runs in; null for a checkout, or once it is removed. */
  worktree: SessionWorktree | null
  /** Unix epoch seconds: when it last did something. */
  updatedAt: number
  /** When it last did something, in ms. */
  activityAt: number
  /** When a person last saw it, in ms; null asks for the dot. */
  viewedAt: number | null
  /** The Kernel's label: chat, scratch, automation, … */
  kind: string | null
  messagesCount: number | null
  archivedAt: string | null
  tags: SessionTag[]
  /** The Session this one continues after a compaction, from a droid session's tags. */
  parentId: string | null
}

/** A Session as a link names it. */
export interface SessionRef {
  sessionId: string
  title: string
}

export interface SessionTag {
  name: string
  metadata?: Record<string, string>
}

/**
 * Tag a compaction's child Session carries so every Client can chain it to
 * its parent; the Daemon writes the link into the session file but does not
 * list it.
 */
export const CONTINUES_TAG = 'droi.continues'

export function continuationParent(tags: readonly SessionTag[] | undefined): string | null {
  return tags?.find((t) => t.name === CONTINUES_TAG)?.metadata?.['parent'] ?? null
}

/** Tags for a child Session: the parent's, minus any older link, plus the new one. */
export function continuationTags(parentId: string, inherited: readonly SessionTag[]): SessionTag[] {
  return [
    ...inherited.filter((t) => t.name !== CONTINUES_TAG),
    { name: CONTINUES_TAG, metadata: { parent: parentId } },
  ]
}

/**
 * Tag of a Session in a Scratch Workspace (ADR 0008). A compaction's child
 * inherits it with the rest of the parent's tags.
 */
export const SCRATCH_TAG = 'droi.scratch'

export function isScratch(tags: readonly SessionTag[] | undefined): boolean {
  return tags?.some((t) => t.name === SCRATCH_TAG) ?? false
}

/** The name the Gateway gives a Scratch Workspace's folder. */
const SCRATCH_FOLDER = /\/\d{4}-\d{2}-\d{2}-[0-9a-f]{6}\/?$/

/**
 * A Session in a Scratch Workspace. The Daemon lists a Session's tags from a
 * file beside its transcript, and some Sessions there come without it, so the
 * folder's name counts too.
 */
export function isScratchSession(session: Pick<SessionSummary, 'tags' | 'cwd'>): boolean {
  return isScratch(session.tags) || (session.cwd !== null && SCRATCH_FOLDER.test(session.cwd))
}

/** Group key of Recents, where every Scratch Session is listed. */
export const RECENTS_GROUP_KEY = 'droi:recents'
/** Group key of the pinned Sessions, taken out of their own groups. */
export const PINNED_SESSIONS_GROUP_KEY = 'droi:pinned-sessions'

/** Drops Sessions that another listed Session continues; the chain shows as its latest link. */
export function foldContinued(sessions: readonly SessionSummary[]): SessionSummary[] {
  // A Session compacted in place was once tagged with itself; that is no fold.
  const parents = new Set(
    sessions.flatMap((s) => (s.parentId && s.parentId !== s.sessionId ? [s.parentId] : [])),
  )
  return sessions.filter((s) => !parents.has(s.sessionId))
}

/** The listed Sessions this one continues after each compaction, nearest first. */
export function continuationChain(
  sessions: readonly SessionSummary[],
  session: SessionSummary,
): SessionSummary[] {
  const byId = new Map(sessions.map((s) => [s.sessionId, s]))
  const chain: SessionSummary[] = []
  const seen = new Set([session.sessionId])
  let parent = session.parentId ? byId.get(session.parentId) : undefined
  while (parent && !seen.has(parent.sessionId)) {
    chain.push(parent)
    seen.add(parent.sessionId)
    parent = parent.parentId ? byId.get(parent.parentId) : undefined
  }
  return chain
}

export interface WorkspaceGroup {
  key: string
  label: string
  /** The Workspace; empty for Recents, whose Sessions each have their own. */
  path: string
  /** Recents: the Sessions in Scratch Workspaces. */
  scratch: boolean
  sessions: SessionSummary[]
}

export const SESSIONS_QUERY_KEY = ['sessions'] as const

/** Conversations per page, the Kernel's most. */
export const SESSION_PAGE = 200

export interface SessionList {
  /** Every Session fetched so far, newest first; undefined before the first page. */
  data: SessionSummary[] | undefined
  isPending: boolean
  error: Error | null
  /** The computer has Sessions older than the last page. */
  hasMore: boolean
  isLoadingMore: boolean
  loadMore(): void
}

/** A list cursor and which list it pages: the live conversations, then the archived. */
interface PageParam {
  archived: boolean
  cursor: unknown
}

interface Page {
  sessions: SessionSummary[]
  next: PageParam | null
}

/** Conversations the Host runs for itself, which no list shows. */
const HIDDEN_KINDS = new Set(['memory'])

/**
 * The computer's Sessions, newest activity first, a page at a time. With archived ones,
 * the archive follows the live list.
 */
export function useSessionList(options: { includeArchived?: boolean } = {}): SessionList {
  const includeArchived = options.includeArchived ?? false
  const connection = useKernelConnection()
  // Read through the hook, not connection.getState(): the compiler memoizes
  // the options on `connection`, which never changes identity.
  const connected = useConnectionState().status === 'connected'
  const queryClient = useQueryClient()
  const generation = connected ? connection.generation() : -1

  useEffect(() => {
    return connection.onNotification(({ method, params }) => {
      if (method !== 'index.changed') return
      const id = sessionIdOf(params['id'] as number)
      let known = false
      let moved = false
      queryClient.setQueriesData<InfiniteData<Page>>({ queryKey: SESSIONS_QUERY_KEY }, (data) => {
        if (!data) return data
        return {
          ...data,
          pages: data.pages.map((page) => ({
            ...page,
            sessions: page.sessions.map((s) => {
              if (s.sessionId !== id) return s
              known = true
              const changed = withIndexChange(s, params)
              if (changed.archivedAt !== s.archivedAt) moved = true
              return changed
            }),
          })),
        }
      })
      // A new conversation, or one that moved between the live list and the archive.
      if (!known || moved) void queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
    })
  }, [connection, queryClient])

  const query = useInfiniteQuery({
    queryKey: [...SESSIONS_QUERY_KEY, { includeArchived, generation }],
    initialPageParam: { archived: false, cursor: null } as PageParam,
    queryFn: async ({ pageParam }): Promise<Page> => {
      const read = async (param: PageParam) => {
        const result = await connection.call<{ items: ListItem[]; cursor?: unknown }>(
          'conversation.list',
          {
            limit: SESSION_PAGE,
            ...(param.archived ? { archived: true } : {}),
            ...(param.cursor ? { cursor: param.cursor } : {}),
          },
        )
        kernelStore(connection).listed(result.items)
        return result
      }
      let result = await read(pageParam)
      let archived = pageParam.archived
      let items = result.items
      // The live list's end is where the archive starts: it comes with that page, not behind
      // "Load older sessions".
      if (!result.cursor && includeArchived && !archived) {
        archived = true
        result = await read({ archived, cursor: null })
        items = [...items, ...result.items]
      }
      return {
        sessions: items.filter((item) => !HIDDEN_KINDS.has(item.kind ?? '')).map(summaryOf),
        next: result.cursor ? { archived, cursor: result.cursor } : null,
      }
    },
    getNextPageParam: (last) => last.next ?? undefined,
    enabled: connected,
    staleTime: 10_000,
  })
  const pages = query.data?.pages
  const data = useMemo(() => pages?.flatMap((page) => page.sessions), [pages])
  const { fetchNextPage } = query
  return {
    data,
    isPending: query.isPending,
    error: query.error,
    hasMore: query.hasNextPage,
    isLoadingMore: query.isFetchingNextPage,
    loadMore: () => void fetchNextPage(),
  }
}

/** The automation a run of Droi's Automations belongs to is named in its title. */
const AUTOMATION_PREFIX = '[Automation] '

export function summaryOf(item: ListItem): SessionSummary {
  const tags: SessionTag[] = []
  if (item.kind === 'scratch') tags.push({ name: SCRATCH_TAG })
  if (item.kind === 'automation') {
    const sessionId = sessionIdOf(item.id)
    tags.push({
      name: DROI_AUTOMATION_TAG,
      metadata: {
        automationId: `run:${sessionId}`,
        automationName: (item.title ?? '').replace(AUTOMATION_PREFIX, ''),
      },
    })
  }
  const archivedAt = item.archived ? new Date(item.lastActivityAt).toISOString() : null
  return {
    sessionId: sessionIdOf(item.id),
    title: item.title?.trim() || 'Untitled session',
    cwd: item.workspace ?? null,
    repoRoot: item.repoRoot ?? null,
    worktree: null,
    updatedAt: Math.floor(item.lastActivityAt / 1000),
    activityAt: item.lastActivityAt,
    viewedAt: item.viewedAt ?? null,
    kind: item.kind ?? null,
    messagesCount: null,
    archivedAt,
    tags,
    parentId: null,
  }
}

/** A row with what an index.changed says of it. */
export function withIndexChange(
  s: SessionSummary,
  change: Record<string, unknown>,
): SessionSummary {
  const next = { ...s }
  if (typeof change['title'] === 'string') next.title = change['title'].trim() || 'Untitled session'
  if (typeof change['lastActivityAt'] === 'number') {
    next.activityAt = change['lastActivityAt']
    next.updatedAt = Math.floor(change['lastActivityAt'] / 1000)
  }
  if ('viewedAt' in change)
    next.viewedAt = typeof change['viewedAt'] === 'number' ? change['viewedAt'] : null
  if (typeof change['archived'] === 'boolean') {
    next.archivedAt = change['archived'] ? (s.archivedAt ?? new Date().toISOString()) : null
  }
  return next
}

export interface Pins {
  workspaces: ReadonlySet<string>
  sessions: ReadonlySet<string>
}

export const NO_PINS: Pins = { workspaces: new Set(), sessions: new Set() }

/** How the Workspace groups are ordered. */
export type WorkspaceSort = 'sessions' | 'recent' | 'name' | 'manual'
/** How the Sessions inside a group (Recents too) are ordered. */
export type SessionSort = 'recent' | 'created'

export const WORKSPACE_SORT_LABELS: Record<WorkspaceSort, string> = {
  sessions: 'Most sessions',
  recent: 'Recently active',
  name: 'Name',
  manual: 'Manual',
}
export const SESSION_SORT_LABELS: Record<SessionSort, string> = {
  recent: 'Recently active',
  created: 'Created',
}

export interface SortOrder {
  workspaces: WorkspaceSort
  sessions: SessionSort
  /** Workspace keys in the order the user dragged them into; for `manual`. */
  manual: readonly string[]
  /**
   * When each Session was first seen, in ms; for `created`. The Daemon lists no
   * creation time, so the Client keeps its own (see noteFirstSeen).
   */
  firstSeen: Readonly<Record<string, number>>
  /** The Session Times (session-times.ts), which date a Session by its last message sent. */
  times?: ReadonlyMap<string, SessionTimes>
  /** The Sessions whose turn runs, waiting for an answer included. */
  busy?: ReadonlySet<string>
  /** The Sessions with a result nobody has seen. */
  unread?: ReadonlySet<string>
}

/**
 * When the Session was last used, in ms: when the user last sent it a message, else the
 * Daemon's date for it. The Daemon dates a Session by its file, which every hook run and
 * every step of a turn writes, so only a Session sent nothing through a Gateway since Droi
 * kept the Session Times (one used in the droid CLI) is dated that way.
 */
export function usedAt(
  session: Pick<SessionSummary, 'sessionId' | 'updatedAt'>,
  times?: ReadonlyMap<string, SessionTimes>,
): number {
  return times?.get(session.sessionId)?.sentAt || session.updatedAt * 1000
}

/** Ranks float a Session above the time order: one whose turn runs, then one unread. */
export const RANK_REST = 0
export const RANK_UNREAD = 1
export const RANK_BUSY = 2

/** What places a Session in the list: its rank, then when it was last used, in ms. */
export interface SortKey {
  rank: number
  at: number
}

/** The Session's key; the Created order has no ranks, a plain list by creation. */
export function sortKey(session: SessionSummary, order: SortOrder): SortKey {
  const at = usedAt(session, order.times)
  if (order.sessions === 'created') return { rank: RANK_REST, at }
  const rank = order.busy?.has(session.sessionId)
    ? RANK_BUSY
    : order.unread?.has(session.sessionId)
      ? RANK_UNREAD
      : RANK_REST
  return { rank, at }
}

const created = (order: SortOrder, session: SessionSummary) =>
  order.firstSeen[session.sessionId] ?? session.updatedAt * 1000

/** Two Sessions of a list: pinned ones first, then by the order chosen, newest first. */
function compareSessions(pins: Pins, order: SortOrder, a: SessionSummary, b: SessionSummary) {
  const pinned = Number(pins.sessions.has(b.sessionId)) - Number(pins.sessions.has(a.sessionId))
  if (pinned !== 0) return pinned
  if (order.sessions === 'created') {
    const byCreation = created(order, b) - created(order, a)
    if (byCreation !== 0) return byCreation
  }
  const ka = sortKey(a, order)
  const kb = sortKey(b, order)
  return kb.rank - ka.rank || kb.at - ka.at
}

/** How the list shows the Sessions: grouped by Workspace, or one list by time. */
export type SessionView = 'workspaces' | 'time'

/** The time view's sections. */
export const NOW_GROUP_KEY = 'droi:time:now'
export const TODAY_GROUP_KEY = 'droi:time:today'
export const YESTERDAY_GROUP_KEY = 'droi:time:yesterday'
export const EARLIER_GROUP_KEY = 'droi:time:earlier'

export interface TimeGroup {
  key: string
  label: string
  sessions: SessionSummary[]
  /** Each Session's Workspace label; none for a Scratch Session. */
  places: ReadonlyMap<string, string>
}

/**
 * The time view, one list of every Session: the pinned ones, then those working or with an
 * unseen result (Right now), then the rest by the local day they were last used (created, in
 * the Created order), today, yesterday and earlier. Each section is in the list's order.
 */
export function groupByTime(
  sessions: readonly SessionSummary[],
  pins: Pins = NO_PINS,
  order: SortOrder = DEFAULT_SORT,
  now: Date = new Date(),
): TimeGroup[] {
  const places = new Map<string, string>()
  for (const session of sessions) {
    if (!isScratchSession(session)) {
      places.set(session.sessionId, workspaceLabel(workspaceOf(session) ?? ''))
    }
  }
  const sorted = [...sessions].sort((a, b) => compareSessions(NO_PINS, order, a, b))
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1).getTime()
  const sections: TimeGroup[] = [
    { key: PINNED_SESSIONS_GROUP_KEY, label: 'Pinned', sessions: [], places },
    { key: NOW_GROUP_KEY, label: 'Right now', sessions: [], places },
    { key: TODAY_GROUP_KEY, label: 'Today', sessions: [], places },
    { key: YESTERDAY_GROUP_KEY, label: 'Yesterday', sessions: [], places },
    { key: EARLIER_GROUP_KEY, label: 'Earlier', sessions: [], places },
  ]
  for (const session of sorted) {
    const key = sortKey(session, order)
    const at = order.sessions === 'created' ? created(order, session) : key.at
    const section = pins.sessions.has(session.sessionId)
      ? sections[0]
      : key.rank > RANK_REST
        ? sections[1]
        : at >= today
          ? sections[2]
          : at >= yesterday
            ? sections[3]
            : sections[4]
    section!.sessions.push(session)
  }
  return sections.filter((section) => section.sessions.length > 0)
}

export const DEFAULT_SORT: SortOrder = {
  workspaces: 'sessions',
  sessions: 'recent',
  manual: [],
  firstSeen: {},
}

/**
 * The first-seen times with any Session not seen before added. A Session is
 * never made after its last change, so its time is the earlier of now and
 * that change: a new one lands at about its creation, and an old one the
 * Client meets for the first time keeps its place among the others. Returns
 * the same object when nothing was new.
 */
export function noteFirstSeen(
  firstSeen: Readonly<Record<string, number>>,
  sessions: readonly SessionSummary[],
  now = Date.now(),
): Readonly<Record<string, number>> {
  let next: Record<string, number> | null = null
  for (const session of sessions) {
    if (firstSeen[session.sessionId] !== undefined) continue
    next ??= { ...firstSeen }
    next[session.sessionId] = Math.min(now, session.updatedAt * 1000)
  }
  return next ?? firstSeen
}

/**
 * The Workspace groups in the chosen order (by default the one with the most
 * conversations first, ties broken by the newest), and inside each the
 * Sessions busy first, then unread, then newest first (sortKey), or by when
 * they were first seen. Pinned Workspaces and Sessions come before the rest,
 * in the same order among themselves. With `pinnedApart` (the computer's
 * sidebar) pinned Sessions instead leave their groups for one of their own,
 * first. Scratch Sessions share one Recents group, always last.
 */
export function groupByWorkspace(
  sessions: readonly SessionSummary[],
  pins: Pins = NO_PINS,
  order: SortOrder = DEFAULT_SORT,
  { pinnedApart = false }: { pinnedApart?: boolean } = {},
): WorkspaceGroup[] {
  const groups = new Map<string, WorkspaceGroup>()
  let recents: WorkspaceGroup | null = null
  let pinned: WorkspaceGroup | null = null
  for (const session of sessions) {
    if (pinnedApart && pins.sessions.has(session.sessionId)) {
      pinned ??= {
        key: PINNED_SESSIONS_GROUP_KEY,
        label: 'Pinned sessions',
        path: '',
        scratch: false,
        sessions: [],
      }
      pinned.sessions.push(session)
      continue
    }
    if (isScratchSession(session)) {
      recents ??= {
        key: RECENTS_GROUP_KEY,
        label: 'Recents',
        path: '',
        scratch: true,
        sessions: [],
      }
      recents.sessions.push(session)
      continue
    }
    const path = workspaceOf(session) ?? ''
    const key = path || '(unknown)'
    let group = groups.get(key)
    if (!group) {
      group = { key, label: workspaceLabel(path), path, scratch: false, sessions: [] }
      groups.set(key, group)
    }
    group.sessions.push(session)
  }
  const result = [...groups.values()]
  for (const group of [...result, ...(recents ? [recents] : []), ...(pinned ? [pinned] : [])]) {
    group.sessions.sort((a, b) => compareSessions(pins, order, a, b))
  }
  const newest = (group: WorkspaceGroup) =>
    Math.max(0, ...group.sessions.map((session) => sortKey(session, order).at))
  const conversations = (group: WorkspaceGroup) =>
    group.sessions.filter((session) => (session.messagesCount ?? 1) > 0).length
  const byDefault = (a: WorkspaceGroup, b: WorkspaceGroup) =>
    conversations(b) - conversations(a) || newest(b) - newest(a)
  // A Workspace not placed by hand yet goes after the placed ones, in the default order.
  const place = (group: WorkspaceGroup) => {
    const index = order.manual.indexOf(group.key)
    return index === -1 ? order.manual.length : index
  }
  const chosen: Record<WorkspaceSort, (a: WorkspaceGroup, b: WorkspaceGroup) => number> = {
    sessions: byDefault,
    recent: (a, b) => newest(b) - newest(a) || byDefault(a, b),
    name: (a, b) => a.label.localeCompare(b.label) || byDefault(a, b),
    manual: (a, b) => place(a) - place(b) || byDefault(a, b),
  }
  result.sort(
    (a, b) =>
      Number(pins.workspaces.has(b.key)) - Number(pins.workspaces.has(a.key)) ||
      chosen[order.workspaces](a, b),
  )
  return [...(pinned ? [pinned] : []), ...result, ...(recents ? [recents] : [])]
}

/**
 * The manual order after moving one Workspace next to another, as shown now
 * (`shown` is the current order of every group's key). Starts from what is
 * shown, so the first drag keeps everything else where it was.
 */
export function moveWorkspace(
  shown: readonly string[],
  key: string,
  target: string,
  place: 'before' | 'after',
): string[] {
  if (key === target) return [...shown]
  const rest = shown.filter((k) => k !== key)
  const at = rest.indexOf(target)
  if (at === -1) return [...shown]
  rest.splice(place === 'before' ? at : at + 1, 0, key)
  return rest
}

/** A Workspace none of whose Sessions changed in this long waits behind "Show N older workspaces". */
export const STALE_WORKSPACE_MS = 30 * 24 * 60 * 60 * 1000

/**
 * Parts groups, in order, into the Workspaces used within STALE_WORKSPACE_MS or holding a busy
 * or unread Session, and the rest.
 */
export function splitStale(
  groups: readonly WorkspaceGroup[],
  now = Date.now(),
  order: SortOrder = DEFAULT_SORT,
): { active: WorkspaceGroup[]; stale: WorkspaceGroup[] } {
  const cutoff = now - STALE_WORKSPACE_MS
  const active: WorkspaceGroup[] = []
  const stale: WorkspaceGroup[] = []
  for (const group of groups) {
    const used = group.sessions.some((session) => {
      const key = sortKey(session, order)
      return key.at >= cutoff || key.rank > RANK_REST
    })
    ;(used ? active : stale).push(group)
  }
  return { active, stale }
}

/** The tag the Daemon gives the Sessions an Automation starts; a run's says type "run". */
export const AUTOMATION_TAG = 'automation'

/** The tag of the Sessions Droi's own Automations start, one per run. */
export const DROI_AUTOMATION_TAG = 'droi.automation'

/** Leaves out Automation runs, the Daemon's and Droi's, which the computer's Automations page lists. */
export function withoutAutomationRuns(sessions: readonly SessionSummary[]): SessionSummary[] {
  return sessions.filter(
    (s) =>
      !s.tags.some(
        (t) =>
          t.name === DROI_AUTOMATION_TAG ||
          (t.name === AUTOMATION_TAG && t.metadata?.['type'] === 'run'),
      ),
  )
}

/** Sessions used within this window always show, as do the busy and unread; older ones sit behind "Show more". */
export const RECENT_WINDOW_MS = 3 * 24 * 60 * 60 * 1000
export const OLDER_BATCH = 30

/**
 * The rows a Workspace section shows: every pinned or recent Session plus the
 * first `revealed` older ones (the list is newest first), and how many stay hidden.
 */
export function visibleSessions(
  sessions: readonly SessionSummary[],
  revealed: number,
  now = Date.now(),
  pinned: ReadonlySet<string> = NO_PINS.sessions,
  order: SortOrder = DEFAULT_SORT,
): { visible: SessionSummary[]; hidden: number } {
  const cutoff = now - RECENT_WINDOW_MS
  const visible: SessionSummary[] = []
  let older = 0
  for (const session of sessions) {
    if (pinned.has(session.sessionId)) {
      visible.push(session)
      continue
    }
    const key = sortKey(session, order)
    const recent = key.at >= cutoff || key.rank > RANK_REST
    if (recent || older < revealed) visible.push(session)
    if (!recent) older += 1
  }
  return { visible, hidden: Math.max(0, older - revealed) }
}

/** The Workspace a Session belongs to: its main checkout, never a worktree's own folder. */
export function workspaceOf(
  session: Pick<SessionSummary, 'cwd' | 'repoRoot' | 'worktree'>,
): string | null {
  return session.worktree?.repoRoot ?? session.repoRoot ?? session.cwd
}

export function workspaceLabel(path: string): string {
  if (!path) return 'Unknown workspace'
  const trimmed = path.replace(/[\\/]+$/, '')
  const last = trimmed.split(/[\\/]/).pop()
  return last || trimmed
}
