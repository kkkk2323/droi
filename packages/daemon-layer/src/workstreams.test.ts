import { describe, expect, test } from 'vitest'
import type { SessionSummary } from './sessions'
import {
  WORKSTREAM_TAG,
  agentCount,
  agentStatus,
  agentsByWorkstream,
  agentsOf,
  groupAgents,
  isWorkstreamAgent,
  ledWorkstreams,
  orchestratorOf,
  waitingLabel,
  withBusyWorkstreams,
  withoutWorkstreamAgents,
  workstreamLabel,
  workstreamLoads,
  workstreamOf,
} from './workstreams'

function summary(overrides: Partial<SessionSummary>): SessionSummary {
  return {
    sessionId: Math.random().toString(36).slice(2),
    title: 't',
    cwd: null,
    repoRoot: null,
    worktree: null,
    updatedAt: 0,
    activityAt: 0,
    viewedAt: 0,
    kind: 'chat',
    messagesCount: null,
    archivedAt: null,
    tags: [],
    parentId: null,
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
  test('leaves out agents whose orchestrator is listed, and keeps every other Session', () => {
    const sessions = [
      summary({ sessionId: 'o', tags: lead('w1') }),
      summary({ sessionId: 'a', tags: agentOf('w1', 'o') }),
      summary({ sessionId: 'plain' }),
      summary({ sessionId: 'b', tags: agentOf('w1', 'o') }),
    ]
    const led = ledWorkstreams(sessions, { includeArchived: false })
    expect([...led]).toEqual(['w1'])
    expect(withoutWorkstreamAgents(sessions, led).map((s) => s.sessionId)).toEqual(['o', 'plain'])
    expect(isWorkstreamAgent(sessions[1]!)).toBe(true)
    expect(isWorkstreamAgent(sessions[0]!)).toBe(false)
  })

  test('an agent whose orchestrator the list lacks stays listed', () => {
    const orphan = summary({ sessionId: 'a', tags: agentOf('w2', 'gone') })
    const sessions = [summary({ sessionId: 'o', tags: lead('w1') }), orphan]
    const led = ledWorkstreams(sessions, { includeArchived: false })
    expect(withoutWorkstreamAgents(sessions, led).map((s) => s.sessionId)).toEqual(['o', 'a'])
  })

  test('an archived orchestrator leads its agents only while archived Sessions show', () => {
    const sessions = [
      summary({ sessionId: 'o', archivedAt: '2026-11-01', tags: lead('w1') }),
      summary({ sessionId: 'a', tags: agentOf('w1', 'o') }),
    ]
    const hidden = ledWorkstreams(sessions, { includeArchived: false })
    expect(withoutWorkstreamAgents(sessions, hidden).map((s) => s.sessionId)).toEqual(['o', 'a'])
    const shown = ledWorkstreams(sessions, { includeArchived: true })
    expect(withoutWorkstreamAgents(sessions, shown).map((s) => s.sessionId)).toEqual(['o'])
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

  test('are mapped by Workstream in one pass', () => {
    const byWorkstream = agentsByWorkstream(sessions)
    expect([...byWorkstream.keys()].sort()).toEqual(['w1', 'w2'])
    expect(byWorkstream.get('w1')!.map((s) => s.sessionId)).toEqual(['new', 'old'])
    expect(byWorkstream.get('w2')!.map((s) => s.sessionId)).toEqual(['other'])
  })

  test('an agent compacted into a new Session counts once, as its newest link', () => {
    // The child Session copies its parent's tags; the parent stays unarchived.
    const first = summary({ sessionId: 'a1', updatedAt: 3, tags: agentOf('w1', 'o') })
    const second = summary({
      sessionId: 'a2',
      updatedAt: 4,
      parentId: 'a1',
      tags: agentOf('w1', 'o'),
    })
    const chained = [orchestrator, first, second]
    expect(agentsOf(chained, orchestrator).map((s) => s.sessionId)).toEqual(['a2'])
    expect(
      agentsByWorkstream(chained)
        .get('w1')!
        .map((s) => s.sessionId),
    ).toEqual(['a2'])
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

describe('an orchestrator’s load in the list', () => {
  const o1 = summary({ sessionId: 'o1', tags: lead('w1') })
  const o2 = summary({ sessionId: 'o2', tags: lead('w2') })
  const plain = summary({ sessionId: 'plain' })
  const sessions = [
    o1,
    o2,
    plain,
    summary({ sessionId: 'a', tags: agentOf('w1', 'o1') }),
    summary({ sessionId: 'b', tags: agentOf('w1', 'o1') }),
    summary({ sessionId: 'c', tags: agentOf('w1', 'o1') }),
    summary({ sessionId: 'd', tags: agentOf('w2', 'o2') }),
  ]
  const activity = new Map([
    ['a', 'needs-input' as const],
    ['b', 'working' as const],
  ])
  const loads = workstreamLoads([o1, o2, plain], agentsByWorkstream(sessions), activity)

  test('counts each orchestrator’s agents, those waiting and those working', () => {
    expect(loads).toEqual(
      new Map([
        ['o1', { agents: 3, waiting: 1, working: 1 }],
        ['o2', { agents: 1, waiting: 0, working: 0 }],
      ]),
    )
  })

  test('an orchestrator whose agents work or wait is busy, like a Session that works', () => {
    expect([...withBusyWorkstreams(new Set(['plain']), loads)].sort()).toEqual(['o1', 'plain'])
  })
})

describe('orchestratorOf', () => {
  test("is the latest link of the orchestrator's compaction chain", () => {
    const first = summary({ sessionId: 'o1', tags: lead('w1') })
    const second = summary({ sessionId: 'o2', parentId: 'o1', tags: lead('w1') })
    const agent = summary({ sessionId: 'a', tags: agentOf('w1', 'o1') })
    expect(orchestratorOf([first, agent, second], agent)?.sessionId).toBe('o2')
  })

  test('an orchestrator that names itself as its parent is still found', () => {
    const self = summary({ sessionId: 'o', parentId: 'o', tags: lead('w1') })
    const agent = summary({ sessionId: 'a', tags: agentOf('w1', 'o') })
    expect(orchestratorOf([self, agent], agent)?.sessionId).toBe('o')
  })

  test('is null for a Session that is no agent, or one whose orchestrator is not listed', () => {
    const agent = summary({ tags: agentOf('w1', 'o1') })
    expect(orchestratorOf([agent], agent)).toBeNull()
    const o = summary({ tags: lead('w1') })
    expect(orchestratorOf([o], o)).toBeNull()
  })
})

describe('labels', () => {
  test('the row counts agents, and those waiting apart', () => {
    expect(workstreamLabel(3)).toBe('Workstream · 3 agents')
    expect(workstreamLabel(1)).toBe('Workstream · 1 agent')
    expect(workstreamLabel(0)).toBe('Workstream · 0 agents')
    expect(waitingLabel(1)).toBe('1 waiting')
    expect(agentCount(2)).toBe('2 agents')
  })
})
