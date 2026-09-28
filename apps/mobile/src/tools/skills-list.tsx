// The Session's skills grouped by where they live, each with the switch the
// Daemon allows (skills.ts has the rules).
import {
  LOCATION_LABELS,
  disabledLabel,
  groupSkills,
  skillSwitch,
  type Skill,
  type SkillLevel,
} from '@droi/daemon-layer/skills'
import { useSkillActions, useSkills } from '@droi/daemon-layer/use-skills'
import { StyleSheet, Switch, View } from 'react-native'
import { Spinner } from '../ui/activity'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SkillsList({ sessionId }: { sessionId: string }) {
  const view = useSkills(sessionId)
  const actions = useSkillActions(sessionId)
  if (view.isLoading) return <Loading what="skills" />
  if (view.error) return <Failure message={view.error} />
  if (view.skills.length === 0) return <Text tone="muted">No skills are installed.</Text>
  return (
    <>
      {actions.error ? <Failure message={actions.error} /> : null}
      {groupSkills(view.skills).map((group) => (
        <View
          key={group.location}
          role="group"
          aria-label={LOCATION_LABELS[group.location] ?? group.location}
          style={styles.group}
        >
          <Text size="xs" weight="medium" tone="muted">
            {LOCATION_LABELS[group.location] ?? group.location}
          </Text>
          {group.skills.map((skill) => (
            <SkillRow
              key={`${skill.location}:${skill.name}`}
              skill={skill}
              projectAvailable={view.projectAvailable}
              saving={actions.saving === skill.name}
              onSwitch={(disabled, level) => void actions.setDisabled(skill.name, disabled, level)}
            />
          ))}
        </View>
      ))}
    </>
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
  const colors = useColors()
  const off = skill.enabled === false
  const toggle = skillSwitch(skill, projectAvailable)
  // Personal skills have no switch at all; the rest show one, locked when
  // something other than this switch turned the skill off.
  const personal = String(skill.location) === 'personal'
  return (
    <View role="listitem" aria-label={skill.name} style={[styles.row, off ? styles.off : null]}>
      <View style={styles.body}>
        <View style={styles.titleRow}>
          <Text weight="medium">{skill.name}</Text>
          {off ? <Badge>{disabledLabel(skill)}</Badge> : null}
          {skill.userInvocable === false ? <Badge>Model only</Badge> : null}
          {saving ? <Spinner size={12} color={colors.mutedForeground} /> : null}
        </View>
        {skill.description ? (
          <Text size="sm" tone="muted" numberOfLines={2}>
            {skill.description}
          </Text>
        ) : null}
      </View>
      {personal ? null : (
        <Switch
          aria-label={`${skill.name} enabled`}
          value={!off}
          disabled={!toggle || saving}
          onValueChange={(checked) => {
            if (toggle) onSwitch(!checked, toggle.level)
          }}
          trackColor={{ true: colors.primary }}
        />
      )}
    </View>
  )
}

export function Badge({ children }: { children: string }) {
  const colors = useColors()
  return (
    <View style={[styles.badge, { backgroundColor: colors.muted }]}>
      <Text size="xs" tone="muted">
        {children}
      </Text>
    </View>
  )
}

export function Loading({ what }: { what: string }) {
  const colors = useColors()
  return (
    <View style={styles.loading}>
      <Spinner size={14} color={colors.mutedForeground} />
      <Text tone="muted" size="sm">
        Loading {what}…
      </Text>
    </View>
  )
}

export function Failure({ message }: { message: string }) {
  const colors = useColors()
  return (
    <Text role="alert" size="sm" style={{ color: colors.destructiveForeground }}>
      {message}
    </Text>
  )
}

const styles = StyleSheet.create({
  group: { gap: space.xs },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.sm, paddingVertical: space.xs },
  body: { flex: 1, gap: 2 },
  off: { opacity: 0.7 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: space.xs, flexWrap: 'wrap' },
  badge: { borderRadius: radius.sm, paddingHorizontal: 6, paddingVertical: 1 },
  loading: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
})
