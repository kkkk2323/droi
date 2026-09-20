import { describe, expect, it } from 'vitest'
import { groupModels } from './session-toolbar'
import type { ModelChoice } from '@/daemon/use-session-settings'

const model = (id: string, provider: string | null, label = id): ModelChoice => ({
  id,
  label,
  provider,
  reasoningEfforts: [],
  disabled: false,
})

describe('groupModels', () => {
  it('groups by provider in first-seen order, naming the router group Auto', () => {
    const groups = groupModels([
      model('auto', null, 'Auto Model'),
      model('claude-opus-4-1', 'anthropic'),
      model('gpt-5', 'openai'),
      model('claude-sonnet-4', 'anthropic'),
      model('glm-5.3-flash', 'zai'),
    ])
    expect(groups.map((g) => [g.label, g.options.map((o) => o.value)])).toEqual([
      ['Auto', ['auto']],
      ['Anthropic', ['claude-opus-4-1', 'claude-sonnet-4']],
      ['OpenAI', ['gpt-5']],
      ['Zai', ['glm-5.3-flash']],
    ])
  })

  it('returns no groups for no models', () => {
    expect(groupModels([])).toEqual([])
  })
})
