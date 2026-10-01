// The Session list's title: the connected computer and how its connection
// stands, which opens a menu of the other Paired Computers.
import { useConnectionState } from '@droi/daemon-layer/connection-context'
import { ChevronDown, Plus } from 'lucide-react-native'
import { Pressable, StyleSheet, View } from 'react-native'
import { selectedComputerId, type PairedComputer } from '../computers/store'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function ComputerTitle({
  computer,
  open,
  onToggle,
}: {
  computer: PairedComputer
  open: boolean
  onToggle: () => void
}) {
  const colors = useColors()
  return (
    <Pressable
      role="button"
      aria-label={`Switch computer, ${computer.name}`}
      aria-expanded={open}
      onPress={onToggle}
      style={styles.title}
    >
      <View style={styles.name}>
        <Text role="heading" weight="semibold" numberOfLines={1} style={styles.shrink}>
          {computer.name}
        </Text>
        <ChevronDown size={14} color={colors.mutedForeground} strokeWidth={2} />
      </View>
      <ConnectionLine />
    </Pressable>
  )
}

export function ComputerMenu({
  computer,
  computers,
  onAddComputer,
}: {
  computer: PairedComputer
  computers: readonly PairedComputer[]
  onAddComputer: () => void
}) {
  const colors = useColors()
  const rowStyle = ({ pressed }: { pressed: boolean }) => [
    styles.row,
    pressed ? { backgroundColor: colors.accent } : null,
  ]
  return (
    <View
      role="menu"
      aria-label="Computers"
      style={[styles.menu, { borderBottomColor: colors.border }]}
    >
      {computers
        .filter((c) => c.id !== computer.id)
        .map((c) => (
          <Pressable
            key={c.id}
            role="menuitem"
            onPress={() => selectedComputerId.set(c.id)}
            style={rowStyle}
          >
            <Text numberOfLines={1}>{c.name}</Text>
          </Pressable>
        ))}
      <Pressable role="menuitem" onPress={onAddComputer} style={rowStyle}>
        <Plus size={16} color={colors.mutedForeground} strokeWidth={1.75} />
        <Text>Add a computer</Text>
      </Pressable>
    </View>
  )
}

function ConnectionLine() {
  const state = useConnectionState()
  const colors = useColors()
  const label =
    state.status === 'connected'
      ? 'Connected'
      : state.status === 'connecting'
        ? 'Connecting…'
        : state.status === 'reconnecting'
          ? 'Reconnecting…'
          : state.status === 'unpaired'
            ? 'Not paired'
            : 'Not reachable'
  return (
    <Text
      role="status"
      aria-label="Connection"
      size="xs"
      style={{ color: state.status === 'connected' ? colors.mutedForeground : colors.attention }}
    >
      {label}
    </Text>
  )
}

const styles = StyleSheet.create({
  title: { alignItems: 'center', maxWidth: 240 },
  name: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  shrink: { flexShrink: 1 },
  menu: {
    paddingHorizontal: space.sm,
    paddingBottom: space.sm,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 44,
    paddingHorizontal: space.sm,
    borderRadius: radius.lg,
  },
})
