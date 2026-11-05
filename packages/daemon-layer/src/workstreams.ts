// Workstreams (ADR 0036): an orchestrator Session the Host leads a long goal
// with, and the agents it starts, each a Session of its own. The Host tags
// every one of them; Clients read the tag to keep agents out of the Session
// list and show them under their orchestrator instead.
import type { SessionSummary, SessionTag } from './sessions'
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

/** Leaves out Workstream agents, which their orchestrator lists. */
export function withoutWorkstreamAgents(sessions: readonly SessionSummary[]): SessionSummary[] {
  return sessions.filter((s) => !isWorkstreamAgent(s))
}

/** The agents of the Workstream an orchestrator leads, archived ones left out, newest first. */
export function agentsOf(
  sessions: readonly SessionSummary[],
  orchestrator: Pick<SessionSummary, 'tags'>,
): SessionSummary[] {
  const led = workstreamOf(orchestrator.tags)
  if (led?.role !== 'orchestrator') return []
  return sessions
    .filter((s) => {
      const member = workstreamOf(s.tags)
      return member?.role === 'agent' && member.workstreamId === led.workstreamId && !s.archivedAt
    })
    .sort((a, b) => b.updatedAt - a.updatedAt)
}

/**
 * The listed orchestrator of an agent's Workstream: a compaction's child
 * carries its parent's tags, so the latest link of the chain is the one that
 * no other listed orchestrator continues. Null for a Session that is no
 * agent, or whose orchestrator the list lacks.
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
  const continued = new Set(leads.flatMap((s) => (s.parentId ? [s.parentId] : [])))
  return leads.find((s) => !continued.has(s.sessionId)) ?? null
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

export function agentCount(count: number): string {
  return count === 1 ? '1 agent' : `${count} agents`
}

/** An orchestrator's row in the Session list: `Workstream · 3 agents · 1 waiting`. */
export function workstreamRowLabel(agents: number, waiting: number): string {
  const parts = ['Workstream', agentCount(agents)]
  if (waiting > 0) parts.push(`${waiting} waiting`)
  return parts.join(' · ')
}
