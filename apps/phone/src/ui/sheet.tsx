// A sheet that slides up from the bottom over a dimmed screen: the phone's
// stand-in for the web Client's popovers and selects.
import type { ReactNode } from 'react'
import { Modal, Pressable, StyleSheet, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { Text } from './primitives'
import { radius, space } from './theme'
import { useColors } from './use-colors'

export function Sheet({
  visible,
  title,
  onClose,
  children,
}: {
  visible: boolean
  title: string
  onClose: () => void
  children: ReactNode
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <View style={styles.root}>
        <Pressable
          aria-label="Close"
          style={[StyleSheet.absoluteFill, { backgroundColor: colors.backdrop }]}
          onPress={onClose}
        />
        <View
          role="dialog"
          aria-label={title}
          style={[
            styles.sheet,
            { backgroundColor: colors.popover, paddingBottom: insets.bottom + space.md },
          ]}
        >
          <View style={[styles.grabber, { backgroundColor: colors.border }]} />
          <Text role="heading" weight="semibold" style={styles.title}>
            {title}
          </Text>
          {children}
        </View>
      </View>
    </Modal>
  )
}

/** One choice in a sheet: a radio row. */
export function SheetOption({
  label,
  checked,
  onPress,
}: {
  label: string
  checked: boolean
  onPress: () => void
}) {
  const colors = useColors()
  return (
    <Pressable
      role="radio"
      aria-checked={checked}
      aria-label={label}
      onPress={onPress}
      style={({ pressed }) => [
        styles.option,
        { backgroundColor: pressed || checked ? colors.accent : undefined },
      ]}
    >
      <Text weight={checked ? 'medium' : 'regular'}>{label}</Text>
    </Pressable>
  )
}

const styles = StyleSheet.create({
  root: { flex: 1, justifyContent: 'flex-end' },
  sheet: {
    borderTopLeftRadius: radius.xl + 6,
    borderTopRightRadius: radius.xl + 6,
    paddingTop: space.sm,
    maxHeight: '85%',
  },
  grabber: { alignSelf: 'center', width: 36, height: 4, borderRadius: 2, marginBottom: space.sm },
  title: { paddingHorizontal: space.lg, paddingBottom: space.sm },
  option: {
    minHeight: 44,
    justifyContent: 'center',
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
    borderRadius: radius.lg,
  },
})
