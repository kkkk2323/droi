// Subagents: Sessions the Daemon starts for a Task tool call. The list carries
// each one's calling Session and tool call; Clients keep them out of the
// Session list and reach them from the Session that called them.
import { useQueryClient } from '@tanstack/react-query'
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useSyncExternalStore,
} from 'react'
import { useDaemonConnection } from './connection-context'
import { SESSION_EVENT, type SubagentStatus } from './sdk-enums'
import { SESSIONS_QUERY_KEY, type SessionSummary } from './sessions'
import { toolResultText, type ToolCall, type TranscriptEntry } from './transcript'

export interface SessionRef {
  sessionId: string
  title: string
}

/** The Sessions a list shows: subagents hang off the Session that called them. */
export function mainSessions(sessions: readonly SessionSummary[]): SessionSummary[] {
  return sessions.filter((s) => !s.callingSessionId)
}

/** Subagents called from any of these Sessions, newest first. */
export function subagentsOf(
  sessions: readonly SessionSummary[],
  callerIds: readonly string[],
): SessionSummary[] {
  const callers = new Set(callerIds)
  return sessions
    .filter((s) => s.callingSessionId !== null && callers.has(s.callingSessionId))
    .sort((a, b) => b.updatedAt - a.updatedAt)
}

/** Every link of the compaction chain a Session belongs to, latest first. */
function compactionChain(sessions: readonly SessionSummary[], sessionId: string): string[] {
  const continuedBy = new Map<string, string>()
  const parentOf = new Map<string, string>()
  for (const s of sessions) {
    if (!s.parentId) continue
    continuedBy.set(s.parentId, s.sessionId)
    parentOf.set(s.sessionId, s.parentId)
  }
  let latest = sessionId
  const seen = new Set([latest])
  while (continuedBy.has(latest) && !seen.has(continuedBy.get(latest)!)) {
    latest = continuedBy.get(latest)!
    seen.add(latest)
  }
  const chain = [latest]
  for (let id = parentOf.get(latest); id && !chain.includes(id); id = parentOf.get(id)) {
    chain.push(id)
  }
  return chain
}

/**
 * The Sessions above a subagent, its main Session first; empty for a main
 * Session. A caller that has since been compacted leads to the Session that
 * continues it. A caller missing from the list still gets a crumb.
 */
export function callerTrail(
  sessions: readonly SessionSummary[],
  session: Pick<SessionSummary, 'sessionId' | 'callingSessionId'>,
): SessionRef[] {
  const byId = new Map(sessions.map((s) => [s.sessionId, s]))
  const trail: SessionRef[] = []
  const seen = new Set([session.sessionId])
  let callerId = session.callingSessionId
  while (callerId && !seen.has(callerId)) {
    seen.add(callerId)
    const caller = byId.get(callerId)
    const latestId = compactionChain(sessions, callerId)[0] ?? callerId
    const latest = byId.get(latestId) ?? caller
    trail.unshift({ sessionId: latestId, title: latest?.title ?? 'Main session' })
    callerId = caller?.callingSessionId ?? null
  }
  return trail
}

/**
 * The subagents a subagent can switch to: everything its caller called, across
 * the caller's compactions, as the caller's header lists them.
 */
export function subagentSiblings(
  sessions: readonly SessionSummary[],
  session: Pick<SessionSummary, 'callingSessionId'>,
): SessionSummary[] {
  if (!session.callingSessionId) return []
  return subagentsOf(sessions, compactionChain(sessions, session.callingSessionId))
}

/**
 * The listed row a Session belongs to: its main Session, at the latest link
 * of that Session's compaction chain (see foldContinued).
 */
export function listedSessionOf(sessions: readonly SessionSummary[], sessionId: string): string {
  const byId = new Map(sessions.map((s) => [s.sessionId, s]))
  const seen = new Set<string>()
  let id = sessionId
  for (let s = byId.get(id); s?.callingSessionId && !seen.has(id); s = byId.get(id)) {
    seen.add(id)
    id = s.callingSessionId
  }
  const continuedBy = new Map<string, string>()
  for (const s of sessions) if (s.parentId) continuedBy.set(s.parentId, s.sessionId)
  while (continuedBy.has(id) && !seen.has(id)) {
    seen.add(id)
    id = continuedBy.get(id)!
  }
  return id
}

export interface SubagentRun {
  status: SubagentStatus
  toolUseCount: number | null
  durationMs: number | null
}

export function isRunning(status: SubagentStatus | null | undefined): boolean {
  return status === 'running' || status === 'pending'
}

/** How many subagents are running under each listed row. */
export function runningSubagents(
  sessions: readonly SessionSummary[],
  runs: ReadonlyMap<string, SubagentRun>,
): ReadonlyMap<string, number> {
  const counts = new Map<string, number>()
  for (const s of sessions) {
    if (!s.callingSessionId || !isRunning(runs.get(s.sessionId)?.status)) continue
    const row = listedSessionOf(sessions, s.sessionId)
    counts.set(row, (counts.get(row) ?? 0) + 1)
  }
  return counts
}

export interface TaskRequest {
  /** The custom droid's name as the Daemon knows it, such as `explorer`. */
  subagentType: string
  description: string
  prompt: string
}

export function taskRequest(call: ToolCall): TaskRequest {
  const input = (call.use.input ?? {}) as Record<string, unknown>
  const text = (key: string) => (typeof input[key] === 'string' ? input[key] : '')
  return {
    subagentType: text('subagent_type'),
    description: text('description'),
    prompt: text('prompt'),
  }
}

/** `explorer` → `Explorer`, the way the Daemon titles the subagent's Session. */
export function subagentName(subagentType: string): string {
  return subagentType ? subagentType.charAt(0).toUpperCase() + subagentType.slice(1) : 'Subagent'
}

