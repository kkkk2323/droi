// The Session list, the first page of a connected computer: grouped by
// Workspace like the web Client's sidebar, with Working and Needs input as
// distinct marks.
import {
  foldContinued,
  groupByWorkspace,
  OLDER_BATCH,
  visibleSessions,
  type SessionSummary,
  type WorkspaceGroup,
} from '@droi/daemon-layer/sessions'
import { mainSessions } from '@droi/daemon-layer/subagents'
import {
  foldedWorkspaces,
  pinnedSessions,
  pinnedWorkspaces,
  toggleListed,
  usePreference,
} from '@droi/daemon-layer/local-preference'
import { useSessionActivity, type SessionActivity } from '@droi/daemon-layer/use-session-activity'
import { ChevronDown, CircleAlert, Pin } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { Spinner } from '../ui/activity'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import type { ComputerSessions } from './use-computer-sessions'
import { Button, Text } from '../ui/primitives'
import { Sheet } from '../ui/sheet'
import { SessionSearchBox, SessionSearchResults } from './session-search'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionList({
  sessions,
  subagentsRunning,
  unread,
  onSelect,
  onNewSessionIn,
  onArchiveToggle,
  onRename,
}: {
  sessions: ComputerSessions
  /** How many subagents each row's Session has running. */
  subagentsRunning: ReadonlyMap<string, number>
  unread: ReadonlySet<string>
  onSelect: (sessionId: string) => void
  /** A new Session in that Workspace, or with None from Recents. */
  onNewSessionIn: (workspace: string | null) => void
  onArchiveToggle: (session: SessionSummary) => void
  onRename: (session: SessionSummary, title: string) => void
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  // The row a sheet acts on outlives the sheet being open, so its actions stay
  // on the panel while it slides away.
  const [sessionActions, setSessionActions] = useState<SessionSummary | null>(null)
  const [sessionActionsOpen, setSessionActionsOpen] = useState(false)
  const [workspaceActions, setWorkspaceActions] = useState<WorkspaceGroup | null>(null)
  const [workspaceActionsOpen, setWorkspaceActionsOpen] = useState(false)
  const activity = useSessionActivity()
  const [query, setQuery] = useState('')
  const [pinnedGroups] = usePreference(pinnedWorkspaces)
  const [pinnedIds] = usePreference(pinnedSessions)
  const groups = groupByWorkspace(foldContinued(mainSessions(sessions.sessions)), {
    workspaces: new Set(pinnedGroups),
    sessions: new Set(pinnedIds),
  })

  return (
    <View
      role="navigation"
      aria-label="Sessions"
      style={[styles.panel, { backgroundColor: colors.background }]}
    >
      <SessionSearchBox query={query} onChange={setQuery} />
      <ScrollView
        style={styles.scroll}
        contentContainerStyle={{ paddingBottom: insets.bottom + space.md }}
        keyboardShouldPersistTaps="handled"
      >
        {query.trim() ? (
          <SessionSearchResults query={query} onSelect={onSelect} />
        ) : (
          <>
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
                unread={unread}
                activity={activity}
                subagentsRunning={subagentsRunning}
                onSelect={onSelect}
                onSessionActions={(session) => {
                  setSessionActions(session)
                  setSessionActionsOpen(true)
                }}
                onWorkspaceActions={(picked) => {
                  setWorkspaceActions(picked)
                  setWorkspaceActionsOpen(true)
                }}
              />
            ))}
            {sessions.older ? (
              <Pressable
                role="button"
                disabled={sessions.older.loading}
                onPress={sessions.older.load}
                style={({ pressed }) => [
                  styles.older,
                  pressed ? { backgroundColor: colors.accent } : null,
                ]}
              >
                {sessions.older.loading ? (
                  <Spinner size={12} color={colors.mutedForeground} />
                ) : (
                  <ChevronDown size={14} color={colors.mutedForeground} strokeWidth={1.75} />
                )}
                <Text tone="muted" size="xs">
                  {sessions.older.loading ? 'Loading older sessions…' : 'Load older sessions'}
                </Text>
              </Pressable>
            ) : null}
          </>
        )}
      </ScrollView>
      <SessionActions
        session={sessionActions}
        open={sessionActionsOpen}
        onClose={() => setSessionActionsOpen(false)}
        onArchiveToggle={onArchiveToggle}
        onRename={onRename}
      />
      <Sheet
        visible={workspaceActionsOpen}
        title={workspaceActions ? `Actions for ${workspaceActions.label}` : ''}
        onClose={() => setWorkspaceActionsOpen(false)}
      >
        {workspaceActions ? (
          <>
            {/* Recents always sits at the bottom. */}
            {workspaceActions.scratch ? null : (
              <SheetButton
                label={
                  pinnedGroups.includes(workspaceActions.key) ? 'Unpin workspace' : 'Pin workspace'
                }
                onPress={() => {
                  toggleListed(pinnedWorkspaces, workspaceActions.key)
                  setWorkspaceActionsOpen(false)
                }}
              />
            )}
            <SheetButton
              label="New session here"
              onPress={() => {
                setWorkspaceActionsOpen(false)
                onNewSessionIn(workspaceActions.scratch ? null : workspaceActions.path)
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
  open,
  onClose,
  onArchiveToggle,
  onRename,
}: {
  session: SessionSummary | null
  open: boolean
  onClose: () => void
  onArchiveToggle: (session: SessionSummary) => void
  onRename: (session: SessionSummary, title: string) => void
}) {
  const colors = useColors()
  const [pinnedIds] = usePreference(pinnedSessions)
  const [renaming, setRenaming] = useState<string | null>(null)
  // Reset on opening rather than closing, so the rename field does not turn
  // back into the buttons while the sheet slides away.
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setRenaming(null)
  }
  const saveRename = () => {
    if (session && renaming?.trim()) onRename(session, renaming.trim())
    onClose()
  }
  return (
    <Sheet visible={open} title={session ? `Actions for ${session.title}` : ''} onClose={onClose}>
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
              onClose()
            }}
          />
          <SheetButton label="Rename" onPress={() => setRenaming(session.title)} />
          <SheetButton
            label={session.archivedAt ? 'Unarchive' : 'Archive'}
            onPress={() => {
              onArchiveToggle(session)
              onClose()
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

function WorkspaceSection({
  group,
  unread,
  activity,
  subagentsRunning,
  onSelect,
  onSessionActions,
  onWorkspaceActions,
}: {
  group: WorkspaceGroup
  unread: ReadonlySet<string>
  activity: ReadonlyMap<string, SessionActivity>
  subagentsRunning: ReadonlyMap<string, number>
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
            const isUnread = unread.has(session.sessionId)
            return (
              <Pressable
                key={session.sessionId}
                role="button"
                accessibilityHint="Hold for more actions"
                onPress={() => onSelect(session.sessionId)}
                onLongPress={() => onSessionActions(session)}
                style={({ pressed }) => [
                  styles.row,
                  pressed ? { backgroundColor: colors.accent } : null,
                ]}
              >
                <Text
                  size="base"
                  weight={isUnread ? 'medium' : 'regular'}
                  numberOfLines={1}
                  style={styles.title}
                >
                  {session.title}
                </Text>
                {isUnread ? (
                  <View
                    role="img"
                    aria-label="Unread"
                    style={[styles.unread, { backgroundColor: colors.unread }]}
                  />
                ) : null}
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
                <ActivityMark
                  activity={activity.get(session.sessionId)}
                  subagents={subagentsRunning.get(session.sessionId) ?? 0}
                />
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

function ActivityMark({
  activity,
  subagents,
}: {
  activity: SessionActivity | undefined
  subagents: number
}) {
  const colors = useColors()
  if (activity === 'compacting') {
    return (
      <View role="status" aria-label="Compacting" style={styles.mark}>
        <Spinner size={12} color={colors.working} />
        <Text size="xs" style={{ color: colors.working }}>
          Compacting
        </Text>
      </View>
    )
  }
  if (activity !== 'needs-input' && subagents > 0) {
    return (
      <View role="status" style={styles.mark}>
        <Spinner size={12} color={colors.working} />
        <Text size="xs" style={{ color: colors.working }}>
          {subagents} {subagents === 1 ? 'subagent' : 'subagents'} running
        </Text>
      </View>
    )
  }
  if (activity === 'working') {
    return (
      <View role="status" aria-label="Working" style={styles.mark}>
        <Spinner size={12} color={colors.working} />
        <Text size="xs" style={{ color: colors.working }}>
          Working
        </Text>
      </View>
    )
  }
  if (activity === 'needs-input') {
    return (
      <View role="status" aria-label="Needs input" style={styles.mark}>
        <CircleAlert size={12} color={colors.attention} strokeWidth={2} />
        <Text size="xs" style={{ color: colors.attention }}>
          Needs input
        </Text>
      </View>
    )
  }
  return null
}

const styles = StyleSheet.create({
  panel: { flex: 1 },
  scroll: { flex: 1, paddingHorizontal: space.sm },
  empty: { paddingHorizontal: space.sm, paddingVertical: space.xs },
  older: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    marginTop: space.xs,
    paddingHorizontal: space.sm,
    paddingVertical: space.xs,
    borderRadius: radius.md,
  },
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
    minHeight: 44,
    paddingHorizontal: space.sm,
    borderRadius: radius.lg,
  },
  title: { flex: 1 },
  unread: { width: 6, height: 6, borderRadius: 3 },
  mark: { flexDirection: 'row', alignItems: 'center', gap: 4 },
})
