import test from 'node:test'
import assert from 'node:assert/strict'
import {
  getAppRouteTarget,
  getSessionRouteTarget,
  getSessionSidebarTestId,
} from '../src/renderer/src/lib/sessionRouting.ts'
import {
  getPendingSessionProtocol,
  mergePendingSessionDraft,
} from '../src/renderer/src/lib/pendingSessionDraft.ts'

test('mission session kind stays selected when workspace mode changes', () => {
  const pending = {
    repoRoot: '/repo',
    branch: '',
    mode: 'local' as const,
    sessionKind: 'mission' as const,
  }

  const next = mergePendingSessionDraft(pending, {
    mode: 'new-worktree',
    branch: 'feature/mission-routing',
  })

  assert.equal(next.mode, 'new-worktree')
  assert.equal(next.branch, 'feature/mission-routing')
  assert.equal(next.sessionKind, 'mission')
  assert.deepEqual(getPendingSessionProtocol(next, 'high'), {
    isMission: true,
    sessionKind: 'mission',
    interactionMode: 'agi',
    autonomyLevel: 'high',
    decompSessionType: 'orchestrator',
  })
  assert.equal(getPendingSessionProtocol(next, 'default').autonomyLevel, 'off')
})

test('mission sessions route to /mission while normal sessions route to /', () => {
  assert.equal(getSessionRouteTarget({ sessionKind: 'mission' }), '/mission')
  assert.equal(getSessionRouteTarget({ isMission: true }), '/mission')
  assert.equal(getSessionRouteTarget({ sessionKind: 'normal', interactionMode: 'spec' }), '/')

  assert.equal(
    getAppRouteTarget({
      activeSession: { sessionKind: 'mission', interactionMode: 'agi' },
    }),
    '/mission',
  )
  assert.equal(
    getAppRouteTarget({
      hasPendingNewSession: true,
      activeSession: { sessionKind: 'mission', interactionMode: 'agi' },
    }),
    '/',
  )
})

test('mission sidebar rows use explicit mission targeting test ids', () => {
  assert.equal(
    getSessionSidebarTestId({ id: 'mission-123', sessionKind: 'mission' }),
    'session-mission-mission-123',
  )
  assert.equal(
    getSessionSidebarTestId({ id: 'local-123', sessionKind: 'normal' }),
    'session-local-123',
  )
})
