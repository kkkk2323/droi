// The Session's skills grouped by where they live, each with the switches the
// Daemon allows (skills.ts has the rules), laid out as buttons under the row.
import {
  LOCATION_LABELS,
  groupSkills,
  isDisabledByOrg,
  skillSwitches,
  type Skill,
  type SkillLevel,
} from '@droi/daemon-layer/skills'
import { useSkillActions, useSkills } from '@droi/daemon-layer/use-skills'
import { Pressable, StyleSheet, View } from 'react-native'
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
  const switches = skillSwitches(skill, projectAvailable)
  return (
    <View role="listitem" aria-label={skill.name} style={[styles.row, off ? styles.off : null]}>
      <View style={styles.titleRow}>
        <Text weight="medium">{skill.name}</Text>
        {off ? (
          <Badge>{isDisabledByOrg(skill) ? 'Disabled by organization' : 'Disabled'}</Badge>
        ) : null}
        {skill.userInvocable === false ? <Badge>Model only</Badge> : null}
        {saving ? <Spinner size={12} color={colors.mutedForeground} /> : null}
      </View>
      {skill.description ? (
        <Text size="sm" tone="muted" numberOfLines={2}>
          {skill.description}
        </Text>
      ) : null}
      {switches.length > 0 ? (
        <View style={styles.switches}>
          {switches.map((s) => (
            <Pressable
              key={s.level}
              role="button"
              disabled={saving}
              onPress={() => onSwitch(s.disabled, s.level)}
              style={({ pressed }) => [
                styles.switchButton,
                {
                  borderColor: colors.border,
                  backgroundColor: pressed ? colors.accent : colors.card,
                },
              ]}
            >
              <Text size="xs" weight="medium">
                {s.label}
              </Text>
            </Pressable>
          ))}
        </View>
      ) : null}
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
  row: { gap: 2, paddingVertical: space.xs },
  off: { opacity: 0.7 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: space.xs, flexWrap: 'wrap' },
  switches: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs, marginTop: space.xs },
  switchButton: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
    paddingHorizontal: space.sm,
    height: 28,
    justifyContent: 'center',
  },
  badge: { borderRadius: radius.sm, paddingHorizontal: 6, paddingVertical: 1 },
  loading: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
})
