import { describe, expect, test } from 'vitest'
import { disabledLabel, groupSkills, skillSwitch, type Skill } from './skills'

const skill = (overrides: Partial<Skill>): Skill =>
  ({
    name: 'review',
    location: 'builtin',
    filePath: '/skills/review/SKILL.md',
    enabled: true,
    ...overrides,
  }) as Skill

const off = (levels: string[], overrides: Partial<Skill> = {}): Skill =>
  skill({
    enabled: false,
    disabledBy: { kind: 'ledger', sources: levels.map((level) => ({ level })) },
    ...overrides,
  } as Partial<Skill>)

describe('skillSwitch', () => {
  test('a built-in skill switches for the user, with or without a project', () => {
    expect(skillSwitch(skill({}), true)).toEqual({ on: true, level: 'user' })
    expect(skillSwitch(skill({}), false)).toEqual({ on: true, level: 'user' })
  })

  test('a project skill switches for its project, and not without one', () => {
    const project = skill({ location: 'project' } as Partial<Skill>)
    expect(skillSwitch(project, true)).toEqual({ on: true, level: 'project' })
    expect(skillSwitch(project, false)).toBeNull()
  })

  test('a skill turned off at the level the switch writes comes back from it', () => {
    expect(skillSwitch(off(['user']), true)).toEqual({ on: false, level: 'user' })
    expect(skillSwitch(off(['project'], { location: 'project' } as Partial<Skill>), true)).toEqual({
      on: false,
      level: 'project',
    })
  })

  test('a skill turned off elsewhere is read-only', () => {
    expect(skillSwitch(off(['project']), true)).toBeNull()
    expect(skillSwitch(off(['user', 'project']), true)).toBeNull()
    expect(skillSwitch(off(['org']), true)).toBeNull()
    expect(
      skillSwitch(
        skill({ enabled: false, disabledBy: { kind: 'frontmatter' } } as Partial<Skill>),
        true,
      ),
    ).toBeNull()
  })

  test('personal skills have no switch', () => {
    expect(skillSwitch(skill({ location: 'personal' } as Partial<Skill>), true)).toBeNull()
  })
})

describe('disabledLabel', () => {
  test('names who turned the skill off', () => {
    expect(disabledLabel(off(['org']))).toBe('Disabled by organization')
    expect(disabledLabel(off(['project']))).toBe('Disabled for this project')
    expect(disabledLabel(off(['user']))).toBe('Disabled')
    expect(disabledLabel(off(['user', 'project']))).toBe('Disabled')
    expect(
      disabledLabel(
        skill({ enabled: false, disabledBy: { kind: 'frontmatter' } } as Partial<Skill>),
      ),
    ).toBe('Disabled in its file')
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
