// Session list and Workspace grouping. The list is a collection fetched from
// the Daemon on demand, so it lives in TanStack Query; the open Session's
// transcript lives in the SDK state manager (see use-session.ts).
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import type { DaemonSessionController } from '@factory/droid-sdk'
import { useConnectionState, useDaemonConnection } from './connection-context'
import { SESSION_EVENT } from './sdk-enums'
import { isMemorySession } from './memory-session'

export interface SessionSummary {
  sessionId: string
  title: string
  cwd: string | null
  /** Grouping key the Daemon provides for checkouts; falls back to cwd. */
  repoRoot: string | null
  /** Unix epoch seconds. */
  updatedAt: number
  messagesCount: number | null
  archivedAt: string | null
  tags: SessionTag[]
  /** The Session this one continues after a compaction (see use-compact.ts). */
  parentId: string | null
  /** For a subagent: the Session whose Task tool call started it (see subagents.ts). */
  callingSessionId: string | null
  /** For a subagent: that Task tool call's id in the calling Session. */
  callingToolUseId: string | null
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
 * Tag of a Draft Session (see use-draft-session.ts). It lives in the session
 * file, so every Client and every later launch leaves the draft unlisted.
 */
export const DRAFT_TAG = 'droi.draft'

export function isDraft(tags: readonly SessionTag[] | undefined): boolean {
  return tags?.some((t) => t.name === DRAFT_TAG) ?? false
}

/**
 * Tag of a Session in a Scratch Workspace (ADR 0008). A compaction's child
 * inherits it with the rest of the parent's tags.
 */
export const SCRATCH_TAG = 'droi.scratch'

export function isScratch(tags: readonly SessionTag[] | undefined): boolean {
  return tags?.some((t) => t.name === SCRATCH_TAG) ?? false
}

/** Group key of Recents, where every Scratch Session is listed. */
export const RECENTS_GROUP_KEY = 'droi:recents'

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

/** Sessions per page, the Daemon's maximum; the Factory App pages by 50. */
export const SESSION_PAGE = 100

export interface SessionList {
  /** Every Session fetched so far, newest first; undefined before the first page. */
  data: SessionSummary[] | undefined
  isPending: boolean
  error: Error | null
  /** The Daemon has Sessions older than the last page. */
  hasMore: boolean
  isLoadingMore: boolean
  loadMore(): void
}

/**
 * The Daemon lists Sessions newest first, a page at a time, with an
 * `endBefore` cursor (the last row's `updatedAt`). The first page comes with
 * the connection; the rest on request, since a computer can hold thousands.
 */
export function useSessionList(options: { includeArchived?: boolean } = {}): SessionList {
  const includeArchived = options.includeArchived ?? false
  const connection = useDaemonConnection()
  // Read through the hook, not connection.getState(): the compiler memoizes
  // the options on `connection`, which never changes identity.
  const connected = useConnectionState().status === 'connected'
  const queryClient = useQueryClient()

  useEffect(() => {
    const invalidate = () => void queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
    connection.controller.on('connected', invalidate)
    connection.controller.on('sessionTitleUpdated', invalidate)
    connection.controller.on('sessionArchiveStateChanged', invalidate)
    // A subagent starting or finishing: its Session is new to the list, or has news.
    const unsubscribe = connection.sessionState.subscribeToSessionEvents(
      [SESSION_EVENT.subagentInvocationSummaryUpdated],
      invalidate,
    )
    return () => {
      connection.controller.off('connected', invalidate)
      connection.controller.off('sessionTitleUpdated', invalidate)
      connection.controller.off('sessionArchiveStateChanged', invalidate)
      unsubscribe()
    }
  }, [connection, queryClient])

  const query = useInfiniteQuery({
    queryKey: [...SESSIONS_QUERY_KEY, { includeArchived }],
    initialPageParam: null as number | null,
    queryFn: async ({ pageParam }) => {
      const result = await connection.controller.listAvailableSessions({
        limit: SESSION_PAGE,
        includeArchived,
        ...(pageParam !== null ? { endBefore: pageParam } : {}),
      })
      return {
        sessions: result.sessions
          .filter((s) => !isDraft(s.tags) && !isMemorySession(s.tags))
          .map(summaryOf),
        nextCursor: result.hasMore ? (result.nextCursor ?? null) : null,
      }
    },
    getNextPageParam: (last) => last.nextCursor ?? undefined,
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

type ListedSession = Awaited<
  ReturnType<DaemonSessionController['listAvailableSessions']>
>['sessions'][number]

function summaryOf(s: ListedSession): SessionSummary {
  return {
    sessionId: s.sessionId,
    title: s.title?.trim() || 'Untitled session',
    cwd: s.cwd ?? null,
    repoRoot: s.repoRoot ?? null,
    updatedAt: s.updatedAt,
    messagesCount: s.messagesCount ?? null,
    archivedAt: s.archivedAt ?? null,
    tags: s.tags ?? [],
    parentId: continuationParent(s.tags),
    callingSessionId: s.callingSessionId ?? null,
    callingToolUseId: s.callingToolUseId ?? null,
  }
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
 * Sessions newest first or by when they were first seen. Pinned Workspaces and
 * Sessions come before the rest, in the same order among themselves.
 *
 * The default does not rank Workspaces by recency alone because the Daemon's
 * `updatedAt` is the file's modified time, which moves when a Session is
 * merely loaded. Scratch Sessions share one Recents group, always last.
 */
export function groupByWorkspace(
  sessions: readonly SessionSummary[],
  pins: Pins = NO_PINS,
  order: SortOrder = DEFAULT_SORT,
): WorkspaceGroup[] {
  const groups = new Map<string, WorkspaceGroup>()
  let recents: WorkspaceGroup | null = null
  for (const session of sessions) {
    if (isScratch(session.tags)) {
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
    const path = session.repoRoot ?? session.cwd ?? ''
    const key = path || '(unknown)'
    let group = groups.get(key)
    if (!group) {
      group = { key, label: workspaceLabel(path), path, scratch: false, sessions: [] }
      groups.set(key, group)
    }
    group.sessions.push(session)
  }
  const result = [...groups.values()]
  const created = (session: SessionSummary) =>
    order.firstSeen[session.sessionId] ?? session.updatedAt * 1000
  for (const group of recents ? [...result, recents] : result) {
    group.sessions.sort(
      (a, b) =>
        Number(pins.sessions.has(b.sessionId)) - Number(pins.sessions.has(a.sessionId)) ||
        (order.sessions === 'created' ? created(b) - created(a) : 0) ||
        b.updatedAt - a.updatedAt,
    )
  }
  const newest = (group: WorkspaceGroup) =>
    Math.max(0, ...group.sessions.map((session) => session.updatedAt))
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
  return recents ? [...result, recents] : result
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

/** Sessions touched within this window always show; older ones sit behind "Show more". */
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
): { visible: SessionSummary[]; hidden: number } {
  const cutoff = now - RECENT_WINDOW_MS
  const visible: SessionSummary[] = []
  let older = 0
  for (const session of sessions) {
    if (pinned.has(session.sessionId)) {
      visible.push(session)
      continue
    }
    const recent = session.updatedAt * 1000 >= cutoff
    if (recent || older < revealed) visible.push(session)
    if (!recent) older += 1
  }
  return { visible, hidden: Math.max(0, older - revealed) }
}

export function workspaceLabel(path: string): string {
  if (!path) return 'Unknown workspace'
  const trimmed = path.replace(/[\\/]+$/, '')
  const last = trimmed.split(/[\\/]/).pop()
  return last || trimmed
}
