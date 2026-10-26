// The Session list, the first page of a connected computer, laid out as the
// computer's sidebar: the pinned Sessions and Workspaces first, then the
// Workspaces in the order the computer shows them, the ones unused for a
// month behind a button, and Recents last (see use-list-order.ts).
import {
  foldContinued,
  groupByWorkspace,
  OLDER_BATCH,
  PINNED_SESSIONS_GROUP_KEY,
  splitStale,
  visibleSessions,
  withoutAutomationRuns,
  type Pins,
  type SessionSummary,
  type WorkspaceGroup,
} from '@droi/daemon-layer/sessions'
import { mainSessions } from '@droi/daemon-layer/subagents'
import { foldedWorkspaces, toggleListed, usePreference } from '@droi/daemon-layer/local-preference'
import { ARCHIVE_WORKTREE_WARNING, archiveDeletesWorktree } from '@droi/daemon-layer/worktree'
import { useSessionActivity, type SessionActivity } from '@droi/daemon-layer/use-session-activity'
import {
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Folder,
  FolderOpen,
  GitBranch,
  MessageSquare,
  Pin,
} from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native'
import { Spinner } from '../ui/activity'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { relativeTime } from '../lib/relative-time'
import type { ComputerSessions } from './use-computer-sessions'
import { useListOrder } from './use-list-order'
import { Button, Text } from '../ui/primitives'
import { Sheet } from '../ui/sheet'
import { SessionSearchBox, SessionSearchResults } from './session-search'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

/** Folded like a Workspace, in the same preference; the computer's own keys. */
const PINNED_SECTION_KEY = 'droi:pinned'
const WORKSPACES_SECTION_KEY = 'droi:workspaces'

