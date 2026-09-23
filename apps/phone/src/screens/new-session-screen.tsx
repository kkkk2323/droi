// Home: a new Session. Picking a Workspace and the composer come with the
// New session page; until then this is where the app lands.
import { StyleSheet, View } from 'react-native'
import { ScreenHeader } from '../ui/screen-header'

export function NewSessionScreen({
  drawerOpen,
  onOpenDrawer,
}: {
  drawerOpen: boolean
  onOpenDrawer: () => void
}) {
  return (
    <View style={styles.fill}>
      <ScreenHeader title="New session" drawerOpen={drawerOpen} onOpenDrawer={onOpenDrawer} />
    </View>
  )
}

const styles = StyleSheet.create({ fill: { flex: 1 } })
