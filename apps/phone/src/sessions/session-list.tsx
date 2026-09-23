// The drawer's Session list: grouped by Workspace like the web Client's
// sidebar, with Working and Needs input as distinct marks.
import { useConnectionState } from '@droi/daemon-layer/connection-context'
import {
  foldContinued,
  groupByWorkspace,
  OLDER_BATCH,
  useSessionList,
  visibleSessions,
  type SessionSummary,
  type WorkspaceGroup,
} from '@droi/daemon-layer/sessions'
import {
  pinnedSessions,
  pinnedWorkspaces,
  showArchivedSessions,
  usePreference,
} from '@droi/daemon-layer/local-preference'
import { useSessionActivity, type SessionActivity } from '@droi/daemon-layer/use-session-activity'
import { Settings, SquarePen } from 'lucide-react-native'
import { useState } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { IconButton, Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionList({
  computerName,
  selectedId,
  onSelect,
  onNewSession,
  onSettings,
}: {
  computerName: string
  selectedId: string | null
  onSelect: (session: SessionSummary) => void
  onNewSession: () => void
  onSettings: () => void
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const [showArchived] = usePreference(showArchivedSessions)
  const sessions = useSessionList({ includeArchived: showArchived })
  const activity = useSessionActivity()
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  const [pinnedIds] = usePreference(pinnedSessions)
  const groups = groupByWorkspace(foldContinued(sessions.data ?? []), {
    workspaces: new Set(pinnedGroups),
    sessions: new Set(pinnedIds),
  })

  return (
    <View
      role="navigation"
      aria-label="Sessions"
      style={[styles.panel, { backgroundColor: colors.sidebar, paddingTop: insets.top }]}
    >
      <View style={styles.header}>
        <View style={styles.computer}>
          <Text weight="semibold" numberOfLines={1}>
            {computerName}
          </Text>
          <ConnectionLine />
        </View>
        <IconButton label="Settings" icon={Settings} onPress={onSettings} />
      </View>
      <Pressable
        role="button"
        onPress={onNewSession}
        style={({ pressed }) => [
          styles.newSession,
          pressed ? { backgroundColor: colors.sidebarAccent } : null,
        ]}
      >
        <SquarePen size={16} color={colors.mutedForeground} strokeWidth={1.75} />
        <Text size="sm">New session</Text>
      </Pressable>
      <ScrollView
        style={styles.scroll}
        contentContainerStyle={{ paddingBottom: insets.bottom + space.md }}
      >
        {sessions.error ? (
          <Text role="alert" size="xs" style={{ color: colors.destructiveForeground }}>
            {sessions.error.message}
          </Text>
        ) : null}
        {!sessions.isPending && groups.length === 0 && !sessions.error ? (
          <Text tone="muted" size="xs" style={styles.empty}>
            No sessions yet.
          </Text>
        ) : null}
        {groups.map((group) => (
          <WorkspaceSection
            key={group.key}
            group={group}
            selectedId={selectedId}
            activity={activity}
            pinned={new Set(pinnedIds)}
            onSelect={onSelect}
          />
        ))}
      </ScrollView>
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

function WorkspaceSection({
  group,
  selectedId,
  activity,
  pinned,
  onSelect,
}: {
  group: WorkspaceGroup
  selectedId: string | null
  activity: ReadonlyMap<string, SessionActivity>
  pinned: ReadonlySet<string>
  onSelect: (session: SessionSummary) => void
}) {
  const colors = useColors()
  const [revealed, setRevealed] = useState(0)
  const { visible, hidden } = visibleSessions(group.sessions, revealed, undefined, pinned)
  return (
    <View role="group" aria-label={group.label} style={styles.section}>
      <Text
        role="heading"
        size="xs"
        weight="medium"
        tone="muted"
        numberOfLines={1}
        style={styles.sectionTitle}
      >
        {group.label}
      </Text>
      {visible.map((session) => {
        const selected = session.sessionId === selectedId
        return (
          <Pressable
            key={session.sessionId}
            role="button"
            aria-current={selected ? 'page' : undefined}
            onPress={() => onSelect(session)}
            style={({ pressed }) => [
              styles.row,
              selected || pressed ? { backgroundColor: colors.sidebarAccent } : null,
            ]}
          >
            <Text size="sm" numberOfLines={1} style={styles.title}>
              {session.title}
            </Text>
            <ActivityMark activity={activity.get(session.sessionId)} />
          </Pressable>
        )
      })}
      {hidden > 0 ? (
        <Pressable
          role="button"
          onPress={() => setRevealed(revealed + OLDER_BATCH)}
          style={styles.row}
        >
          <Text size="xs" tone="muted">
            Show {Math.min(hidden, OLDER_BATCH)} more
          </Text>
        </Pressable>
      ) : null}
    </View>
  )
}

function ActivityMark({ activity }: { activity: SessionActivity | undefined }) {
  const colors = useColors()
  if (activity === 'working') {
    return (
      <View role="status" aria-label="Working">
        <ActivityIndicator size="small" color={colors.mutedForeground} />
      </View>
    )
  }
  if (activity === 'needs-input') {
    return (
      <View role="status" aria-label="Needs input">
        <Text size="xs" weight="medium" style={{ color: colors.attention }}>
          Needs input
        </Text>
      </View>
    )
  }
  return null
}

const styles = StyleSheet.create({
  panel: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
  },
  computer: { flex: 1, minWidth: 0 },
  newSession: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    marginHorizontal: space.sm,
    paddingHorizontal: space.sm,
    height: 36,
    borderRadius: radius.lg,
  },
  scroll: { flex: 1, paddingHorizontal: space.sm },
  empty: { paddingHorizontal: space.sm, paddingVertical: space.xs },
  section: { marginTop: space.md },
  sectionTitle: { paddingHorizontal: space.sm, paddingBottom: space.xs },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 36,
    paddingHorizontal: space.sm,
    borderRadius: radius.lg,
  },
  title: { flex: 1 },
})
