// The connected screen: the open Session (or New session) fills the phone,
// and the Session list slides in from the left. The open Session is kept per
// Paired Computer, so the app reopens where it was.
import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import { usePreference } from '@droi/daemon-layer/local-preference'
import { continuationChain } from '@droi/daemon-layer/sessions'
import {
  callerTrail,
  runningSubagents,
  SubagentLinksProvider,
  subagentsByToolUse,
  subagentSiblings,
  subagentsOf,
  useSubagentRuns,
} from '@droi/daemon-layer/subagents'
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
  const runs = useSubagentRuns(
    sessions.sessions.filter((s) => s.callingSessionId).map((s) => s.sessionId),
  )
  const chain = selected ? continuationChain(sessions.sessions, selected) : []
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
            subagentsRunning={runningSubagents(sessions.sessions, runs)}
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
            <SubagentLinksProvider
              value={{
                byToolUse: subagentsByToolUse(sessions.sessions),
                runs,
                open: (sessionId) => setLastId(sessionId),
              }}
            >
              <SessionScreen
                key={selected.sessionId}
                session={selected}
                chain={chain}
                trail={callerTrail(sessions.sessions, selected)}
                siblings={subagentSiblings(sessions.sessions, selected)}
                subagents={subagentsOf(sessions.sessions, [
                  selected.sessionId,
                  ...chain.map((s) => s.sessionId),
                ])}
                onContinued={(sessionId) => setLastId(sessionId)}
                drawerOpen={drawerOpen}
                onOpenDrawer={openDrawer}
              />
            </SubagentLinksProvider>
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
