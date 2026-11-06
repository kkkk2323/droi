// Workstreams (ADR 0036): an orchestrator Session the Host leads a long goal
// with, and the agents it starts, each a Session of its own. The Host tags
// every one of them; Clients read the tag to keep agents out of the Session
// list and show them under their orchestrator instead.
import { foldContinued, type SessionSummary, type SessionTag } from './sessions'
import type { SessionActivity } from './use-session-activity'

export const WORKSTREAM_TAG = 'droi.workstream'

export type WorkstreamRole = 'orchestrator' | 'agent'

export interface WorkstreamMembership {
  workstreamId: string
  role: WorkstreamRole
  /** For an agent: the orchestrator that started it; null for the orchestrator. */
  orchestratorId: string | null
}

/** What a Session's `droi.workstream` tag says; null when it has none, or one without an id or role. */
export function workstreamOf(tags: readonly SessionTag[] | undefined): WorkstreamMembership | null {
  const metadata = tags?.find((t) => t.name === WORKSTREAM_TAG)?.metadata
  const workstreamId = metadata?.['workstreamId']
  const role = metadata?.['role']
  if (!workstreamId || (role !== 'orchestrator' && role !== 'agent')) return null
  return {
    workstreamId,
    role,
    orchestratorId: role === 'agent' ? metadata['orchestratorId'] || null : null,
  }
}

export function isWorkstreamAgent(session: Pick<SessionSummary, 'tags'>): boolean {
  return workstreamOf(session.tags)?.role === 'agent'
}

/**
 * The Workstreams whose orchestrator the list shows: an archived one counts
 * only while the list shows archived Sessions.
 */
export function ledWorkstreams(
  sessions: readonly SessionSummary[],
  { includeArchived }: { includeArchived: boolean },
): ReadonlySet<string> {
  const led = new Set<string>()
  for (const s of sessions) {
    const member = workstreamOf(s.tags)
    if (member?.role === 'orchestrator' && (includeArchived || !s.archivedAt)) {
      led.add(member.workstreamId)
    }
  }
  return led
}

/**
 * Leaves out the agents of the Workstreams in `led`, which their orchestrator
 * lists; an agent whose orchestrator the list lacks stays, so it can be reached.
 */
export function withoutWorkstreamAgents(
  sessions: readonly SessionSummary[],
  led: ReadonlySet<string>,
): SessionSummary[] {
  return sessions.filter((s) => {
    const member = workstreamOf(s.tags)
    return member?.role !== 'agent' || !led.has(member.workstreamId)
  })
}

/**
 * Every Workstream's agents, archived ones left out, newest first. A
 * compaction's child copies its parent's tags while the parent stays listed,
 * so each agent counts once, as the latest link of its chain.
 */
export function agentsByWorkstream(
  sessions: readonly SessionSummary[],
): ReadonlyMap<string, SessionSummary[]> {
  const all = new Map<string, SessionSummary[]>()
  for (const s of sessions) {
    const member = workstreamOf(s.tags)
    if (member?.role !== 'agent') continue
    let agents = all.get(member.workstreamId)
    if (!agents) all.set(member.workstreamId, (agents = []))
    agents.push(s)
  }
  const byWorkstream = new Map<string, SessionSummary[]>()
  for (const [workstreamId, agents] of all) {
    const latest = foldContinued(agents).filter((s) => !s.archivedAt)
    if (latest.length > 0) byWorkstream.set(workstreamId, latest.sort(newestFirst))
  }
  return byWorkstream
}

const newestFirst = (a: SessionSummary, b: SessionSummary) => b.updatedAt - a.updatedAt

/** The agents of the Workstream an orchestrator leads, as agentsByWorkstream gives them. */
export function agentsOf(
  sessions: readonly SessionSummary[],
  orchestrator: Pick<SessionSummary, 'tags'>,
): SessionSummary[] {
  const led = workstreamOf(orchestrator.tags)
  if (led?.role !== 'orchestrator') return []
  return agentsByWorkstream(sessions).get(led.workstreamId) ?? []
}

/**
 * The listed orchestrator of an agent's Workstream: a compaction's child
 * carries its parent's tags, so it is the latest link of the chain. Null for
 * a Session that is no agent, or whose orchestrator the list lacks.
 */
export function orchestratorOf(
  sessions: readonly SessionSummary[],
  agent: Pick<SessionSummary, 'tags'>,
): SessionSummary | null {
  const member = workstreamOf(agent.tags)
  if (member?.role !== 'agent') return null
  const leads = sessions.filter((s) => {
    const m = workstreamOf(s.tags)
    return m?.role === 'orchestrator' && m.workstreamId === member.workstreamId
  })
  return foldContinued(leads)[0] ?? null
}

/** Where an agent stands: stopped until the user answers, working, or idle. */
export type AgentStatus = 'waiting' | 'working' | 'idle'

export function agentStatus(activity: SessionActivity | undefined): AgentStatus {
  if (activity === 'needs-input') return 'waiting'
  return activity ? 'working' : 'idle'
}

export interface AgentGroups {
  /** Waiting on you: a permission or a question. */
  waiting: SessionSummary[]
  working: SessionSummary[]
  idle: SessionSummary[]
}

/** Agents by where they stand, each group in the order given. */
export function groupAgents(
  agents: readonly SessionSummary[],
  activity: ReadonlyMap<string, SessionActivity>,
): AgentGroups {
  const groups: AgentGroups = { waiting: [], working: [], idle: [] }
  for (const agent of agents) groups[agentStatus(activity.get(agent.sessionId))].push(agent)
  return groups
}

/** What an orchestrator's agents are doing, for its row in the list. */
export interface WorkstreamLoad {
  agents: number
  waiting: number
  working: number
}

/** The load of each orchestrator among these Sessions, by its Session id. */
export function workstreamLoads(
  sessions: readonly SessionSummary[],
  byWorkstream: ReadonlyMap<string, readonly SessionSummary[]>,
  activity: ReadonlyMap<string, SessionActivity>,
): ReadonlyMap<string, WorkstreamLoad> {
  const loads = new Map<string, WorkstreamLoad>()
  for (const s of sessions) {
    const led = workstreamOf(s.tags)
    if (led?.role !== 'orchestrator') continue
    const agents = byWorkstream.get(led.workstreamId) ?? []
    const groups = groupAgents(agents, activity)
    loads.set(s.sessionId, {
      agents: agents.length,
      waiting: groups.waiting.length,
      working: groups.working.length,
    })
  }
  return loads
}

/** The busy Sessions plus every orchestrator whose agents work or wait, so it ranks as busy. */
export function withBusyWorkstreams(
  busy: ReadonlySet<string>,
  loads: ReadonlyMap<string, WorkstreamLoad>,
): ReadonlySet<string> {
  const lifted = [...loads].filter(([, load]) => load.waiting + load.working > 0)
  if (lifted.length === 0) return busy
  return new Set([...busy, ...lifted.map(([id]) => id)])
}

export function agentCount(count: number): string {
  return count === 1 ? '1 agent' : `${count} agents`
}

/** An orchestrator's row in the Session list: `Workstream · 3 agents`, then waitingLabel. */
export function workstreamLabel(agents: number): string {
  return `Workstream · ${agentCount(agents)}`
}

export function waitingLabel(waiting: number): string {
  return `${waiting} waiting`
}
