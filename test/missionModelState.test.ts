import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildRuntimeModelCatalog,
  getRuntimeModelDefaultReasoning,
  getRuntimeModelReasoningLevels,
} from '../src/renderer/src/lib/modelCatalog.ts'
import { resolveSessionRuntimeSelection } from '../src/renderer/src/lib/missionModelState.ts'

const AVAILABLE_MODELS = [
  {
    id: 'gpt-5.4-mini',
    modelId: 'gpt-5.4-mini',
    displayName: 'GPT-5.4 Mini',
    modelProvider: 'openai',
    tokenMultiplier: 0.3,
    supportedReasoningEfforts: ['none', 'low', 'medium', 'high', 'xhigh'],
    defaultReasoningEffort: 'high',
    isCustom: false,
  },
  {
    id: 'gemini-3-flash-preview',
    modelId: 'gemini-3-flash-preview',
    displayName: 'Gemini 3 Flash Preview',
    modelProvider: 'google',
    supportedReasoningEfforts: ['low', 'medium', 'high'],
    defaultReasoningEffort: 'high',
    isCustom: false,
  },
] as const

test('gpt-5.4-mini is available and uses the expected reasoning defaults', () => {
  const catalog = buildRuntimeModelCatalog({ availableModels: [...AVAILABLE_MODELS] })
  const model = catalog.flatMap((group) => group.options).find((entry) => entry.value === 'gpt-5.4-mini')

  assert.ok(model)
  assert.equal(model.provider, 'openai')
  assert.equal(model.multiplier, '0.3×')
  assert.deepEqual(getRuntimeModelReasoningLevels('gpt-5.4-mini', [...AVAILABLE_MODELS]), [
    'none',
    'low',
    'medium',
    'high',
    'xhigh',
  ])
  assert.equal(getRuntimeModelDefaultReasoning('gpt-5.4-mini', [...AVAILABLE_MODELS]), 'high')
})

test('Mission session creation prefers orchestrator model and resets reasoning for the new model', () => {
  const selection = resolveSessionRuntimeSelection({
    isMission: true,
    sessionModel: 'gpt-5.4',
    sessionReasoningEffort: 'xhigh',
    missionModelSettings: { orchestratorModel: 'gemini-3-flash-preview' },
    availableModels: [...AVAILABLE_MODELS],
  })

  assert.equal(selection.model, 'gemini-3-flash-preview')
  assert.equal(
    selection.reasoningEffort,
    getRuntimeModelDefaultReasoning('gemini-3-flash-preview', [...AVAILABLE_MODELS]) || '',
  )
})

test('Mission hot-switch preserves the current reasoning when the orchestrator model is unchanged', () => {
  const selection = resolveSessionRuntimeSelection({
    isMission: true,
    sessionModel: 'gemini-3-flash-preview',
    sessionReasoningEffort: 'minimal',
    missionModelSettings: { orchestratorModel: 'gemini-3-flash-preview' },
    availableModels: [...AVAILABLE_MODELS],
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
    availableModels: [...AVAILABLE_MODELS],
  })

  assert.deepEqual(selection, {
    model: 'gpt-5.4',
    reasoningEffort: 'medium',
  })
})
