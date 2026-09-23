// A sheet for pickers and action menus: the phone's stand-in for the web
// Client's popovers and selects. It is the platform's own bottom sheet (SwiftUI
// on iOS, vaul in the web build), so the dim fades in place while only the
// panel slides, and a swipe down or a tap outside dismisses it.
import { BottomSheetModal, BottomSheetView } from '@expo/ui/community/bottom-sheet'
import { useEffect, useRef, type ReactNode } from 'react'
import { Pressable, StyleSheet, View, useWindowDimensions } from 'react-native'
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
  const { height } = useWindowDimensions()
  const sheet = useRef<BottomSheetModal>(null)
  // The native sheet reports its dismissal only after the slide ends; by then
  // the caller may have opened something else that this must not close.
  const open = useRef(visible)

  useEffect(() => {
    open.current = visible
    if (visible) sheet.current?.present()
    else sheet.current?.dismiss()
  }, [visible])

  return (
    <BottomSheetModal
      ref={sheet}
      enablePanDownToClose
      backgroundStyle={{ backgroundColor: colors.popover }}
      onDismiss={() => {
        if (open.current) onClose()
      }}
    >
      <BottomSheetView>
        <View
          role="dialog"
          aria-label={title}
          style={{ maxHeight: Math.round(height * 0.85), paddingBottom: insets.bottom + space.md }}
        >
          <Text role="heading" weight="semibold" style={styles.title}>
            {title}
          </Text>
          {children}
        </View>
      </BottomSheetView>
    </BottomSheetModal>
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
  title: { paddingHorizontal: space.lg, paddingBottom: space.sm },
  option: {
    minHeight: 44,
    justifyContent: 'center',
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
    borderRadius: radius.lg,
  },
})
