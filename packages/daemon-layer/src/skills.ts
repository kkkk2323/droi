// Skills as the Daemon lists them for a Session, and the switches a Client may
// offer on each. The Daemon owns the state: a switch writes `disabledSkills`
// to `~/.factory/settings.json` (user level, every project) or to the
// Workspace's `.factory/settings.json` (project level); the droid CLI and the
// Factory App read the same files. The rules for which switches exist follow
// the Factory App: a project skill can be turned off for its project, a
// built-in one across all projects, a personal skill (`~/.factory/skills`) is
// managed by editing its files, and one the organization turned off is read-only.
import type { DaemonSessionController } from '@factory/droid-sdk'

export type Skill = Awaited<ReturnType<DaemonSessionController['listSkills']>>['skills'][number]
export type SkillLocation = Skill['location']
/** Where a switch is written. */
export type SkillLevel = 'user' | 'project'

export interface SkillSwitch {
  disabled: boolean
  level: SkillLevel
  label: string
}

export const LOCATION_LABELS: Record<string, string> = {
  project: 'Project',
  personal: 'Personal',
  builtin: 'Built-in',
  automation: 'Automation',
}

export const LOCATION_ORDER: readonly string[] = ['project', 'personal', 'builtin', 'automation']

function disabledAt(skill: Skill, level: string): boolean {
  return (
    skill.disabledBy?.kind === 'ledger' &&
    skill.disabledBy.sources.some((source) => String(source.level) === level)
  )
}

export function isDisabledByOrg(skill: Skill): boolean {
  return disabledAt(skill, 'org')
}

/** The switches for a skill; empty when nothing here can change it. */
export function skillSwitches(skill: Skill, projectAvailable: boolean): SkillSwitch[] {
  const project = String(skill.location) === 'project'
  const builtin = String(skill.location) === 'builtin'
  if (!project && !builtin) return []
  const switches: Array<Omit<SkillSwitch, 'label'>> = []
  if (skill.enabled !== false) {
    if (projectAvailable) switches.push({ disabled: true, level: 'project' })
    if (builtin) switches.push({ disabled: true, level: 'user' })
  } else if (skill.disabledBy?.kind === 'ledger') {
    if (projectAvailable && disabledAt(skill, 'project')) {
      switches.push({ disabled: false, level: 'project' })
    }
    if (builtin && disabledAt(skill, 'user')) switches.push({ disabled: false, level: 'user' })
  }
  const several = switches.length > 1
  return switches.map((s) => ({
    ...s,
    label:
      (s.disabled ? 'Disable' : 'Enable') +
      (s.level === 'project' ? ' for this project' : several ? ' across all projects' : ''),
  }))
}

/** The skills grouped by where they live, in a fixed order, each group sorted by name. */
export function groupSkills(
  skills: readonly Skill[],
): Array<{ location: string; skills: Skill[] }> {
  const groups = new Map<string, Skill[]>()
  for (const skill of skills) {
    const location = String(skill.location)
    const group = groups.get(location)
    if (group) group.push(skill)
    else groups.set(location, [skill])
  }
  const order = (location: string) => {
    const index = LOCATION_ORDER.indexOf(location)
    return index === -1 ? LOCATION_ORDER.length : index
  }
  return [...groups.entries()]
    .sort(([a], [b]) => order(a) - order(b) || a.localeCompare(b))
    .map(([location, list]) => ({
      location,
      skills: [...list].sort((a, b) => a.name.localeCompare(b.name)),
    }))
}
