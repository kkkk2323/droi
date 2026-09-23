// The connected screen: the open Session (or New session) fills the phone,
// and the Session list slides in from the left. The open Session is kept per
// Paired Computer, so the app reopens where it was.
import { usePreference } from '@droi/daemon-layer/local-preference'
import { useRouter } from 'expo-router'
import { useState } from 'react'
import { StyleSheet, View } from 'react-native'
import { Drawer } from 'react-native-drawer-layout'
import { lastSessionOf, pairedComputers, type PairedComputer } from '../computers/store'
import { ConnectionGate } from '../connection/connection-notices'
import { SessionList } from '../sessions/session-list'
import { useComputerSessions } from '../sessions/use-computer-sessions'
import { useColors } from '../ui/use-colors'
import { NewSessionScreen } from './new-session-screen'
import { SessionScreen } from './session-screen'

export function MainScreen({ computer }: { computer: PairedComputer }) {
  const colors = useColors()
  const router = useRouter()
  const [computers] = usePreference(pairedComputers)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [lastId, setLastId] = usePreference(lastSessionOf(computer.id))
  const sessions = useComputerSessions(computer.id)
  // The last Session reopens only while the list still has it.
  const selected = sessions.sessions.find((s) => s.sessionId === lastId) ?? null
  const openDrawer = () => setDrawerOpen(true)
  const select = (sessionId: string | null) => {
    setLastId(sessionId)
    setDrawerOpen(false)
  }

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
            computer={computer}
            computers={computers}
            sessions={sessions}
            selectedId={selected?.sessionId ?? null}
            onSelect={select}
            onNewSession={() => select(null)}
            onSettings={() => {
              setDrawerOpen(false)
              router.push('/settings')
            }}
            onAddComputer={() => {
              setDrawerOpen(false)
              router.push('/pair')
            }}
          />
        </View>
      )}
    >
      <View style={[styles.fill, { backgroundColor: colors.background }]}>
        <ConnectionGate computer={computer} drawerOpen={drawerOpen} onOpenDrawer={openDrawer}>
          {selected ? (
            <SessionScreen
              key={selected.sessionId}
              session={selected}
              parent={sessions.sessions.find((s) => s.sessionId === selected.parentId) ?? null}
              onContinued={(sessionId) => setLastId(sessionId)}
              drawerOpen={drawerOpen}
              onOpenDrawer={openDrawer}
            />
          ) : (
            <NewSessionScreen drawerOpen={drawerOpen} onOpenDrawer={openDrawer} />
          )}
        </ConnectionGate>
      </View>
    </Drawer>
  )
}

const styles = StyleSheet.create({ fill: { flex: 1 } })
