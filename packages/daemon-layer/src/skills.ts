// Skills as the computer's Kernel lists them for a Session (`skills.list`): the user's own
// (`personal`) and those of the Session's Workspace and its parents (`workspace`). Turning one
// off is the Kernel's setting, kept in its database: it applies wherever a skill of that name
// is found, for every Session.

export interface Skill {
  name: string
  scope: 'personal' | 'workspace'
  /** Where the SKILL.md is on the computer; shown, never read here. */
  path: string
  description?: string
  enabled: boolean
}

export const SCOPE_LABELS: Record<string, string> = {
  workspace: 'Project',
  personal: 'Personal',
}

export const SCOPE_ORDER: readonly string[] = ['workspace', 'personal']

/** The skills grouped by where they live, in a fixed order, each group sorted by name. */
export function groupSkills(skills: readonly Skill[]): Array<{ scope: string; skills: Skill[] }> {
  const groups = new Map<string, Skill[]>()
  for (const skill of skills) {
    const group = groups.get(skill.scope)
    if (group) group.push(skill)
    else groups.set(skill.scope, [skill])
  }
  const order = (scope: string) => {
    const index = SCOPE_ORDER.indexOf(scope)
    return index === -1 ? SCOPE_ORDER.length : index
  }
  return [...groups.entries()]
    .sort(([a], [b]) => order(a) - order(b) || a.localeCompare(b))
    .map(([scope, list]) => ({
      scope,
      skills: [...list].sort((a, b) => a.name.localeCompare(b.name)),
    }))
}
