import { describe, expect, test } from 'vitest'
import type { SessionSummary } from './sessions'
import {
  callerTrail,
  formatRunDuration,
  launchedSessionId,
  listedSessionOf,
  mainSessions,
  runningSubagents,
  subagentName,
  subagentsOf,
  taskReport,
  taskRequest,
  taskState,
  type SubagentRun,
} from './subagents'
import type { ToolCall } from './transcript'

function summary(overrides: Partial<SessionSummary>): SessionSummary {
  return {
    sessionId: Math.random().toString(36).slice(2),
    title: 't',
    cwd: null,
    repoRoot: null,
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

function task(result: { content: string; isError?: boolean } | null, input = {}): ToolCall {
  return {
    use: { type: 'tool_use', id: 'call', name: 'Task', input },
    result: result ? { type: 'tool_result', toolUseId: 'call', ...result } : null,
  } as ToolCall
}

const run = (status: SubagentRun['status']): SubagentRun => ({
  status,
  toolUseCount: null,
  durationMs: null,
})

describe('subagents in the list', () => {
  const main = summary({ sessionId: 'main', title: 'Main' })
  const older = summary({ sessionId: 'a', callingSessionId: 'main', updatedAt: 1 })
  const newer = summary({ sessionId: 'b', callingSessionId: 'main', updatedAt: 2 })
  const nested = summary({ sessionId: 'c', callingSessionId: 'b', title: 'Nested' })
  const sessions = [main, older, newer, nested]

  test('only main Sessions are listed', () => {
    expect(mainSessions(sessions)).toEqual([main])
  })

  test('subagentsOf lists the direct subagents of the given callers, newest first', () => {
    expect(subagentsOf(sessions, ['main']).map((s) => s.sessionId)).toEqual(['b', 'a'])
    expect(subagentsOf(sessions, ['b']).map((s) => s.sessionId)).toEqual(['c'])
  })

  test('callerTrail walks up to the main Session, and names a caller the list lacks', () => {
    expect(callerTrail(sessions, nested)).toEqual([
      { sessionId: 'main', title: 'Main' },
      { sessionId: 'b', title: 't' },
    ])
    expect(callerTrail(sessions, main)).toEqual([])
    expect(callerTrail([], { sessionId: 'x', callingSessionId: 'gone' })).toEqual([
      { sessionId: 'gone', title: 'Main session' },
    ])
  })

  test('a subagent belongs to its main Session’s row, at the latest compaction', () => {
    const continued = summary({ sessionId: 'main2', parentId: 'main' })
    expect(listedSessionOf([...sessions, continued], 'c')).toBe('main2')
    expect(listedSessionOf(sessions, 'main')).toBe('main')
  })

  test('runningSubagents counts running subagents per listed row', () => {
    const runs = new Map([
      ['a', run('completed')],
      ['b', run('running')],
      ['c', run('pending')],
    ])
    expect(runningSubagents(sessions, runs)).toEqual(new Map([['main', 2]]))
  })
})

describe('Task calls', () => {
  test('reads the request from the input', () => {
    expect(
      taskRequest(task(null, { subagent_type: 'explorer', description: 'Map it', prompt: 'Go' })),
    ).toEqual({ subagentType: 'explorer', description: 'Map it', prompt: 'Go' })
    expect(subagentName('explorer')).toBe('Explorer')
    expect(subagentName('')).toBe('Subagent')
  })

  test('a background launch names the subagent’s Session and is no report', () => {
    const launch = task({
      content:
        'Task launched in background.\ntask_id: s-1\nsession_id: s-1\nsubagent_type: explorer',
    })
    expect(launchedSessionId(launch)).toBe('s-1')
    expect(taskState(launch, null)).toBe('launched')
    expect(taskReport(launch)).toBe('')
  })

  test('the Daemon’s account wins over the tool result, except for a failed call', () => {
    expect(taskState(task(null), null)).toBe('running')
    expect(taskState(task({ content: 'report' }), null)).toBe('completed')
    expect(taskReport(task({ content: 'report' }))).toBe('report')
    expect(taskState(task({ content: 'Task launched in background.' }), run('completed'))).toBe(
      'completed',
    )
    expect(taskState(task({ content: 'boom', isError: true }), run('running'))).toBe('failed')
  })

  test('formats run durations', () => {
    expect(formatRunDuration(4_200)).toBe('4s')
    expect(formatRunDuration(134_000)).toBe('2m 14s')
    expect(formatRunDuration(3_900_000)).toBe('1h 5m')
  })
})
