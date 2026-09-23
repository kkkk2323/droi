// Grouped rows in the style of iOS settings, drawn with Droi's tokens.
import { ChevronRight } from 'lucide-react-native'
import { Children, Fragment, isValidElement, type ReactNode } from 'react'
import { Pressable, StyleSheet, Switch, View } from 'react-native'
import { Text } from './primitives'
import { radius, space } from './theme'
import { useColors } from './use-colors'

export function ListSection({
  title,
  footer,
  children,
}: {
  title?: string
  footer?: string
  children: ReactNode
}) {
  const colors = useColors()
  const rows = Children.toArray(children).filter(Boolean)
  return (
    <View style={styles.section}>
      {title ? (
        <Text role="heading" tone="muted" size="sm" weight="medium" style={styles.title}>
          {title}
        </Text>
      ) : null}
      <View style={[styles.group, { backgroundColor: colors.card }]}>
        {rows.map((row, index) => (
          <Fragment key={isValidElement(row) ? row.key : index}>
            {index > 0 ? (
              <View style={[styles.divider, { backgroundColor: colors.border }]} />
            ) : null}
            {row}
          </Fragment>
        ))}
      </View>
      {footer ? (
        <Text tone="muted" size="sm" style={styles.footer}>
          {footer}
        </Text>
      ) : null}
    </View>
  )
}

/** A row with a label and a value; pressable rows get a chevron. */
export function ListRow({
  label,
  value,
  onPress,
  destructive = false,
}: {
  label: string
  value?: string
  onPress?: () => void
  destructive?: boolean
}) {
  const colors = useColors()
  const content = (
    <>
      <Text
        style={[styles.label, destructive ? { color: colors.destructiveForeground } : null]}
        numberOfLines={1}
      >
        {label}
      </Text>
      {value !== undefined ? (
        <Text tone="muted" numberOfLines={1} style={styles.value}>
          {value}
        </Text>
      ) : null}
      {onPress && !destructive ? (
        <ChevronRight size={16} color={colors.mutedForeground} strokeWidth={1.75} />
      ) : null}
    </>
  )
  if (!onPress) {
    return (
      <View style={styles.row} accessible aria-label={value ? `${label}: ${value}` : label}>
        {content}
      </View>
    )
  }
  return (
    <Pressable
      role="button"
      aria-label={value ? `${label}: ${value}` : label}
      onPress={onPress}
      style={({ pressed }) => [styles.row, pressed ? { backgroundColor: colors.accent } : null]}
    >
      {content}
    </Pressable>
  )
}

/** A row with an on/off switch. */
export function ListSwitch({
  label,
  value,
  onChange,
}: {
  label: string
  value: boolean
  onChange: (value: boolean) => void
}) {
  const colors = useColors()
  return (
    <View style={styles.row}>
      <Text style={styles.switchLabel}>{label}</Text>
      <Switch
        aria-label={label}
        value={value}
        onValueChange={onChange}
        trackColor={{ true: colors.primary, false: colors.input }}
      />
    </View>
  )
}

const styles = StyleSheet.create({
  switchLabel: { flex: 1 },
  section: { gap: space.sm },
  title: { paddingHorizontal: space.xs },
  group: { borderRadius: radius.lg, overflow: 'hidden' },
  divider: { height: StyleSheet.hairlineWidth, marginLeft: space.lg },
  footer: { paddingHorizontal: space.xs },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.lg,
    minHeight: 44,
  },
  label: { flexShrink: 0 },
  value: { flex: 1, textAlign: 'right' },
})
