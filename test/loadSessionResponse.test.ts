import test from 'node:test'
import assert from 'node:assert/strict'
import { mergeLoadSessionResponse } from '../src/backend/session/loadSessionResponse.ts'

test('mergeLoadSessionResponse preserves stored UI messages while dropping live pending state', () => {
  const merged = mergeLoadSessionResponse(
    {
      id: 'session-1',
      projectDir: '/repo',
      title: 'Session 1',
      savedAt: 1,
      messages: [{ id: 'stored-msg', role: 'user', blocks: [], timestamp: 1 } as any],
      model: 'stored-model',
      autoLevel: 'low',
    },
    {
      session: {
        messages: [{ id: 'live-msg', role: 'assistant', blocks: [], timestamp: 2 }],
      },
      settings: {
        modelId: 'live-model',
        interactionMode: 'auto',
        autonomyLevel: 'medium',
        reasoningEffort: 'high',
      },
      pendingPermissions: [{ requestId: 'perm-1', options: ['proceed_once'] }],
      pendingAskUserRequests: [{ requestId: 'ask-1', toolCallId: 'tool-1', questions: [] }],
      isAgentLoopInProgress: true,
    },
  )

  assert.ok(merged)
  assert.equal(merged?.model, 'live-model')
  assert.equal(merged?.interactionMode, 'auto')
  assert.equal(merged?.autonomyLevel, 'low')
  assert.equal(merged?.reasoningEffort, 'high')
  assert.equal(merged?.messages[0]?.id, 'stored-msg')
  assert.deepEqual(merged?.pendingPermissions, [])
  assert.deepEqual(merged?.pendingAskUserRequests, [])
  assert.equal(merged?.isAgentLoopInProgress, true)
})
