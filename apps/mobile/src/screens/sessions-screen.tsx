// The connected computer's first page: its Session list. Its title switches
// computers; New session and Settings sit on the right of the bar. A launch
// that lands here reopens the Session the app last showed, pushed over the
// list so going back lands here.
import { archiveConversation, unarchiveConversation } from '@droi/daemon-layer/archive'
import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import { usePreference } from '@droi/daemon-layer/local-preference'
import { listOmpSessions, type OmpSession } from '@droi/daemon-layer/omp'
import { runningSubagents } from '@droi/daemon-layer/subagents'
import { Stack, useFocusEffect, useNavigation, useRouter } from 'expo-router'
import { Settings, SquarePen } from 'lucide-react-native'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Pressable, StyleSheet, View } from 'react-native'
import { lastSessionOf, pairedComputers } from '../computers/store'
import { ConnectionGate } from '../connection/connection-notices'
import { ComputerMenu, ComputerTitle } from '../sessions/computer-switch'
import { sessionPath, useConnectedComputer } from '../sessions/connected-computer'
import { SessionList } from '../sessions/session-list'
import { IconButton, Text } from '../ui/primitives'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionsScreen() {
  const connected = useConnectedComputer()
  if (!connected) return null
  return <Sessions connected={connected} />
}

function Sessions({
  connected,
}: {
  connected: NonNullable<ReturnType<typeof useConnectedComputer>>
}) {
  const colors = useColors()
  const router = useRouter()
  const connection = useDaemonConnection()
  const [computers] = usePreference(pairedComputers)
  const [switching, setSwitching] = useState(false)
  const { computer, sessions, unread, runs } = connected
  const [ompSessions, setOmpSessions] = useState<OmpSession[]>([])
  const [ompAvailable, setOmpAvailable] = useState(false)
  useEffect(() => {
    if (!connection.gateway.token) return
    let cancelled = false
    const load = async () => {
      try {
        const got = await listOmpSessions(connection.gateway.url, connection.gateway.token!)
        if (cancelled) return
        setOmpAvailable(got.available)
        setOmpSessions(got.sessions)
      } catch {
        if (!cancelled) {
          setOmpAvailable(false)
          setOmpSessions([])
        }
      }
    }
    void load()
    const t = setInterval(() => void load(), 5000)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [connection.gateway.url, connection.gateway.token])
  useReopenLastSession(computer.id, (id) => connected.find(id) !== null)
  const computerTitle = (
    <ComputerTitle
      computer={computer}
      open={switching}
      onToggle={() => setSwitching((open) => !open)}
    />
  )
  const actions = (
    <View style={styles.actions}>
      <IconButton label="New session" icon={SquarePen} onPress={() => router.push('/new')} />
      <IconButton label="Settings" icon={Settings} onPress={() => router.push('/settings')} />
    </View>
  )

  return (
    <View style={styles.fill}>
      <Stack.Screen
        options={{
          headerTitle: () => computerTitle,
          headerRight: () => actions,
          headerStyle: { backgroundColor: colors.sidebar },
        }}
      />
      {switching ? (
        <ComputerMenu
          computer={computer}
          computers={computers}
          onAddComputer={() => {
            setSwitching(false)
            router.push('/pair')
          }}
        />
      ) : null}
      <ConnectionGate computer={computer} keepWhenUnreachable>
        {ompAvailable || ompSessions.length > 0 ? (
          <View style={[styles.ompStrip, { borderBottomColor: colors.border }]}>
            <Text size="xs" weight="semibold" tone="muted">
              OMP{ompSessions.length ? ` · ${ompSessions.length}` : ''}
            </Text>
            {ompSessions.map((s) => (
              <Pressable
                key={s.id}
                onPress={() => router.push(`/omp/${s.id}`)}
                style={[styles.ompChip, { backgroundColor: colors.secondary }]}
                accessibilityRole="button"
                accessibilityLabel={s.title || 'OMP session'}
              >
                <Text size="xs" numberOfLines={1}>
                  {s.title || 'OMP session'}
                </Text>
              </Pressable>
            ))}
            {ompAvailable && ompSessions.length === 0 ? (
              <Text size="xs" tone="muted">
                Live sessions appear here
              </Text>
            ) : null}
          </View>
        ) : null}
        <SessionList
          sessions={sessions}
          subagentsRunning={runningSubagents(sessions.sessions, runs)}
          unread={unread}
          onSelect={connected.open}
          onNewSessionIn={(workspace) =>
            router.push(
              workspace === null
                ? '/new?scratch=1'
                : `/new?workspace=${encodeURIComponent(workspace)}`,
            )
          }
          onArchiveToggle={(session) => {
            const change = session.archivedAt ? unarchiveConversation : archiveConversation
            void change(connection, sessions.sessions, session).catch(console.error)
          }}
          onRename={(session, title) =>
            void connection.controller.renameSession(session.sessionId, title).catch(console.error)
          }
        />
      </ConnectionGate>
    </View>
  )
}

/**
 * The first time the list is on screen, pushes the Session the app last
 * showed, if it is still known. Coming back to the list forgets it, so the
 * next launch opens on the list.
 */
function useReopenLastSession(computerId: string, known: (sessionId: string) => boolean) {
  const router = useRouter()
  const navigation = useNavigation()
  // A reload on a Session's own address builds the list underneath it; that
  // launch did not land here.
  const first = useRef(navigation.isFocused())
  const latest = useRef(known)
  useEffect(() => {
    latest.current = known
  })
  useFocusEffect(
    useCallback(() => {
      const last = lastSessionOf(computerId)
      const reopen = first.current ? last.get() : null
      first.current = false
      if (reopen && latest.current(reopen)) router.push(sessionPath(reopen))
      else last.set(null)
    }, [computerId, router]),
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  actions: { flexDirection: 'row', alignItems: 'center' },
  ompStrip: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  ompChip: {
    maxWidth: 160,
    paddingHorizontal: space.sm,
    paddingVertical: 4,
    borderRadius: 8,
  },
})
