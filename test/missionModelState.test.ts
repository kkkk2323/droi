import test from 'node:test'
import assert from 'node:assert/strict'

import { getModelDefaultReasoning } from '../src/renderer/src/types.ts'
import { resolveSessionRuntimeSelection } from '../src/renderer/src/lib/missionModelState.ts'

test('Mission session creation prefers orchestrator model and resets reasoning for the new model', () => {
  const selection = resolveSessionRuntimeSelection({
    isMission: true,
    sessionModel: 'gpt-5.4',
    sessionReasoningEffort: 'xhigh',
    missionModelSettings: { orchestratorModel: 'gemini-3-flash-preview' },
  })

  assert.equal(selection.model, 'gemini-3-flash-preview')
  assert.equal(
    selection.reasoningEffort,
    getModelDefaultReasoning('gemini-3-flash-preview') || '',
  )
})

test('Mission hot-switch preserves the current reasoning when the orchestrator model is unchanged', () => {
  const selection = resolveSessionRuntimeSelection({
    isMission: true,
    sessionModel: 'gemini-3-flash-preview',
    sessionReasoningEffort: 'minimal',
    missionModelSettings: { orchestratorModel: 'gemini-3-flash-preview' },
  })

  assert.deepEqual(selection, {
    model: 'gemini-3-flash-preview',
    reasoningEffort: 'minimal',
  })
})

test('Normal sessions keep their own runtime model even when Mission settings are configured', () => {
  const selection = resolveSessionRuntimeSelection({
    isMission: false,
    sessionModel: 'gpt-5.4',
    sessionReasoningEffort: 'medium',
    missionModelSettings: { orchestratorModel: 'gemini-3-flash-preview' },
  })

  assert.deepEqual(selection, {
    model: 'gpt-5.4',
    reasoningEffort: 'medium',
  })
})
