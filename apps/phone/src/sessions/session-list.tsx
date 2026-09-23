// The drawer's Session list: grouped by Workspace like the web Client's
// sidebar, with Working and Needs input as distinct marks.
import { useConnectionState } from '@droi/daemon-layer/connection-context'
import {
  foldContinued,
  groupByWorkspace,
  OLDER_BATCH,
  visibleSessions,
  type SessionSummary,
  type WorkspaceGroup,
} from '@droi/daemon-layer/sessions'
import {
  foldedWorkspaces,
  pinnedSessions,
  pinnedWorkspaces,
  toggleListed,
  usePreference,
} from '@droi/daemon-layer/local-preference'
import { useSessionActivity, type SessionActivity } from '@droi/daemon-layer/use-session-activity'
import { ChevronDown, Pin, Plus, Settings, SquarePen } from 'lucide-react-native'
import { useState } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { selectedComputerId, type PairedComputer } from '../computers/store'
import type { ComputerSessions } from './use-computer-sessions'
import { Button, IconButton, Text } from '../ui/primitives'
import { Sheet } from '../ui/sheet'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionList({
  computer,
  computers,
  sessions,
  selectedId,
  onSelect,
  onNewSession,
  onNewSessionIn,
  onSettings,
  onAddComputer,
  onArchiveToggle,
  onRename,
}: {
  computer: PairedComputer
  computers: readonly PairedComputer[]
  sessions: ComputerSessions
  selectedId: string | null
  onSelect: (sessionId: string) => void
  onNewSession: () => void
  onNewSessionIn: (workspace: string) => void
  onSettings: () => void
  onAddComputer: () => void
  onArchiveToggle: (session: SessionSummary) => void
  onRename: (session: SessionSummary, title: string) => void
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const [switching, setSwitching] = useState(false)
  const [sessionActions, setSessionActions] = useState<SessionSummary | null>(null)
  const [workspaceActions, setWorkspaceActions] = useState<WorkspaceGroup | null>(null)
  const activity = useSessionActivity()
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  const [pinnedIds] = usePreference(pinnedSessions)
  const groups = groupByWorkspace(foldContinued(sessions.sessions), {
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
        <Pressable
          role="button"
          aria-label={`Switch computer, ${computer.name}`}
          aria-expanded={switching}
          onPress={() => setSwitching(!switching)}
          style={styles.computer}
        >
          <View style={styles.computerName}>
            <Text weight="semibold" numberOfLines={1} style={styles.shrink}>
              {computer.name}
            </Text>
            <ChevronDown size={14} color={colors.mutedForeground} strokeWidth={2} />
          </View>
          <ConnectionLine />
        </Pressable>
        <IconButton label="Settings" icon={Settings} onPress={onSettings} />
      </View>
      {switching ? (
        <View role="menu" aria-label="Computers" style={styles.switcher}>
          {computers
            .filter((c) => c.id !== computer.id)
            .map((c) => (
              <Pressable
                key={c.id}
                role="menuitem"
                onPress={() => selectedComputerId.set(c.id)}
                style={({ pressed }) => [
                  styles.switcherRow,
                  pressed ? { backgroundColor: colors.sidebarAccent } : null,
                ]}
              >
                <Text size="sm" numberOfLines={1}>
                  {c.name}
                </Text>
              </Pressable>
            ))}
          <Pressable
            role="menuitem"
            onPress={onAddComputer}
            style={({ pressed }) => [
              styles.switcherRow,
              pressed ? { backgroundColor: colors.sidebarAccent } : null,
            ]}
          >
            <Plus size={16} color={colors.mutedForeground} strokeWidth={1.75} />
            <Text size="sm">Add a computer</Text>
          </Pressable>
        </View>
      ) : null}
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
        {!sessions.isPending && sessions.live && groups.length === 0 && !sessions.error ? (
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
            onSelect={onSelect}
            onSessionActions={setSessionActions}
            onWorkspaceActions={setWorkspaceActions}
          />
        ))}
      </ScrollView>
      <SessionActions
        session={sessionActions}
        onClose={() => setSessionActions(null)}
        onArchiveToggle={onArchiveToggle}
        onRename={onRename}
      />
      <Sheet
        visible={workspaceActions !== null}
        title={workspaceActions ? `Actions for ${workspaceActions.label}` : ''}
        onClose={() => setWorkspaceActions(null)}
      >
        {workspaceActions ? (
          <>
            <SheetButton
              label={
                pinnedGroups.includes(workspaceActions.key) ? 'Unpin workspace' : 'Pin workspace'
              }
              onPress={() => {
                toggleListed(pinnedWorkspaces, workspaceActions.key)
                setWorkspaceActions(null)
              }}
            />
            <SheetButton
              label="New session here"
              onPress={() => {
                setWorkspaceActions(null)
                onNewSessionIn(workspaceActions.path)
              }}
            />
          </>
        ) : null}
      </Sheet>
    </View>
  )
}

function SessionActions({
  session,
  onClose,
  onArchiveToggle,
  onRename,
}: {
  session: SessionSummary | null
  onClose: () => void
  onArchiveToggle: (session: SessionSummary) => void
  onRename: (session: SessionSummary, title: string) => void
}) {
  const colors = useColors()
  const [pinnedIds] = usePreference(pinnedSessions)
  const [renaming, setRenaming] = useState<string | null>(null)
  const close = () => {
    setRenaming(null)
    onClose()
  }
  const saveRename = () => {
    if (session && renaming?.trim()) onRename(session, renaming.trim())
    close()
  }
  return (
    <Sheet
      visible={session !== null}
      title={session ? `Actions for ${session.title}` : ''}
      onClose={close}
    >
      {session && renaming !== null ? (
        <View style={styles.rename}>
          <TextInput
            aria-label="Session title"
            value={renaming}
            onChangeText={setRenaming}
            autoFocus
            onSubmitEditing={saveRename}
            style={[styles.renameInput, { borderColor: colors.input, color: colors.foreground }]}
          />
          <Button label="Save" disabled={!renaming.trim()} onPress={saveRename} />
        </View>
      ) : session ? (
        <>
          <SheetButton
            label={pinnedIds.includes(session.sessionId) ? 'Unpin' : 'Pin'}
            onPress={() => {
              toggleListed(pinnedSessions, session.sessionId)
              close()
            }}
          />
          <SheetButton label="Rename" onPress={() => setRenaming(session.title)} />
          <SheetButton
            label={session.archivedAt ? 'Unarchive' : 'Archive'}
            onPress={() => {
              onArchiveToggle(session)
              close()
            }}
          />
        </>
      ) : null}
    </Sheet>
  )
}

function SheetButton({ label, onPress }: { label: string; onPress: () => void }) {
  const colors = useColors()
  return (
    <Pressable
      role="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.sheetButton,
        pressed ? { backgroundColor: colors.accent } : null,
      ]}
    >
      <Text>{label}</Text>
    </Pressable>
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
  onSelect,
  onSessionActions,
  onWorkspaceActions,
}: {
  group: WorkspaceGroup
  selectedId: string | null
  activity: ReadonlyMap<string, SessionActivity>
  onSelect: (sessionId: string) => void
  onSessionActions: (session: SessionSummary) => void
  onWorkspaceActions: (group: WorkspaceGroup) => void
}) {
  const colors = useColors()
  const [folded] = usePreference(foldedWorkspaces)
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  const [pinnedIds] = usePreference(pinnedSessions)
  const open = !folded.includes(group.key)
  const [revealed, setRevealed] = useState(0)
  const { visible, hidden } = visibleSessions(
    group.sessions,
    revealed,
    undefined,
    new Set(pinnedIds),
  )
  return (
    <View role="group" aria-label={group.label} style={styles.section}>
      <Pressable
        role="button"
        aria-label={group.label}
        aria-expanded={open}
        accessibilityHint="Hold for more actions"
        onPress={() => toggleListed(foldedWorkspaces, group.key)}
        onLongPress={() => onWorkspaceActions(group)}
        style={styles.sectionHeader}
      >
        <Text
          role="heading"
          size="xs"
          weight="medium"
          tone="muted"
          numberOfLines={1}
          style={styles.title}
        >
          {group.label}
        </Text>
        {pinnedGroups.includes(group.key) ? (
          <View role="img" aria-label="Pinned">
            <Pin size={11} color={colors.mutedForeground} strokeWidth={2} />
          </View>
        ) : null}
        <ChevronDown
          size={12}
          color={colors.mutedForeground}
          style={{ transform: [{ rotate: open ? '0deg' : '-90deg' }] }}
        />
      </Pressable>
      {open
        ? visible.map((session) => {
            const selected = session.sessionId === selectedId
            return (
              <Pressable
                key={session.sessionId}
                role="button"
                aria-current={selected ? 'page' : undefined}
                accessibilityHint="Hold for more actions"
                onPress={() => onSelect(session.sessionId)}
                onLongPress={() => onSessionActions(session)}
                style={({ pressed }) => [
                  styles.row,
                  selected || pressed ? { backgroundColor: colors.sidebarAccent } : null,
                ]}
              >
                <Text size="sm" numberOfLines={1} style={styles.title}>
                  {session.title}
                </Text>
                {pinnedIds.includes(session.sessionId) ? (
                  <View role="img" aria-label="Pinned">
                    <Pin size={11} color={colors.mutedForeground} strokeWidth={2} />
                  </View>
                ) : null}
                {session.archivedAt ? (
                  <Text size="xs" tone="muted">
                    Archived
                  </Text>
                ) : null}
                <ActivityMark activity={activity.get(session.sessionId)} />
              </Pressable>
            )
          })
        : null}
      {open && hidden > 0 ? (
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
  computerName: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  shrink: { flexShrink: 1 },
  switcher: { marginHorizontal: space.sm, marginBottom: space.sm },
  switcherRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 36,
    paddingHorizontal: space.sm,
    borderRadius: radius.lg,
  },
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
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    paddingHorizontal: space.sm,
    minHeight: 28,
  },
  rename: { flexDirection: 'row', gap: space.sm, paddingHorizontal: space.lg },
  renameInput: {
    flex: 1,
    height: 40,
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    fontFamily: fonts.sans,
    fontSize: fontSize.base,
  },
  sheetButton: {
    minHeight: 44,
    justifyContent: 'center',
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
    borderRadius: radius.lg,
  },
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
