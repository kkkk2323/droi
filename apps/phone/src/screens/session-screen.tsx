// An open Session. Loading it through useSession subscribes this phone to
// its notifications, which is also what makes its activity show in the list.
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { useSession } from '@droi/daemon-layer/use-session'
import { StyleSheet, View } from 'react-native'
import { ScreenHeader } from '../ui/screen-header'
import { Text } from '../ui/primitives'
import { space } from '../ui/theme'

export function SessionScreen({
  session,
  drawerOpen,
  onOpenDrawer,
}: {
  session: SessionSummary
  drawerOpen: boolean
  onOpenDrawer: () => void
}) {
  const view = useSession(session.sessionId)
  return (
    <View style={styles.fill}>
      <ScreenHeader title={session.title} drawerOpen={drawerOpen} onOpenDrawer={onOpenDrawer} />
      <View style={styles.body}>
        {view.loadError ? (
          <Text role="alert" tone="muted">
            {view.loadError}
          </Text>
        ) : null}
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  body: { flex: 1, padding: space.lg },
})
