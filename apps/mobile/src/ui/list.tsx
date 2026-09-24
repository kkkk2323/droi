// Grouped rows in the style of iOS settings, drawn with Droi's tokens.
import { Check, ChevronRight } from 'lucide-react-native'
import { Children, Fragment, isValidElement, useState, type ReactNode } from 'react'
import { Pressable, ScrollView, StyleSheet, Switch, View } from 'react-native'
import { Text } from './primitives'
import { Sheet, SheetOption } from './sheet'
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
  disabled = false,
}: {
  label: string
  value: boolean
  onChange: (value: boolean) => void
  disabled?: boolean
}) {
  const colors = useColors()
  return (
    <View style={styles.row}>
      <Text style={styles.switchLabel}>{label}</Text>
      <Switch
        aria-label={label}
        value={value}
        onValueChange={onChange}
        disabled={disabled}
        trackColor={{ true: colors.primary, false: colors.input }}
      />
    </View>
  )
}

/**
 * A row showing the current choice that opens a sheet of all of them: the
 * phone's stand-in for the web Client's settings selects. Without `onChange`
 * (a setting the organization manages) it only shows the value.
 */
export function ListPicker({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: ReadonlyArray<{ value: string; label: string }>
  value: string
  onChange?: (value: string) => void
}) {
  const [open, setOpen] = useState(false)
  const current = options.find((o) => o.value === value)?.label ?? value
  return (
    <>
      <ListRow
        label={label}
        value={current}
        onPress={onChange && options.length > 0 ? () => setOpen(true) : undefined}
      />
      <Sheet visible={open} title={label} onClose={() => setOpen(false)}>
        <ScrollView role="radiogroup" aria-label={label} style={styles.sheetList}>
          {options.map((option) => (
            <SheetOption
              key={option.value}
              label={option.label}
              checked={option.value === value}
              onPress={() => {
                setOpen(false)
                if (option.value !== value) onChange?.(option.value)
              }}
            />
          ))}
        </ScrollView>
      </Sheet>
    </>
  )
}

/** A section whose rows are one choice among several, the picked one ticked. */
export function ListChoices<T extends string>({
  title,
  options,
  value,
  onChange,
}: {
  title: string
  options: ReadonlyArray<{ value: T; label: string }>
  value: T
  onChange: (value: T) => void
}) {
  const colors = useColors()
  return (
    <View role="radiogroup" aria-label={title}>
      <ListSection title={title}>
        {options.map((option) => (
          <Pressable
            key={option.value}
            role="radio"
            aria-checked={option.value === value}
            aria-label={option.label}
            onPress={() => onChange(option.value)}
            style={({ pressed }) => [
              styles.row,
              pressed ? { backgroundColor: colors.accent } : null,
            ]}
          >
            <Text style={styles.switchLabel}>{option.label}</Text>
            {option.value === value ? (
              <Check size={16} color={colors.foreground} strokeWidth={2} />
            ) : null}
          </Pressable>
        ))}
      </ListSection>
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
  sheetList: { flexGrow: 0 },
  value: { flex: 1, textAlign: 'right' },
})
