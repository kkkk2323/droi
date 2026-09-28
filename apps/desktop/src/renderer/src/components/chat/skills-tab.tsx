// The skills the Session can use, grouped by where they live, each with the
// switches the Daemon allows (skills.ts has the rules).
import { Menu } from '@base-ui/react/menu'
import { ChevronDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import {
  LOCATION_LABELS,
  groupSkills,
  isDisabledByOrg,
  skillSwitches,
  type Skill,
  type SkillLevel,
} from '@droi/daemon-layer/skills'
import { useSkillActions, useSkills } from '@droi/daemon-layer/use-skills'
import { cn } from '@/lib/utils'

export function SkillsTab({ sessionId }: { sessionId: string }) {
  const view = useSkills(sessionId)
  const actions = useSkillActions(sessionId)
  if (view.isLoading) return <Loading what="skills" />
  if (view.error) return <Failure message={view.error} />
  if (view.skills.length === 0) {
    return <p className="p-2 text-sm text-muted-foreground">No skills are installed.</p>
  }
  return (
    <div className="flex flex-col gap-4">
      {actions.error ? <Failure message={actions.error} /> : null}
      {groupSkills(view.skills).map((group) => (
        <section
          key={group.location}
          aria-label={LOCATION_LABELS[group.location] ?? group.location}
        >
          <h3 className="px-2 pb-1 text-xs font-medium text-muted-foreground">
            {LOCATION_LABELS[group.location] ?? group.location}
          </h3>
          <ul className="flex flex-col gap-0.5">
            {group.skills.map((skill) => (
              <SkillRow
                key={`${skill.location}:${skill.name}`}
                skill={skill}
                projectAvailable={view.projectAvailable}
                saving={actions.saving === skill.name}
                onSwitch={(disabled, level) =>
                  void actions.setDisabled(skill.name, disabled, level)
                }
              />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}

function SkillRow({
  skill,
  projectAvailable,
  saving,
  onSwitch,
}: {
  skill: Skill
  projectAvailable: boolean
  saving: boolean
  onSwitch: (disabled: boolean, level: SkillLevel) => void
}) {
  const off = skill.enabled === false
  const switches = skillSwitches(skill, projectAvailable)
  return (
    <li
      aria-label={skill.name}
      className={cn('flex items-start gap-3 rounded-lg px-2 py-1.5', off && 'opacity-70')}
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-sm font-medium">{skill.name}</span>
          {off ? (
            <Badge>{isDisabledByOrg(skill) ? 'Disabled by organization' : 'Disabled'}</Badge>
          ) : null}
          {skill.userInvocable === false ? <Badge>Model only</Badge> : null}
        </div>
        {skill.description ? (
          <p className="mt-0.5 line-clamp-2 text-[13px] leading-5 text-muted-foreground">
            {skill.description}
          </p>
        ) : null}
      </div>
      {switches.length > 0 ? (
        <Menu.Root>
          <Menu.Trigger
            aria-label={`Manage ${skill.name}`}
            disabled={saving}
            render={<Button size="xs" variant="outline" />}
          >
            {saving ? <Spinner className="size-3" /> : null}
            {off ? 'Disabled' : 'Enabled'}
            <ChevronDown aria-hidden data-icon="inline-end" />
          </Menu.Trigger>
          <Menu.Portal>
            <Menu.Positioner side="bottom" align="end" sideOffset={4} className="z-50 outline-none">
              <Menu.Popup className="min-w-48 rounded-lg border bg-popover p-1 text-sm text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none">
                {switches.map((s) => (
                  <Menu.Item
                    key={s.level}
                    onClick={() => onSwitch(s.disabled, s.level)}
                    className="rounded-md px-2 py-1.5 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
                  >
                    {s.label}
                  </Menu.Item>
                ))}
              </Menu.Popup>
            </Menu.Positioner>
          </Menu.Portal>
        </Menu.Root>
      ) : null}
    </li>
  )
}

export function Badge({ children }: { children: string }) {
  return (
    <span className="rounded-md bg-muted px-1.5 py-px text-[11px] text-muted-foreground">
      {children}
    </span>
  )
}

export function Loading({ what }: { what: string }) {
  return (
    <p className="flex items-center gap-2 p-2 text-sm text-muted-foreground">
      <Spinner className="size-3.5" />
      Loading {what}…
    </p>
  )
}

export function Failure({ message }: { message: string }) {
  return (
    <p role="alert" className="rounded-md bg-destructive/10 px-2 py-1.5 text-sm text-destructive">
      {message}
    </p>
  )
}
