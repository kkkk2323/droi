// The connected screen: the open Session (or New session) fills the phone,
// and the Session list slides in from the left. The open Session is kept per
// Paired Computer, so the app reopens where it was.
import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import { usePreference } from '@droi/daemon-layer/local-preference'
import { recentWorkspaces } from '@droi/daemon-layer/use-new-session'
import { useRouter } from 'expo-router'
import { usePhoneAlerts } from '../alerts/use-phone-alerts'
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
  const { controller } = useDaemonConnection()
  const [drawerOpen, setDrawerOpen] = useState(false)
  // The New session page, preselected on a Workspace from its group's actions.
  const [newIn, setNewIn] = useState<string | null>(null)
  const [lastId, setLastId] = usePreference(lastSessionOf(computer.id))
  const sessions = useComputerSessions(computer.id)
  // The last Session reopens only while the list still has it.
  const selected = sessions.sessions.find((s) => s.sessionId === lastId) ?? null
  const unread = usePhoneAlerts(selected?.sessionId ?? null)
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
            unread={unread}
            onSelect={select}
            onNewSession={() => {
              setNewIn(null)
              select(null)
            }}
            onNewSessionIn={(workspace) => {
              setNewIn(workspace)
              select(null)
            }}
            onArchiveToggle={(session) => {
              if (session.archivedAt) {
                void controller.unarchiveSession(session.sessionId).catch(console.error)
                return
              }
              void controller
                .archiveSession(session.sessionId)
                .then(() => {
                  if (session.sessionId === lastId) setLastId(null)
                })
                .catch(console.error)
            }}
            onRename={(session, title) =>
              void controller.renameSession(session.sessionId, title).catch(console.error)
            }
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
            <NewSessionScreen
              key={newIn ?? ''}
              initialWorkspace={newIn}
              recent={recentWorkspaces(sessions.sessions)}
              onCreated={(sessionId) => setLastId(sessionId)}
              drawerOpen={drawerOpen}
              onOpenDrawer={openDrawer}
            />
          )}
        </ConnectionGate>
      </View>
    </Drawer>
  )
}

const styles = StyleSheet.create({ fill: { flex: 1 } })
