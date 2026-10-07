// New session: pick a recent Workspace, None (a fresh Scratch Workspace) or
// type a path, the model and friends, and send the first message, or nothing. As soon as the Workspace is
// known a Draft Session opens so "/" offers its commands and skills; the
// first send takes it over, and leaving without sending closes it.
import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import { setPendingPrompt } from '@droi/daemon-layer/pending-prompt'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { closeDraftSession, useDraftSession } from '@droi/daemon-layer/use-draft-session'
import {
  recentWorkspaces,
  useNewSession,
  type RecentWorkspace,
} from '@droi/daemon-layer/use-new-session'
import { useWorkspaceChoice, type WorkspacePick } from '@droi/daemon-layer/use-workspace-choice'
import { useSessionDefaults } from '@droi/daemon-layer/use-session-defaults'
import { useSlashItems, type SlashItem } from '@droi/daemon-layer/use-slash-items'
import {
  usePreference,
  worktreeByDefault,
  worktreeLifecyclePreference,
} from '@droi/daemon-layer/local-preference'
import { useGitBranches } from '@droi/daemon-layer/use-git-branches'
import {
  WORKTREE_LIFECYCLES,
  WORKTREE_LIFECYCLE_DESCRIPTIONS,
  WORKTREE_LIFECYCLE_LABELS,
  type SessionWorktree,
} from '@droi/daemon-layer/worktree'
import { defaultToolMode, newSessionToolMode, type ToolMode } from '@droi/daemon-layer/tool-mode'
import { useRouter } from 'expo-router'
import { useHeaderHeight } from 'expo-router/react-navigation'
import { ChevronDown, GitBranch } from 'lucide-react-native'
import { useEffect, useRef, useState } from 'react'
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { Composer, type Submission } from '../composer/composer'
import { ConnectionGate } from '../connection/connection-notices'
import {
  sessionPath,
  useConnectedComputer,
  type ConnectedComputer,
} from '../sessions/connected-computer'
import { SettingsControls } from '../composer/session-settings'
import { ListSwitch } from '../ui/list'
import { Button, Text } from '../ui/primitives'
import { Sheet, SheetOption } from '../ui/sheet'
import { TextScale } from '../ui/text-scale'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const NO_BUILTINS: SlashItem[] = []
const NO_RECENT: RecentWorkspace[] = []

export function NewSessionScreen({
  initialPick = null,
}: {
  /** Preselected Workspace, or None (opened from a group's actions). */
  initialPick?: WorkspacePick | null
}) {
  const connected = useConnectedComputer()
  if (!connected) return null
  return (
    <ConnectionGate computer={connected.computer}>
      <NewSession initialPick={initialPick} connected={connected} />
    </ConnectionGate>
  )
}

