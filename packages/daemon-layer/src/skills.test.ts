import { describe, expect, test } from 'vitest'
import { groupSkills, type Skill } from './skills'

const skill = (overrides: Partial<Skill>): Skill => ({
  name: 'review',
  scope: 'personal',
  path: '/skills/review/SKILL.md',
  enabled: true,
  ...overrides,
})

describe('groupSkills', () => {
  test('groups the project’s skills before the personal ones, names sorted inside', () => {
    const groups = groupSkills([
      skill({ name: 'zeta' }),
      skill({ name: 'alpha' }),
      skill({ name: 'ours', scope: 'workspace' }),
    ])
    expect(groups.map((g) => [g.scope, g.skills.map((s) => s.name)])).toEqual([
      ['workspace', ['ours']],
      ['personal', ['alpha', 'zeta']],
    ])
  })
})