const BACKGROUND_LAUNCH = 'Task launched in background'

/** A background Task answers at once with the subagent's Session id. */
export function launchedSessionId(call: ToolCall): string | null {
  return /^session_id:\s*(\S+)\s*$/m.exec(toolResultText(call.result))?.[1] ?? null
}

/**
 * What the Task call is doing: the Daemon's own account when this Client has
 * one, otherwise what the tool result says. `launched` is a background run
 * nobody here has heard back from.
 */
export type TaskState = SubagentStatus | 'launched'

export function taskState(call: ToolCall, run: SubagentRun | null): TaskState {
  if (call.result?.isError) return 'failed'
  if (run) return run.status
  if (!call.result) return 'running'
  return toolResultText(call.result).startsWith(BACKGROUND_LAUNCH) ? 'launched' : 'completed'
}

/** The subagent's report, once a waited-for Task has one; a launch notice is not a report. */
export function taskReport(call: ToolCall): string {
  const text = toolResultText(call.result)
  return text.startsWith(BACKGROUND_LAUNCH) ? '' : text
}

const NO_RUNS: ReadonlyMap<string, SubagentRun> = new Map()

/**
 * The Daemon's account of each subagent's run, for the ones this Client has
 * heard about: loading the calling Session reports its subagents, and a live
 * one reports as it starts and finishes.
 */
export function useSubagentRuns(sessionIds: readonly string[]): ReadonlyMap<string, SubagentRun> {
  const { sessionState } = useDaemonConnection()
  const key = sessionIds.join(',')
  const version = useRef(0)
  const snapshot = useRef({ version: -1, key: '', value: NO_RUNS })

  const subscribe = useCallback(
    (listener: () => void) =>
      sessionState.subscribeToSessionEvents(
        [
          SESSION_EVENT.subagentInvocationSummaryUpdated,
          SESSION_EVENT.workingStateChanged,
          SESSION_EVENT.loadStateChanged,
        ],
        () => {
          version.current += 1
          listener()
        },
      ),
    [sessionState],
  )

  const getSnapshot = useCallback(() => {
    const current = snapshot.current
    if (current.version === version.current && current.key === key) return current.value
    const runs = new Map<string, SubagentRun>()
    for (const id of key ? key.split(',') : []) {
      const summary = sessionState.getSubagentInvocationSummary(id)
      const working = sessionState.getSessionManager(id)?.getDroidWorkingState()
      const status: SubagentStatus | null =
        summary?.status ?? (working && working !== 'idle' ? 'running' : null)
      if (!status) continue
      runs.set(id, {
        status,
        toolUseCount: summary?.toolUseCount ?? null,
        durationMs: summary?.durationMs ?? null,
      })
    }
    snapshot.current = {
      version: version.current,
      key,
      value: runs.size === 0 ? NO_RUNS : runs,
    }
    return snapshot.current.value
  }, [sessionState, key])

  return useSyncExternalStore(subscribe, getSnapshot, () => NO_RUNS)
}

/** What a transcript needs to link a Task call to its subagent. */
export interface SubagentLinks {
  /** Listed subagents by the Task call that started them. */
  byToolUse: ReadonlyMap<string, SessionSummary>
  runs: ReadonlyMap<string, SubagentRun>
  open: (sessionId: string) => void
}

export function subagentsByToolUse(
  sessions: readonly SessionSummary[],
): ReadonlyMap<string, SessionSummary> {
  const map = new Map<string, SessionSummary>()
  for (const s of sessions) if (s.callingToolUseId) map.set(s.callingToolUseId, s)
  return map
}

const SubagentLinksContext = createContext<SubagentLinks | null>(null)

export const SubagentLinksProvider = SubagentLinksContext.Provider

export function useSubagentLinks(): SubagentLinks | null {
  return useContext(SubagentLinksContext)
}

/**
 * The Task calls whose subagent the list does not show, each with whether it
 * has answered yet; empty when every one is listed.
 */
export function unlistedTaskCalls(
  transcript: readonly TranscriptEntry[],
  byToolUse: ReadonlyMap<string, SessionSummary>,
): string {
  const missing: string[] = []
  for (const entry of transcript) {
    for (const block of entry.blocks) {
      if (block.kind === 'subagent' && !byToolUse.has(block.call.use.id)) {
        missing.push(`${block.call.use.id}${block.call.result ? ':answered' : ''}`)
      }
    }
  }
  return missing.join(',')
}

/**
 * Reads the Session list again when the transcript shows a Task call whose
 * subagent the list lacks. A Session another process drives (the CLI, say)
 * starts subagents without any event reaching this Client.
 */
export function useListNewSubagents(transcript: readonly TranscriptEntry[]): void {
  const links = useContext(SubagentLinksContext)
  const queryClient = useQueryClient()
  const missing = links ? unlistedTaskCalls(transcript, links.byToolUse) : ''
  useEffect(() => {
    if (missing) void queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
  }, [missing, queryClient])
}

export interface SubagentLink {
  request: TaskRequest
  state: TaskState
  run: SubagentRun | null
  report: string
  /** Absent until the subagent's Session is known. */
  open: (() => void) | null
}

export function useSubagentLink(call: ToolCall): SubagentLink {
  const links = useContext(SubagentLinksContext)
  const sessionId = links?.byToolUse.get(call.use.id)?.sessionId ?? launchedSessionId(call)
  const run = (sessionId && links?.runs.get(sessionId)) || null
  return {
    request: taskRequest(call),
    state: taskState(call, run),
    run,
    report: taskReport(call),
    open: sessionId && links ? () => links.open(sessionId) : null,
  }
}

export function formatRunDuration(ms: number): string {
  const seconds = Math.round(ms / 1_000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}
