import test from 'node:test'
import assert from 'node:assert/strict'

import { MODELS, getModelDefaultReasoning, getModelReasoningLevels } from '../src/renderer/src/types.ts'
import { resolveSessionRuntimeSelection } from '../src/renderer/src/lib/missionModelState.ts'

test('gpt-5.4-mini is available and uses the expected reasoning defaults', () => {
  const model = MODELS.find((entry) => entry.value === 'gpt-5.4-mini')

  assert.ok(model)
  assert.equal(model.provider, 'openai')
  assert.equal(model.multiplier, '0.3×')
  assert.deepEqual(getModelReasoningLevels('gpt-5.4-mini'), ['none', 'low', 'medium', 'high', 'xhigh'])
  assert.equal(getModelDefaultReasoning('gpt-5.4-mini'), 'high')
})

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
