import { describe, expect, test } from 'vitest'
import type { SessionSummary } from './sessions'
import {
  WORKSTREAM_TAG,
  agentCount,
  agentStatus,
  agentsOf,
  groupAgents,
  isWorkstreamAgent,
  orchestratorOf,
  withoutWorkstreamAgents,
  workstreamOf,
  workstreamRowLabel,
} from './workstreams'

function summary(overrides: Partial<SessionSummary>): SessionSummary {
  return {
    sessionId: Math.random().toString(36).slice(2),
    title: 't',
    cwd: null,
    repoRoot: null,
    worktree: null,
    updatedAt: 0,
    messagesCount: null,
    archivedAt: null,
    tags: [],
    parentId: null,
    callingSessionId: null,
    callingToolUseId: null,
    ...overrides,
  }
}

const lead = (workstreamId: string) => [
  { name: WORKSTREAM_TAG, metadata: { workstreamId, role: 'orchestrator', agentModel: 'gpt-6' } },
]
const agentOf = (workstreamId: string, orchestratorId: string) => [
  {
    name: WORKSTREAM_TAG,
    metadata: {
      workstreamId,
      role: 'agent',
      orchestratorId,
      agentModel: 'glm-5.3-flash',
      agentReasoningEffort: 'high',
    },
  },
]

describe('workstreamOf', () => {
  test('reads the role, Workstream and orchestrator from the tag', () => {
    expect(workstreamOf(lead('w1'))).toEqual({
      workstreamId: 'w1',
      role: 'orchestrator',
      orchestratorId: null,
    })
    expect(workstreamOf(agentOf('w1', 'o1'))).toEqual({
      workstreamId: 'w1',
      role: 'agent',
      orchestratorId: 'o1',
    })
  })

  test('no tag, another tag, or one without an id or a known role is no membership', () => {
    expect(workstreamOf(undefined)).toBeNull()
    expect(workstreamOf([{ name: 'droi.scratch' }])).toBeNull()
    expect(workstreamOf([{ name: WORKSTREAM_TAG }])).toBeNull()
    expect(workstreamOf([{ name: WORKSTREAM_TAG, metadata: { role: 'agent' } }])).toBeNull()
    expect(
      workstreamOf([{ name: WORKSTREAM_TAG, metadata: { workstreamId: 'w', role: 'boss' } }]),
    ).toBeNull()
  })
})

describe('the Session list', () => {
  test('leaves agents out and keeps the orchestrator and every other Session', () => {
    const sessions = [
      summary({ sessionId: 'o', tags: lead('w1') }),
      summary({ sessionId: 'a', tags: agentOf('w1', 'o') }),
      summary({ sessionId: 'plain' }),
      summary({ sessionId: 'b', tags: agentOf('w2', 'x') }),
    ]
    expect(withoutWorkstreamAgents(sessions).map((s) => s.sessionId)).toEqual(['o', 'plain'])
    expect(isWorkstreamAgent(sessions[1]!)).toBe(true)
    expect(isWorkstreamAgent(sessions[0]!)).toBe(false)
  })
})

describe("an orchestrator's agents", () => {
  const orchestrator = summary({ sessionId: 'o', title: 'Payments', tags: lead('w1') })
  const sessions = [
    orchestrator,
    summary({ sessionId: 'old', updatedAt: 1, tags: agentOf('w1', 'o') }),
    summary({ sessionId: 'new', updatedAt: 9, tags: agentOf('w1', 'o') }),
    summary({
      sessionId: 'gone',
      updatedAt: 5,
      archivedAt: '2026-11-01',
      tags: agentOf('w1', 'o'),
    }),
    summary({ sessionId: 'other', updatedAt: 7, tags: agentOf('w2', 'p') }),
    summary({ sessionId: 'plain', updatedAt: 8 }),
  ]

  test('are the unarchived agents of its Workstream, newest first', () => {
    expect(agentsOf(sessions, orchestrator).map((s) => s.sessionId)).toEqual(['new', 'old'])
  })

  test('a Session that leads no Workstream has none', () => {
    expect(agentsOf(sessions, summary({}))).toEqual([])
    expect(agentsOf(sessions, sessions[1]!)).toEqual([])
  })

  test('group as waiting on you, working and idle', () => {
    const activity = new Map([
      ['old', 'needs-input' as const],
      ['new', 'working' as const],
    ])
    const groups = groupAgents(agentsOf(sessions, orchestrator), activity)
    expect(groups.waiting.map((s) => s.sessionId)).toEqual(['old'])
    expect(groups.working.map((s) => s.sessionId)).toEqual(['new'])
    expect(groups.idle).toEqual([])
    expect(groupAgents(agentsOf(sessions, orchestrator), new Map()).idle).toHaveLength(2)
  })

  test('compacting counts as working', () => {
    expect(agentStatus('compacting')).toBe('working')
    expect(agentStatus('needs-input')).toBe('waiting')
    expect(agentStatus(undefined)).toBe('idle')
  })
})

describe('orchestratorOf', () => {
  test("is the latest link of the orchestrator's compaction chain", () => {
    const first = summary({ sessionId: 'o1', tags: lead('w1') })
    const second = summary({ sessionId: 'o2', parentId: 'o1', tags: lead('w1') })
    const agent = summary({ sessionId: 'a', tags: agentOf('w1', 'o1') })
    expect(orchestratorOf([first, agent, second], agent)?.sessionId).toBe('o2')
  })

  test('is null for a Session that is no agent, or one whose orchestrator is not listed', () => {
    const agent = summary({ tags: agentOf('w1', 'o1') })
    expect(orchestratorOf([agent], agent)).toBeNull()
    const o = summary({ tags: lead('w1') })
    expect(orchestratorOf([o], o)).toBeNull()
  })
})

describe('labels', () => {
  test('the row counts agents and those waiting, leaving out none waiting', () => {
    expect(workstreamRowLabel(3, 1)).toBe('Workstream · 3 agents · 1 waiting')
    expect(workstreamRowLabel(1, 0)).toBe('Workstream · 1 agent')
    expect(workstreamRowLabel(0, 0)).toBe('Workstream · 0 agents')
    expect(agentCount(2)).toBe('2 agents')
  })
})