function NewSession({
  initialPick,
  connected,
}: {
  initialPick: WorkspacePick | null
  connected: ConnectedComputer
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const headerHeight = useHeaderHeight()
  const router = useRouter()
  const { sessions } = connected
  // Null while the Session list is on its way.
  const known = sessions.live || !sessions.isPending ? recentWorkspaces(sessions.sessions) : null
  // The Session as it stands before the Daemon lists it, which it does only once written to.
  const onCreated = (session: SessionSummary) => {
    connected.started(session)
    router.replace(sessionPath(session.sessionId))
  }
  const connection = useDaemonConnection()
  const { create, isCreating, error } = useNewSession()
  const choice = useWorkspaceChoice(known, initialPick)
  const recent = known ?? NO_RECENT
  const workspace = choice.workspace
  const scratch = choice.pick?.kind === 'scratch'
  const typing = choice.pick?.kind === 'other'
  const [path, setPath] = useState('')
  const [picking, setPicking] = useState(false)

  // Worktrees are offered for a recent Workspace that is a Git repository. A
  // typed path is only checked on Start, so it always works in place.
  const pickedPath = choice.pick?.kind === 'recent' ? choice.pick.path : null
  const git = useGitBranches(pickedPath)
  const canWorktree = git?.isGitRepository === true
  const [startWithWorktree, setStartWithWorktree] = usePreference(worktreeByDefault)
  const [lifecycle, setLifecycle] = usePreference(worktreeLifecyclePreference)
  // This page's own choice; until made, the device default decides.
  const [placement, setPlacement] = useState<'local' | 'worktree' | null>(null)
  const [base, setBase] = useState<{ path: string; branch: string } | null>(null)
  const [worktreeSheet, setWorktreeSheet] = useState<'closed' | 'options' | 'branches'>('closed')
  const inWorktree =
    canWorktree && (placement ?? (startWithWorktree ? 'worktree' : 'local')) === 'worktree'
  const chosenBase = base?.path === pickedPath ? base.branch : null
  const baseBranch = chosenBase ?? git?.currentBranch ?? null

  const defaults = useSessionDefaults()
  const [overrides, setOverrides] = useState<{
    modelId?: string
    reasoningEffort?: string
    autonomyLevel?: string
    toolMode?: ToolMode
  }>({})
  const [toolModeDefault] = usePreference(defaultToolMode)
  const pickModel = (modelId: string) => {
    // A new model may not offer the current effort; fall back to its list.
    const model = defaults.models.find((m) => m.id === modelId)
    const effort = settings.reasoningEffort
    const keep = effort && model?.reasoningEfforts.includes(effort)
    setOverrides({
      ...overrides,
      modelId,
      ...(keep ? {} : { reasoningEffort: model?.reasoningEfforts[0] ?? undefined }),
    })
  }

  const draft = useDraftSession(workspace, choice.tags)
  const toolMode = newSessionToolMode(overrides.toolMode, toolModeDefault, draft.toolMode)
  const settings = {
    models: defaults.models,
    modelId: overrides.modelId ?? defaults.modelId,
    reasoningEffort: overrides.reasoningEffort ?? defaults.reasoningEffort,
    autonomyLevel: overrides.autonomyLevel ?? defaults.autonomyLevel,
    toolExecutionMode: toolMode.requested,
  }
  const slashItems = useSlashItems(draft.sessionId, NO_BUILTINS)

  // Leaving without sending closes the draft; a send has taken it over already.
  const taking = useRef(false)
  useEffect(
    () => () => {
      if (!taking.current) closeDraftSession(connection)
    },
    [connection],
  )

  const start = async (target: string, prompt?: Submission) => {
    taking.current = true
    const inTree = inWorktree && target === workspace
    let made: SessionWorktree | null = null
    let sessionId: string | null
    if (inTree) {
      // A draft is in the Workspace itself; the worktree is made by a Session of its own.
      const created = await create(target, settings, choice.tags, {
        lifecycle,
        promptSlug: prompt?.text ?? '',
        ...(chosenBase ? { baseBranch: chosenBase } : {}),
      })
      sessionId = created?.sessionId ?? null
      made = created?.worktree ?? null
      if (created) closeDraftSession(connection)
    } else {
      sessionId =
        (await draft.take(target, settings)) ??
        (await create(target, settings, choice.tags))?.sessionId ??
        null
    }
    if (!sessionId) {
      taking.current = false
      return
    }
    if (prompt && (prompt.text.trim() || prompt.images.length > 0)) {
      setPendingPrompt(sessionId, { text: prompt.text, images: prompt.images })
    }
    onCreated({
      sessionId,
      title: 'Untitled session',
      cwd: made?.path ?? target.trim(),
      repoRoot: made?.repoRoot ?? null,
      worktree: made,
      updatedAt: Math.floor(Date.now() / 1000),
      messagesCount: 0,
      archivedAt: null,
      tags: choice.tags,
      parentId: null,
      callingSessionId: null,
      callingToolUseId: null,
    })
  }

  const label = pickedPath
    ? (recent.find((w) => w.path === pickedPath)?.label ?? name(pickedPath))
    : null
  const failure = error ?? choice.error

  return (
    <KeyboardAvoidingView
      role="region"
      aria-label="New session"
      style={styles.fill}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      keyboardVerticalOffset={headerHeight}
    >
      <ScrollView contentContainerStyle={styles.body} keyboardShouldPersistTaps="handled">
        <View style={styles.question}>
          <Text role="heading" aria-level={2} size="xl" weight="medium" style={styles.center}>
            {label
              ? 'What do you want to build in'
              : scratch
                ? 'What’s on your mind?'
                : 'Where should Droid work?'}
          </Text>
          {label || scratch || known ? (
            <Pressable
              role="button"
              aria-label="Workspace"
              aria-haspopup="dialog"
              onPress={() => setPicking(true)}
              style={[styles.workspace, { borderBottomColor: colors.mutedForeground }]}
            >
              <Text
                size={scratch ? 'base' : 'xl'}
                weight="medium"
                tone={scratch ? 'muted' : undefined}
              >
                {label ? `${label}?` : scratch ? 'Workspace: None' : 'Choose a workspace'}
              </Text>
              <ChevronDown size={16} color={colors.mutedForeground} />
            </Pressable>
          ) : null}
        </View>
        {canWorktree ? (
          <Pressable
            role="button"
            aria-label="Worktree"
            aria-haspopup="dialog"
            onPress={() => setWorktreeSheet('options')}
            style={styles.worktree}
          >
            <GitBranch size={14} color={colors.mutedForeground} />
            <Text size="sm" tone="muted" numberOfLines={1} style={styles.worktreeText}>
              {inWorktree
                ? `New worktree · ${WORKTREE_LIFECYCLE_LABELS[lifecycle]}${baseBranch ? ` · from ${baseBranch}` : ''}`
                : 'Work locally'}
            </Text>
            <ChevronDown size={14} color={colors.mutedForeground} />
          </Pressable>
        ) : null}
        {typing ? (
          <View style={styles.pathRow}>
            <TextInput
              aria-label="Workspace path"
              value={path}
              onChangeText={setPath}
              placeholder="/Users/you/projects/app"
              placeholderTextColor={colors.mutedForeground}
              autoCapitalize="none"
              autoCorrect={false}
              onSubmitEditing={() => void start(path)}
              style={[
                styles.path,
                {
                  borderColor: colors.input,
                  color: colors.foreground,
                  backgroundColor: colors.background,
                },
              ]}
            />
            <Button
              label="Start"
              busy={isCreating}
              disabled={!path.trim()}
              onPress={() => void start(path)}
            />
          </View>
        ) : null}
        {isCreating && inWorktree ? (
          <Text role="status" size="sm" tone="muted" style={styles.center}>
            Creating worktree…
          </Text>
        ) : null}
        {failure ? (
          <Text
            role="alert"
            size="sm"
            style={[styles.center, { color: colors.destructiveForeground }]}
          >
            {failure}
          </Text>
        ) : null}
      </ScrollView>
      <View style={{ paddingBottom: Math.max(insets.bottom, space.sm) }}>
        <TextScale>
          <Composer
            isRunning={false}
            disabled={!workspace || isCreating || draft.isTaking}
            allowEmpty
            placeholder="Do anything…"
            sendLabel="Start session"
            onSend={(submission) => {
              if (workspace) void start(workspace, submission)
            }}
            onCancel={() => {}}
            error={null}
            slashItems={slashItems}
            accessory={
              <SettingsControls
                settings={settings}
                onModel={pickModel}
                onReasoningEffort={(reasoningEffort) =>
                  setOverrides({ ...overrides, reasoningEffort })
                }
                onAutonomyLevel={(autonomyLevel) => setOverrides({ ...overrides, autonomyLevel })}
                toolMode={{
                  value: toolMode.shown,
                  onChange: (mode) => setOverrides({ ...overrides, toolMode: mode }),
                }}
              />
            }
          />
        </TextScale>
        {workspace ? (
          <Text tone="muted" size="xs" numberOfLines={1} style={styles.footer}>
            {workspace}
          </Text>
        ) : null}
      </View>
      <Sheet
        visible={worktreeSheet !== 'closed'}
        title={worktreeSheet === 'branches' ? 'Base branch' : 'Worktree'}
        onClose={() => setWorktreeSheet('closed')}
      >
        {worktreeSheet === 'branches' ? (
          <>
            <ScrollView style={styles.branches}>
              <View role="radiogroup" aria-label="Base branches">
                {(git?.branches ?? []).map((branch) => (
                  <SheetOption
                    key={branch}
                    label={branch}
                    description={branch === git?.currentBranch ? 'Current branch' : undefined}
                    checked={branch === baseBranch}
                    onPress={() => {
                      if (pickedPath) setBase({ path: pickedPath, branch })
                      setWorktreeSheet('options')
                    }}
                  />
                ))}
              </View>
            </ScrollView>
            <Button
              label="Back"
              variant="ghost"
              onPress={() => setWorktreeSheet('options')}
              style={styles.sheetAction}
            />
          </>
        ) : (
          <>
            <View role="radiogroup" aria-label="Where to work">
              <SheetOption
                label="Work locally"
                description="In the Workspace's own folder."
                checked={!inWorktree}
                onPress={() => setPlacement('local')}
              />
              <SheetOption
                label="New worktree"
                description="A separate checkout on a new branch, so the Workspace stays as it is."
                checked={inWorktree}
                onPress={() => setPlacement('worktree')}
              />
            </View>
            {inWorktree ? (
              <>
                <View role="radiogroup" aria-label="Lifecycle">
                  {WORKTREE_LIFECYCLES.map((each) => (
                    <SheetOption
                      key={each}
                      label={WORKTREE_LIFECYCLE_LABELS[each]}
                      description={WORKTREE_LIFECYCLE_DESCRIPTIONS[each]}
                      checked={each === lifecycle}
                      onPress={() => setLifecycle(each)}
                    />
                  ))}
                </View>
                <Pressable
                  role="button"
                  aria-label="Base branch"
                  onPress={() => setWorktreeSheet('branches')}
                  style={styles.baseBranch}
                >
                  <Text>Base branch</Text>
                  <Text tone="muted" mono numberOfLines={1} style={styles.worktreeText}>
                    {baseBranch ?? 'Default'}
                  </Text>
                </Pressable>
              </>
            ) : null}
            <ListSwitch
              label="Start with a worktree by default"
              value={startWithWorktree}
              onChange={(value) => {
                setStartWithWorktree(value)
                setPlacement(null)
              }}
            />
          </>
        )}
      </Sheet>
      <Sheet visible={picking} title="Workspace" onClose={() => setPicking(false)}>
        <View role="radiogroup" aria-label="Recent workspaces">
          <SheetOption
            label="None"
            checked={scratch}
            onPress={() => {
              setPicking(false)
              choice.choose({ kind: 'scratch' })
            }}
          />
          {recent.map((w) => (
            <SheetOption
              key={w.path}
              label={w.label}
              checked={w.path === pickedPath}
              onPress={() => {
                setPicking(false)
                choice.choose({ kind: 'recent', path: w.path })
              }}
            />
          ))}
        </View>
        <Pressable
          role="button"
          onPress={() => {
            setPicking(false)
            choice.choose({ kind: 'other' })
          }}
          style={styles.other}
        >
          <Text>Other folder…</Text>
        </Pressable>
      </Sheet>
    </KeyboardAvoidingView>
  )
}

function name(path: string): string {
  return path.split('/').filter(Boolean).pop() ?? path
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  body: { flexGrow: 1, justifyContent: 'center', padding: space.xl, gap: space.lg },
  question: { alignItems: 'center', gap: space.xs },
  center: { textAlign: 'center' },
  workspace: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    borderBottomWidth: 1,
    borderStyle: 'dashed',
  },
  worktree: {
    flexDirection: 'row',
    alignItems: 'center',
    alignSelf: 'center',
    gap: space.xs,
    minHeight: 44,
    maxWidth: '100%',
  },
  worktreeText: { flexShrink: 1 },
  baseBranch: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: space.md,
    minHeight: 44,
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
  },
  branches: { maxHeight: 320 },
  sheetAction: { marginHorizontal: space.lg },
  pathRow: { flexDirection: 'row', gap: space.sm },
  path: {
    flex: 1,
    height: 40,
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    fontFamily: fonts.mono,
    fontSize: fontSize.sm,
  },
  footer: { paddingHorizontal: space.xl, paddingTop: space.xs },
  other: { minHeight: 44, justifyContent: 'center', paddingHorizontal: space.xl },
})
