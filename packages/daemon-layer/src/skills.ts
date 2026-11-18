// Skills as the Daemon lists them for a Session, and the one switch a Client
// may offer on each. The Daemon owns the state: a switch writes
// `disabledSkills` to `~/.factory/settings.json` (user level, every project)
// or to the Workspace's `.factory/settings.json` (project level); the droid
// CLI and the Factory App read the same files. Which skills get a switch
// follows the Factory App: a project skill and a built-in one do, a personal
// skill (`~/.factory/skills`) is managed by editing its files, and one the
// organization turned off is read-only. Where the switch writes is one rule:
// a project skill to its project, a built-in one for the user.
import type { DaemonSessionController } from '@factory/droid-sdk'

export type Skill = Awaited<ReturnType<DaemonSessionController['listSkills']>>['skills'][number]
export type SkillLocation = Skill['location']
/** Where a switch is written. */
export type SkillLevel = 'user' | 'project'

export interface SkillSwitch {
  /** Whether the skill is on now; flipping writes `disabled: on` at `level`. */
  on: boolean
  level: SkillLevel
}

export const LOCATION_LABELS: Record<string, string> = {
  project: 'Project',
  personal: 'Personal',
  builtin: 'Built-in',
  automation: 'Automation',
}

export const LOCATION_ORDER: readonly string[] = ['project', 'personal', 'builtin', 'automation']

function disabledLevels(skill: Skill): string[] {
  return skill.disabledBy?.kind === 'ledger'
    ? skill.disabledBy.sources.map((source) => String(source.level))
    : []
}

/**
 * The switch for a skill, or null when nothing here can change it: a personal
 * skill, one with no Workspace to write a project switch into, one turned off
 * in its own file, and one turned off at a level this switch does not write
 * (the organization's, or the other of user and project).
 */
export function skillSwitch(skill: Skill, projectAvailable: boolean): SkillSwitch | null {
  const location = String(skill.location)
  const level: SkillLevel | null =
    location === 'project'
      ? projectAvailable
        ? 'project'
        : null
      : location === 'builtin'
        ? 'user'
        : null
  if (!level) return null
  if (skill.enabled !== false) return { on: true, level }
  const levels = disabledLevels(skill)
  if (levels.length === 0 || levels.some((l) => l !== level)) return null
  return { on: false, level }
}

/** Why a skill is off, for its badge. */
export function disabledLabel(skill: Skill): string {
  const levels = disabledLevels(skill)
  if (levels.includes('org')) return 'Disabled by organization'
  if (skill.disabledBy?.kind === 'frontmatter') return 'Disabled in its file'
  if (levels.length > 0 && levels.every((l) => l === 'project')) return 'Disabled for this project'
  return 'Disabled'
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