interface RowContext {
  pins: Pins
  unread: ReadonlySet<string>
  activity: ReadonlyMap<string, SessionActivity>
  subagentsRunning: ReadonlyMap<string, number>
  onSelect: (sessionId: string) => void
  onSessionActions: (session: SessionSummary) => void
  onWorkspaceActions: (group: WorkspaceGroup) => void
}

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
  const [staleShown, setStaleShown] = useState(false)
  const activity = useSessionActivity()
  const [query, setQuery] = useState('')
  const { pins, order, setPinned } = useListOrder()
  const groups = groupByWorkspace(
    foldContinued(withoutAutomationRuns(mainSessions(sessions.sessions))),
    pins,
    order,
    { pinnedApart: true },
  )
  let loose: WorkspaceGroup | null = null
  let recents: WorkspaceGroup | null = null
  const pinned: WorkspaceGroup[] = []
  const rest: WorkspaceGroup[] = []
  for (const group of groups) {
    if (group.scratch) recents = group
    else if (group.key === PINNED_SESSIONS_GROUP_KEY) loose = group
    else if (pins.workspaces.has(group.key)) pinned.push(group)
    else rest.push(group)
  }
  const { active, stale } = splitStale(rest)
  const rows: RowContext = {
    pins,
    unread,
    activity,
    subagentsRunning,
    onSelect,
    onSessionActions: (session) => {
      setSessionActions(session)
      setSessionActionsOpen(true)
    },
    onWorkspaceActions: (picked) => {
      setWorkspaceActions(picked)
      setWorkspaceActionsOpen(true)
    },
  }
  const staleLabel = staleShown
    ? 'Hide older workspaces'
    : stale.length === 1
      ? 'Show 1 older workspace'
      : `Show ${stale.length} older workspaces`
  const restSections = (
    <>
      {active.map((group) => (
        <WorkspaceSection key={group.key} group={group} rows={rows} />
      ))}
      {stale.length > 0 ? (
        <Pressable
          role="button"
          aria-expanded={staleShown}
          onPress={() => setStaleShown(!staleShown)}
          style={({ pressed }) => [
            styles.older,
            pressed ? { backgroundColor: colors.sidebarAccent } : null,
          ]}
        >
          <ChevronDown
            size={14}
            color={colors.mutedForeground}
            strokeWidth={1.75}
            style={{ transform: [{ rotate: staleShown ? '180deg' : '0deg' }] }}
          />
          <Text tone="muted" size="xs">
            {staleLabel}
          </Text>
        </Pressable>
      ) : null}
      {staleShown
        ? stale.map((group) => <WorkspaceSection key={group.key} group={group} rows={rows} />)
        : null}
    </>
  )

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
            {loose || pinned.length > 0 ? (
              <Foldable label="Pinned" foldKey={PINNED_SECTION_KEY}>
                {loose ? (
                  <View style={styles.loose}>
                    {loose.sessions.map((session) => (
                      <SessionRow key={session.sessionId} session={session} rows={rows} />
                    ))}
                  </View>
                ) : null}
                {pinned.map((group) => (
                  <WorkspaceSection key={group.key} group={group} rows={rows} />
                ))}
              </Foldable>
            ) : null}
            {rest.length > 0 && (loose || pinned.length > 0 || recents) ? (
              <Foldable label="Workspaces" foldKey={WORKSPACES_SECTION_KEY}>
                {restSections}
              </Foldable>
            ) : (
              restSections
            )}
            {recents ? <WorkspaceSection group={recents} rows={rows} /> : null}
            {sessions.older ? (
              <Pressable
                role="button"
                disabled={sessions.older.loading}
                onPress={sessions.older.load}
                style={({ pressed }) => [
                  styles.older,
                  pressed ? { backgroundColor: colors.sidebarAccent } : null,
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
        pinned={sessionActions ? pins.sessions.has(sessionActions.sessionId) : false}
        open={sessionActionsOpen}
        onClose={() => setSessionActionsOpen(false)}
        onPinToggle={(session, pin) => setPinned('session', session.sessionId, pin)}
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
                  pins.workspaces.has(workspaceActions.key) ? 'Unpin workspace' : 'Pin workspace'
                }
                onPress={() => {
                  setPinned(
                    'workspace',
                    workspaceActions.key,
                    !pins.workspaces.has(workspaceActions.key),
                  )
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

/** The Pinned or Workspaces section: a label folding what is under it. */
function Foldable({
  label,
  foldKey,
  children,
}: {
  label: string
  foldKey: string
  children: ReactNode
}) {
  const colors = useColors()
  const [folded] = usePreference(foldedWorkspaces)
  const open = !folded.includes(foldKey)
  return (
    <View role="group" aria-label={label} style={styles.foldable}>
      <Pressable
        role="button"
        aria-label={label}
        aria-expanded={open}
        onPress={() => toggleListed(foldedWorkspaces, foldKey)}
        style={styles.foldHeader}
      >
        <Text size="xs" weight="medium" tone="muted" numberOfLines={1}>
          {label}
        </Text>
        <ChevronRight
          size={12}
          color={colors.mutedForeground}
          strokeWidth={2}
          style={{ transform: [{ rotate: open ? '90deg' : '0deg' }] }}
        />
      </Pressable>
      {open ? children : null}
    </View>
  )
}

function SessionActions({
  session,
  pinned,
  open,
  onClose,
  onPinToggle,
  onArchiveToggle,
  onRename,
}: {
  session: SessionSummary | null
  pinned: boolean
  open: boolean
  onClose: () => void
  onPinToggle: (session: SessionSummary, pinned: boolean) => void
  onArchiveToggle: (session: SessionSummary) => void
  onRename: (session: SessionSummary, title: string) => void
}) {
  const colors = useColors()
  const [renaming, setRenaming] = useState<string | null>(null)
  const [confirmingArchive, setConfirmingArchive] = useState(false)
  // Reset on opening rather than closing, so the rename field does not turn
  // back into the buttons while the sheet slides away.
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) {
      setRenaming(null)
      setConfirmingArchive(false)
    }
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
      ) : session && confirmingArchive ? (
        <View style={styles.confirm}>
          <Text size="sm">{ARCHIVE_WORKTREE_WARNING}</Text>
          <View style={styles.confirmButtons}>
            <Button
              label="Cancel"
              variant="secondary"
              style={styles.grow}
              onPress={() => setConfirmingArchive(false)}
            />
            <Button
              label="Archive"
              style={[styles.grow, { backgroundColor: colors.destructive }]}
              onPress={() => {
                onArchiveToggle(session)
                onClose()
              }}
            />
          </View>
        </View>
      ) : session ? (
        <>
          <SheetButton
            label={pinned ? 'Unpin' : 'Pin'}
            onPress={() => {
              onPinToggle(session, !pinned)
              onClose()
            }}
          />
          <SheetButton label="Rename" onPress={() => setRenaming(session.title)} />
          <SheetButton
            label={session.archivedAt ? 'Unarchive' : 'Archive'}
            onPress={() => {
              if (!session.archivedAt && archiveDeletesWorktree(session)) {
                setConfirmingArchive(true)
                return
              }
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

/** A Workspace's header and its Sessions; Recents' header is a quiet label. */
function WorkspaceSection({ group, rows }: { group: WorkspaceGroup; rows: RowContext }) {
  const colors = useColors()
  const [folded] = usePreference(foldedWorkspaces)
  const open = !folded.includes(group.key)
  const [revealed, setRevealed] = useState(0)
  const { visible, hidden } = visibleSessions(
    group.sessions,
    revealed,
    undefined,
    rows.pins.sessions,
  )
  const FolderIcon = open ? FolderOpen : Folder
  return (
    <View role="group" aria-label={group.label} style={styles.section}>
      <Pressable
        role="button"
        aria-label={group.label}
        aria-expanded={open}
        accessibilityHint="Hold for more actions"
        onPress={() => toggleListed(foldedWorkspaces, group.key)}
        onLongPress={() => rows.onWorkspaceActions(group)}
        style={group.scratch ? styles.foldHeader : styles.sectionHeader}
      >
        {group.scratch ? (
          <>
            <Text size="xs" weight="medium" tone="muted" numberOfLines={1}>
              {group.label}
            </Text>
            <ChevronRight
              size={12}
              color={colors.mutedForeground}
              strokeWidth={2}
              style={{ transform: [{ rotate: open ? '90deg' : '0deg' }] }}
            />
          </>
        ) : (
          <>
            <FolderIcon size={16} color={colors.mutedForeground} strokeWidth={1.75} />
            <Text
              role="heading"
              size="base"
              weight="semibold"
              numberOfLines={1}
              style={styles.title}
            >
              {group.label}
            </Text>
          </>
        )}
      </Pressable>
      {open
        ? visible.map((session) => (
            <SessionRow
              key={session.sessionId}
              session={session}
              rows={rows}
              indent={!group.scratch}
            />
          ))
        : null}
      {open && hidden > 0 ? (
        <Pressable
          role="button"
          onPress={() => setRevealed(revealed + OLDER_BATCH)}
          style={[styles.more, !group.scratch ? styles.indent : null]}
        >
          <Text size="xs" tone="muted">
            Show {Math.min(hidden, OLDER_BATCH)} more
          </Text>
        </Pressable>
      ) : null}
    </View>
  )
}

/** One Session: its title, then what it is doing or its message count, and how long ago it changed. */
function SessionRow({
  session,
  rows,
  indent = false,
}: {
  session: SessionSummary
  rows: RowContext
  indent?: boolean
}) {
  const colors = useColors()
  const isUnread = rows.unread.has(session.sessionId)
  const pinned = rows.pins.sessions.has(session.sessionId)
  return (
    <Pressable
      role="button"
      accessibilityHint="Hold for more actions"
      onPress={() => rows.onSelect(session.sessionId)}
      onLongPress={() => rows.onSessionActions(session)}
      style={({ pressed }) => [
        styles.row,
        indent ? styles.indent : null,
        pressed ? { backgroundColor: colors.sidebarAccent } : null,
      ]}
    >
      <View style={styles.rowLine}>
        <Text
          size="base"
          weight={isUnread ? 'medium' : 'regular'}
          numberOfLines={1}
          style={styles.sessionTitle}
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
        {pinned ? (
          <View role="img" aria-label="Pinned">
            <Pin size={12} color={colors.mutedForeground} strokeWidth={2} />
          </View>
        ) : null}
      </View>
      <View style={styles.rowLine}>
        <View style={styles.meta}>
          <SessionMeta
            session={session}
            activity={rows.activity.get(session.sessionId)}
            subagents={rows.subagentsRunning.get(session.sessionId) ?? 0}
          />
          {session.archivedAt ? (
            <Text size="xs" tone="muted">
              Archived
            </Text>
          ) : null}
        </View>
        <Text size="xs" tone="muted" style={styles.time}>
          {relativeTime(session.updatedAt)}
        </Text>
      </View>
    </Pressable>
  )
}

function SessionMeta({
  session,
  activity,
  subagents,
}: {
  session: SessionSummary
  activity: SessionActivity | undefined
  subagents: number
}) {
  const colors = useColors()
  const status = (label: string, color: string, spin: boolean, name = label) => (
    <View role="status" aria-label={name} style={styles.mark}>
      {spin ? (
        <Spinner size={12} color={color} />
      ) : (
        <CircleAlert size={12} color={color} strokeWidth={2} />
      )}
      <Text size="xs" numberOfLines={1} style={[styles.markText, { color }]}>
        {label}
      </Text>
    </View>
  )
  if (activity === 'needs-input') return status('Needs input', colors.attention, false)
  if (activity === 'compacting') return status('Compacting', colors.working, true)
  if (subagents > 0) {
    const label = `${subagents} ${subagents === 1 ? 'subagent' : 'subagents'} running`
    return status(label, colors.working, true)
  }
  if (activity === 'working') return status('Working', colors.working, true)
  if (session.worktree) {
    return (
      <View style={styles.mark}>
        <GitBranch size={12} color={colors.mutedForeground} strokeWidth={2} />
        <Text size="xs" tone="muted" mono numberOfLines={1} style={styles.markText}>
          {session.worktree.branch}
        </Text>
      </View>
    )
  }
  if (session.messagesCount !== null) {
    return (
      <View style={styles.mark}>
        <MessageSquare size={12} color={colors.mutedForeground} strokeWidth={2} />
        <Text size="xs" tone="muted" numberOfLines={1} style={styles.markText}>
          {session.messagesCount} {session.messagesCount === 1 ? 'message' : 'messages'}
        </Text>
      </View>
    )
  }
  return null
}

const styles = StyleSheet.create({
  panel: { flex: 1 },
  scroll: { flex: 1, paddingHorizontal: space.sm },
  empty: { paddingHorizontal: space.sm, paddingVertical: space.lg },
  older: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    marginTop: space.xs,
    paddingHorizontal: space.sm,
    minHeight: 44,
    borderRadius: radius.md,
  },
  foldable: { marginTop: space.sm },
  foldHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    paddingHorizontal: space.sm,
    minHeight: 36,
  },
  loose: { marginBottom: space.sm },
  section: { marginBottom: space.sm },
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.sm,
    minHeight: 44,
  },
  rename: { flexDirection: 'row', gap: space.sm, paddingHorizontal: space.lg },
  renameInput: {
    flex: 1,
    height: 44,
    borderWidth: 1,
    borderRadius: radius.md,
    paddingHorizontal: space.md,
    fontFamily: fonts.sans,
    fontSize: fontSize.base,
  },
  sheetButton: {
    minHeight: 44,
    justifyContent: 'center',
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
    borderRadius: radius.md,
  },
  row: {
    gap: 3,
    minHeight: 52,
    justifyContent: 'center',
    paddingHorizontal: space.sm,
    paddingVertical: space.sm,
    borderRadius: radius.lg,
  },
  indent: { paddingLeft: space.md },
  more: { minHeight: 36, justifyContent: 'center', paddingHorizontal: space.sm },
  rowLine: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  title: { flex: 1 },
  sessionTitle: { flex: 1, lineHeight: 21 },
  meta: { flex: 1, minWidth: 0, flexDirection: 'row', alignItems: 'center', gap: space.sm },
  time: { fontVariant: ['tabular-nums'] },
  confirm: { gap: space.md, paddingHorizontal: space.lg },
  confirmButtons: { flexDirection: 'row', gap: space.sm },
  grow: { flex: 1 },
  unread: { width: 6, height: 6, borderRadius: 3 },
  mark: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    flexShrink: 1,
    minWidth: 0,
  },
  markText: { flexShrink: 1, lineHeight: 17 },
})
