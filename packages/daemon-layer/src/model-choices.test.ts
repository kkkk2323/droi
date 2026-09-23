import { describe, expect, it } from 'vitest'
import { brandOf } from './model-brand'
import { brandsOf, formatMultiplier, visibleModels } from './model-choices'
import { toModelChoices, type ModelChoice } from './use-session-settings'

const model = (id: string, provider: string | null, label = id): ModelChoice => ({
  id,
  label,
  provider,
  reasoningEfforts: [],
  disabled: false,
  multiplier: null,
})

const models = [
  model('auto', null, 'Auto Model'),
  model('claude-opus-4-1', 'anthropic', 'Claude Opus 4.1'),
  model('gpt-5', 'openai', 'GPT-5'),
  model('glm-5.3-flash', 'anthropic', 'GLM 5.3 Flash'),
  model('gemini-3-pro', 'generic-chat-completion-api', 'Gemini 3 Pro'),
]

describe('brandOf', () => {
  it('reads the brand off the model id before trusting the wire provider', () => {
    expect(brandOf('glm-5.3-flash', 'anthropic')).toBe('zhipu')
    expect(brandOf('gemini-3-pro', 'generic-chat-completion-api')).toBe('google')
    expect(brandOf('claude-opus-4-1', 'anthropic')).toBe('anthropic')
  })

  it('falls back to the provider, then to other', () => {
    expect(brandOf('my-fine-tune', 'openai')).toBe('openai')
    expect(brandOf('auto', null)).toBe('other')
    expect(brandOf('mystery', 'generic-chat-completion-api')).toBe('other')
  })
})

describe('brandsOf', () => {
  it('lists the brands present in rail order', () => {
    expect(brandsOf(models)).toEqual(['anthropic', 'openai', 'google', 'zhipu', 'other'])
  })
})

describe('visibleModels', () => {
  it('shows everything by default', () => {
    expect(visibleModels(models, [], 'all', '').map((m) => m.id)).toEqual(models.map((m) => m.id))
  })

  it('filters by brand', () => {
    expect(visibleModels(models, [], 'zhipu', '').map((m) => m.id)).toEqual(['glm-5.3-flash'])
  })

  it('lists favorites in the order they were starred', () => {
    const favorites = ['gpt-5', 'auto']
    expect(visibleModels(models, favorites, 'favorites', '').map((m) => m.id)).toEqual([
      'gpt-5',
      'auto',
    ])
  })

  it('searches label, id and brand across every model, ignoring the filter', () => {
    expect(visibleModels(models, [], 'openai', 'flash').map((m) => m.id)).toEqual(['glm-5.3-flash'])
    expect(visibleModels(models, [], 'favorites', 'zhipu').map((m) => m.id)).toEqual([
      'glm-5.3-flash',
    ])
    expect(visibleModels(models, [], 'all', 'opus 4.1').map((m) => m.id)).toEqual([
      'claude-opus-4-1',
    ])
  })
})

describe('multipliers', () => {
  it('keeps Factory’s multiplier, and none for a custom model', () => {
    const base = {
      displayName: 'm',
      shortDisplayName: 'm',
      modelProvider: 'anthropic',
      supportedReasoningEfforts: [],
      defaultReasoningEffort: 'none',
    }
    const choices = toModelChoices([
      { ...base, id: 'opus', isCustom: false, tokenMultiplier: 1.6 },
      { ...base, id: 'mine', isCustom: true, tokenMultiplier: 1 },
      { ...base, id: 'unpriced', isCustom: false },
    ] as unknown as Parameters<typeof toModelChoices>[0])
    expect(choices.map((c) => c.multiplier)).toEqual([1.6, null, null])
  })

  it('formats like Factory', () => {
    expect(formatMultiplier(1.6)).toBe('1.6×')
    expect(formatMultiplier(0.04)).toBe('0.04×')
    expect(formatMultiplier(1)).toBe('1×')
    expect(formatMultiplier(0.333333)).toBe('0.33×')
  })
})
