// The Session's skills grouped by where they live: the project's (the Session's
// folder and its parents) and the user's own. Turning one off is the Kernel's
// setting, for every Session.
import { SCOPE_LABELS, groupSkills, type Skill } from '@droi/daemon-layer/skills'
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
          key={group.scope}
          role="group"
          aria-label={SCOPE_LABELS[group.scope] ?? group.scope}
          style={styles.group}
        >
          <Text size="xs" weight="medium" tone="muted">
            {SCOPE_LABELS[group.scope] ?? group.scope}
          </Text>
          {group.skills.map((skill) => (
            <SkillRow
              key={`${skill.scope}:${skill.name}`}
              skill={skill}
              saving={actions.saving === skill.name}
              onSwitch={(disabled) => void actions.setDisabled(skill.name, disabled)}
            />
          ))}
        </View>
      ))}
    </>
  )
}

function SkillRow({
  skill,
  saving,
  onSwitch,
}: {
  skill: Skill
  saving: boolean
  onSwitch: (disabled: boolean) => void
}) {
  const colors = useColors()
  const off = !skill.enabled
  return (
    <View role="listitem" aria-label={skill.name} style={[styles.row, off ? styles.off : null]}>
      <View style={styles.body}>
        <View style={styles.titleRow}>
          <Text weight="medium">{skill.name}</Text>
          {off ? <Badge>Disabled</Badge> : null}
          {saving ? <Spinner size={12} color={colors.mutedForeground} /> : null}
        </View>
        {skill.description ? (
          <Text size="sm" tone="muted" numberOfLines={2}>
            {skill.description}
          </Text>
        ) : null}
      </View>
      <Switch
        aria-label={`${skill.name} enabled`}
        value={!off}
        disabled={saving}
        onValueChange={(checked) => onSwitch(!checked)}
        trackColor={{ true: colors.primary }}
      />
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
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.md,
    minHeight: 52,
    paddingVertical: space.sm,
  },
  body: { flex: 1, gap: space.xs },
  off: { opacity: 0.7 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: space.xs, flexWrap: 'wrap' },
  badge: { borderRadius: radius.sm, paddingHorizontal: 6, paddingVertical: 1 },
  loading: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
})
