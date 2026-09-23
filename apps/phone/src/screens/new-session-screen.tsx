// New session: pick a recent Workspace (or type a path), the model and
// friends, and send the first message, or nothing. As soon as the Workspace is
// known a Draft Session opens so "/" offers its commands and skills; the
// first send takes it over, and leaving without sending closes it.
import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import { setPendingPrompt } from '@droi/daemon-layer/pending-prompt'
import { closeDraftSession, useDraftSession } from '@droi/daemon-layer/use-draft-session'
import { useNewSession, type RecentWorkspace } from '@droi/daemon-layer/use-new-session'
import { useSessionDefaults } from '@droi/daemon-layer/use-session-defaults'
import { useSlashItems, type SlashItem } from '@droi/daemon-layer/use-slash-items'
import { ChevronDown } from 'lucide-react-native'
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
import { SettingsControls } from '../composer/session-settings'
import { Button, Text } from '../ui/primitives'
import { ScreenHeader } from '../ui/screen-header'
import { Sheet, SheetOption } from '../ui/sheet'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const NO_BUILTINS: SlashItem[] = []

export function NewSessionScreen({
  initialWorkspace = null,
  recent,
  onCreated,
  drawerOpen,
  onOpenDrawer,
}: {
  /** Preselected Workspace (opened from a Workspace group's actions). */
  initialWorkspace?: string | null
  recent: RecentWorkspace[]
  onCreated: (sessionId: string) => void
  drawerOpen: boolean
  onOpenDrawer: () => void
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const connection = useDaemonConnection()
  const { create, isCreating, error } = useNewSession()
  const [choice, setChoice] = useState<{ kind: 'recent'; path: string } | { kind: 'other' } | null>(
    initialWorkspace ? { kind: 'recent', path: initialWorkspace } : null,
  )
  const workspace =
    choice === null ? (recent[0]?.path ?? null) : choice.kind === 'recent' ? choice.path : null
  const typing = choice?.kind === 'other' || (choice === null && recent.length === 0)
  const [path, setPath] = useState('')
  const [picking, setPicking] = useState(false)

  const defaults = useSessionDefaults()
  const [overrides, setOverrides] = useState<{
    modelId?: string
    reasoningEffort?: string
    autonomyLevel?: string
  }>({})
  const settings = {
    models: defaults.models,
    modelId: overrides.modelId ?? defaults.modelId,
    reasoningEffort: overrides.reasoningEffort ?? defaults.reasoningEffort,
    autonomyLevel: overrides.autonomyLevel ?? defaults.autonomyLevel,
  }
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

  const draft = useDraftSession(workspace)
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
    const sessionId = (await draft.take(target, settings)) ?? (await create(target, settings))
    if (!sessionId) {
      taking.current = false
      return
    }
    if (prompt && (prompt.text.trim() || prompt.images.length > 0)) {
      setPendingPrompt(sessionId, { text: prompt.text, images: prompt.images })
    }
    onCreated(sessionId)
  }

  const label = workspace
    ? (recent.find((w) => w.path === workspace)?.label ?? name(workspace))
    : null

  return (
    <KeyboardAvoidingView
      role="region"
      aria-label="New session"
      style={styles.fill}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
    >
      <ScreenHeader title="New session" drawerOpen={drawerOpen} onOpenDrawer={onOpenDrawer} />
      <ScrollView contentContainerStyle={styles.body} keyboardShouldPersistTaps="handled">
        <View style={styles.question}>
          <Text role="heading" aria-level={2} size="xl" weight="medium" style={styles.center}>
            {label ? 'What do you want to build in' : 'Where should Droid work?'}
          </Text>
          {label || recent.length > 0 ? (
            <Pressable
              role="button"
              aria-label="Workspace"
              aria-haspopup="dialog"
              onPress={() => setPicking(true)}
              style={[styles.workspace, { borderBottomColor: colors.mutedForeground }]}
            >
              <Text size="xl" weight="medium">
                {label ? `${label}?` : 'Choose a workspace'}
              </Text>
              <ChevronDown size={16} color={colors.mutedForeground} />
            </Pressable>
          ) : null}
        </View>
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
        {error ? (
          <Text
            role="alert"
            size="sm"
            style={[styles.center, { color: colors.destructiveForeground }]}
          >
            {error}
          </Text>
        ) : null}
      </ScrollView>
      <View style={{ paddingBottom: Math.max(insets.bottom, space.sm) }}>
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
            />
          }
        />
        {workspace ? (
          <Text tone="muted" size="xs" numberOfLines={1} style={styles.footer}>
            {workspace}
          </Text>
        ) : null}
      </View>
      <Sheet visible={picking} title="Workspace" onClose={() => setPicking(false)}>
        <View role="radiogroup" aria-label="Recent workspaces">
          {recent.map((w) => (
            <SheetOption
              key={w.path}
              label={w.label}
              checked={w.path === workspace}
              onPress={() => {
                setPicking(false)
                setChoice({ kind: 'recent', path: w.path })
              }}
            />
          ))}
        </View>
        <Pressable
          role="button"
          onPress={() => {
            setPicking(false)
            setChoice({ kind: 'other' })
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
  footer: { paddingHorizontal: space.lg, paddingTop: space.xs },
  other: { minHeight: 44, justifyContent: 'center', paddingHorizontal: space.xl },
})
