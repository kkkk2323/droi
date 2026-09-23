// The connected screen: the open Session (or New session) fills the phone,
// and the Session list slides in from the left.
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { useRouter } from 'expo-router'
import { useState } from 'react'
import { StyleSheet, View } from 'react-native'
import { Drawer } from 'react-native-drawer-layout'
import type { PairedComputer } from '../computers/store'
import { SessionList } from '../sessions/session-list'
import { useColors } from '../ui/use-colors'
import { NewSessionScreen } from './new-session-screen'
import { SessionScreen } from './session-screen'

export function MainScreen({ computer }: { computer: PairedComputer }) {
  const colors = useColors()
  const router = useRouter()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [selected, setSelected] = useState<SessionSummary | null>(null)
  const openDrawer = () => setDrawerOpen(true)

  return (
    <Drawer
      open={drawerOpen}
      onOpen={openDrawer}
      onClose={() => setDrawerOpen(false)}
      drawerType="front"
      drawerStyle={{ width: '85%', maxWidth: 320, backgroundColor: colors.sidebar }}
      overlayStyle={{ backgroundColor: colors.backdrop }}
      overlayAccessibilityLabel="Close sessions"
      renderDrawerContent={() => (
        <View role="dialog" aria-label="Sessions" aria-hidden={!drawerOpen} style={styles.fill}>
          <SessionList
            computerName={computer.name}
            selectedId={selected?.sessionId ?? null}
            onSelect={(session) => {
              setSelected(session)
              setDrawerOpen(false)
            }}
            onNewSession={() => {
              setSelected(null)
              setDrawerOpen(false)
            }}
            onSettings={() => {
              setDrawerOpen(false)
              router.push('/settings')
            }}
          />
        </View>
      )}
    >
      <View style={[styles.fill, { backgroundColor: colors.background }]}>
        {selected ? (
          <SessionScreen
            key={selected.sessionId}
            session={selected}
            drawerOpen={drawerOpen}
            onOpenDrawer={openDrawer}
          />
        ) : (
          <NewSessionScreen drawerOpen={drawerOpen} onOpenDrawer={openDrawer} />
        )}
      </View>
    </Drawer>
  )
}

const styles = StyleSheet.create({ fill: { flex: 1 } })
