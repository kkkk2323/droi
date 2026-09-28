import { describe, expect, test } from 'vitest'
import { groupSkills, isDisabledByOrg, skillSwitches, type Skill } from './skills'

const skill = (overrides: Partial<Skill>): Skill =>
  ({
    name: 'review',
    location: 'builtin',
    filePath: '/skills/review/SKILL.md',
    enabled: true,
    ...overrides,
  }) as Skill

describe('skillSwitches', () => {
  test('a built-in skill in a project can go off for the project or everywhere', () => {
    expect(skillSwitches(skill({}), true).map((s) => [s.level, s.disabled, s.label])).toEqual([
      ['project', true, 'Disable for this project'],
      ['user', true, 'Disable across all projects'],
    ])
  })

  test('outside a project a built-in skill has one switch, worded plainly', () => {
    expect(skillSwitches(skill({}), false).map((s) => s.label)).toEqual(['Disable'])
  })

  test('a project skill only goes off for its project', () => {
    expect(skillSwitches(skill({ location: 'project' } as Partial<Skill>), true)).toEqual([
      { disabled: true, level: 'project', label: 'Disable for this project' },
    ])
  })

  test('a disabled skill offers to come back only at the level that turned it off', () => {
    const off = skill({
      enabled: false,
      disabledBy: { kind: 'ledger', sources: [{ level: 'user' }] },
    } as Partial<Skill>)
    expect(skillSwitches(off, true)).toEqual([{ disabled: false, level: 'user', label: 'Enable' }])
  })

  test('personal skills and ones the organization turned off have no switch', () => {
    expect(skillSwitches(skill({ location: 'personal' } as Partial<Skill>), true)).toEqual([])
    const byOrg = skill({
      enabled: false,
      disabledBy: { kind: 'ledger', sources: [{ level: 'org' }] },
    } as Partial<Skill>)
    expect(skillSwitches(byOrg, true)).toEqual([])
    expect(isDisabledByOrg(byOrg)).toBe(true)
  })
})

describe('groupSkills', () => {
  test('groups by location in a fixed order, names sorted inside', () => {
    const groups = groupSkills([
      skill({ name: 'zeta' }),
      skill({ name: 'alpha' }),
      skill({ name: 'mine', location: 'personal' } as Partial<Skill>),
      skill({ name: 'ours', location: 'project' } as Partial<Skill>),
    ])
    expect(groups.map((g) => [g.location, g.skills.map((s) => s.name)])).toEqual([
      ['project', ['ours']],
      ['personal', ['mine']],
      ['builtin', ['alpha', 'zeta']],
    ])
  })
})
