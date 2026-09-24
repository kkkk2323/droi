import { describe, expect, test } from 'vitest'
import {
  applyPatch,
  reasoningChoices,
  specSaveChoice,
  specSavePaths,
  toSessionDefaults,
  tokenLimitLabel,
  withSubagentTier,
} from './session-defaults'

const MODELS = [
  {
    id: 'auto',
    displayName: 'Auto Model',
    modelProvider: 'factory',
    supportedReasoningEfforts: ['none'],
    isCustom: false,
    kind: 'router',
  },
  {
    id: 'gpt-5',
    displayName: 'GPT-5',
    modelProvider: 'openai',
    supportedReasoningEfforts: ['low', 'medium', 'high'],
    isCustom: false,
  },
]

describe('session defaults', () => {
  test('reads the Daemon’s answer, filling what it leaves out', () => {
    const view = toSessionDefaults({
      reasoningEffort: 'high',
      availableModels: MODELS,
      specSavePresets: { userFactoryDir: '/Users/dev/.factory' },
      management: {
        modelId: { disabled: true, source: 'org' },
        reasoningEffort: { disabled: false, source: 'builtin' },
        subagent: { heavyModel: { disabled: true, source: 'org' } },
      },
    })
    expect(view).toMatchObject({
      modelId: null,
      reasoningEffort: 'high',
      interactionMode: 'auto',
      compactionTokenLimit: null,
      compactionTokenLimitPerModel: {},
      compactionModel: 'current-model',
      compactionThresholdCheckEnabled: true,
      subagentAutonomyLevel: 'inherit',
      subagentModelSettings: {},
      availableAutonomyLevels: ['off', 'low', 'medium', 'high'],
      userFactoryDir: '/Users/dev/.factory',
    })
    expect(view.models.map((m) => m.id)).toEqual(['auto', 'gpt-5'])
    expect([...view.locked]).toEqual(['modelId', 'subagent.heavyModel'])
  })

  test('a patch shows at once; null clears a "same as main" choice', () => {
    const view = toSessionDefaults({ specModeModelId: 'gpt-5', compactionTokenLimit: 300_000 })
    const next = applyPatch(view, { specModeModelId: null, compactionTokenLimit: 500_000 })
    expect(next.specModeModelId).toBeNull()
    expect(next.compactionTokenLimit).toBe(500_000)
    expect(view.specModeModelId).toBe('gpt-5')
  })

  test('reasoning levels follow the model, or keep the current value without one', () => {
    const { models } = toSessionDefaults({ availableModels: MODELS })
    expect(reasoningChoices(models, 'gpt-5', 'high')).toEqual(['low', 'medium', 'high'])
    expect(reasoningChoices(models, 'gone', 'xhigh')).toEqual(['xhigh'])
    expect(reasoningChoices(models, null, null)).toEqual(['medium'])
  })

  test('a tier change sends every tier; inheriting drops the tier’s model and level', () => {
    const settings = { lightModel: 'gpt-5', lightReasoningEffort: 'low', heavyModel: 'opus' }
    expect(withSubagentTier(settings, 'medium', { model: 'gpt-5' })).toEqual({
      ...settings,
      mediumModel: 'gpt-5',
    })
    expect(withSubagentTier(settings, 'light', { reasoningEffort: 'high' })).toEqual({
      ...settings,
      lightReasoningEffort: 'high',
    })
    expect(withSubagentTier(settings, 'light', { model: null })).toEqual({ heavyModel: 'opus' })
    expect(withSubagentTier(settings, 'light', { reasoningEffort: null })).toEqual({
      lightModel: 'gpt-5',
      heavyModel: 'opus',
    })
    // A new model starts from its own default level.
    expect(withSubagentTier(settings, 'light', { model: 'opus' })).toEqual({
      lightModel: 'opus',
      heavyModel: 'opus',
    })
  })

  test('spec folders: nothing stored is the home folder, as the Factory App reads it', () => {
    const paths = specSavePaths('/Users/dev/.factory')
    expect(paths).toEqual({ user: '~/.factory/docs', project: '.factory/docs' })
    expect(specSavePaths(null)).toEqual(paths)
    expect(specSaveChoice(null, paths)).toBe('user')
    expect(specSaveChoice('~/.factory/docs', paths)).toBe('user')
    expect(specSaveChoice('.factory/docs', paths)).toBe('project')
    expect(specSaveChoice('/Users/dev/specs', paths)).toBe('custom')
  })

  test('token limits read as Factory writes them', () => {
    expect(tokenLimitLabel(250_000)).toBe('250K')
    expect(tokenLimitLabel(1_000_000)).toBe('1M')
  })
})
