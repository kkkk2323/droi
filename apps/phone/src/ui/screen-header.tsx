// The main screen's top bar: the drawer button, a title and optional detail.
import { PanelLeft } from 'lucide-react-native'
import type { ReactNode } from 'react'
import { StyleSheet, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { IconButton, Text } from './primitives'
import { space } from './theme'
import { useColors } from './use-colors'

export function ScreenHeader({
  title,
  drawerOpen,
  onOpenDrawer,
  trailing,
}: {
  title: string
  drawerOpen: boolean
  onOpenDrawer: () => void
  /** Controls at the right end of the bar. */
  trailing?: ReactNode
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  return (
    <View
      style={[
        styles.bar,
        {
          paddingTop: insets.top,
          borderBottomColor: colors.border,
          backgroundColor: colors.background,
        },
      ]}
    >
      <View style={styles.row}>
        <IconButton
          label="Open sessions"
          icon={PanelLeft}
          expanded={drawerOpen}
          onPress={onOpenDrawer}
        />
        <Text role="heading" weight="semibold" numberOfLines={1} style={styles.title}>
          {title}
        </Text>
        {trailing}
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  bar: { borderBottomWidth: StyleSheet.hairlineWidth },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    paddingHorizontal: space.sm,
    height: 44,
  },
  title: { flex: 1 },
})
