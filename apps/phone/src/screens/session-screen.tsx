// An open Session: its transcript, and what came before a compaction when
// the reader asks for it. Loading it subscribes this phone to its
// notifications, which is also what makes its activity show in the list.
import { takePendingPrompt } from '@droi/daemon-layer/pending-prompt'
import { LOAD_STATE } from '@droi/daemon-layer/sdk-enums'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { COMPACT_COMMAND, useCompact } from '@droi/daemon-layer/use-compact'
import { useContextUsage } from '@droi/daemon-layer/use-context-usage'
import { useGitChanges } from '@droi/daemon-layer/use-git-changes'
import { usePrompts } from '@droi/daemon-layer/use-prompts'
import { useSession } from '@droi/daemon-layer/use-session'
import { useSessionSettings } from '@droi/daemon-layer/use-session-settings'
import { useSlashItems } from '@droi/daemon-layer/use-slash-items'
import { useTurn } from '@droi/daemon-layer/use-turn'
import { ChevronUp } from 'lucide-react-native'
import { useEffect, useState } from 'react'
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  StyleSheet,
  View,
} from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { Composer, type Submission } from '../composer/composer'
import { ComposerFooter, ComposerShelf } from '../composer/composer-shelf'
import { hasPrompt, PromptArea } from '../composer/prompt-cards'
import { SessionSettingsBar } from '../composer/session-settings'
import { GitChangesButton } from '../transcript/git-changes'
import { TranscriptView } from '../transcript/transcript-view'
import { Text } from '../ui/primitives'
import { ScreenHeader } from '../ui/screen-header'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionScreen({
  session,
  parent,
  onContinued,
  drawerOpen,
  onOpenDrawer,
}: {
  session: SessionSummary
  /** `/compact` moved the conversation to a new Session; show that one. */
  onContinued: (sessionId: string) => void
  /** The Session this one continues after a compaction, when listed. */
  parent: SessionSummary | null
  drawerOpen: boolean
  onOpenDrawer: () => void
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const view = useSession(session.sessionId)
  const loaded = view.loadState === LOAD_STATE.loaded
  const turn = useTurn(session.sessionId)
  const settings = useSessionSettings(session.sessionId)
  const contextUsage = useContextUsage(session.sessionId, { loaded, modelId: settings.modelId })
  const slashItems = useSlashItems(session.sessionId)
  const prompts = usePrompts(session.sessionId)
  const compaction = useCompact(session.sessionId, session.tags)
  const isRunning = view.workingState !== 'idle' || compaction.isCompacting
  const gitChanges = useGitChanges(session.sessionId, { loaded, running: isRunning })
  const [sentCount, setSentCount] = useState(0)
  // A message typed on the New session page goes out once the Session can take it.
  const send = turn.send
  useEffect(() => {
    if (!loaded) return
    const prompt = takePendingPrompt(session.sessionId)
    if (prompt) void send(prompt.text, { images: prompt.images })
  }, [loaded, session.sessionId, send])
  const submit = ({ text, images, placement }: Submission) => {
    setSentCount((n) => n + 1)
    const command = COMPACT_COMMAND.exec(text.trim())
    if (command && images.length === 0) {
      void compaction.compact(command[1]).then((next) => {
        if (next) onContinued(next)
      })
      return
    }
    void turn.send(text, { images, placement })
  }
  const [showEarlier, setShowEarlier] = useState(false)
  const earlier = useSession(showEarlier && parent ? parent.sessionId : null)

  const lead = parent ? (
    showEarlier ? (
      earlier.loadState !== LOAD_STATE.loaded ? (
        <View style={styles.leadRow}>
          <ActivityIndicator size="small" color={colors.mutedForeground} />
          <Text tone="muted" size="xs">
            Loading earlier messages…
          </Text>
        </View>
      ) : null
    ) : (
      <Pressable role="button" onPress={() => setShowEarlier(true)} style={styles.leadRow}>
        <ChevronUp size={14} color={colors.mutedForeground} />
        <Text tone="muted" size="xs">
          Continued from “{parent.title}” · Show earlier messages
        </Text>
      </Pressable>
    )
  ) : null

  return (
    <KeyboardAvoidingView
      style={styles.fill}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
    >
      <ScreenHeader
        title={session.title}
        drawerOpen={drawerOpen}
        onOpenDrawer={onOpenDrawer}
        trailing={<GitChangesButton changes={gitChanges} />}
      />
      {view.loadError ? (
        <Text role="alert" style={[styles.message, { color: colors.destructiveForeground }]}>
          {view.loadError}
        </Text>
      ) : !loaded && view.messages.length === 0 ? (
        <View style={styles.loading}>
          <ActivityIndicator size="small" color={colors.mutedForeground} />
          <Text tone="muted" size="sm">
            Loading session…
          </Text>
        </View>
      ) : (
        <TranscriptView
          messages={view.messages}
          earlierMessages={showEarlier ? earlier.messages : undefined}
          workingState={compaction.isCompacting ? 'compacting_conversation' : view.workingState}
          lead={lead}
          scrollToEndKey={sentCount}
        />
      )}
      <View style={{ paddingBottom: Math.max(insets.bottom, space.sm) }}>
        <ComposerShelf sessionId={session.sessionId} />
        {hasPrompt(prompts) ? (
          // The Prompt stands in for the composer; the draft comes back after.
          <PromptArea sessionId={session.sessionId} />
        ) : (
          <Composer
            isRunning={isRunning}
            disabled={!loaded}
            onSend={submit}
            onCancel={() => void turn.cancel()}
            error={turn.sendError ?? compaction.error}
            draftKey={session.sessionId}
            slashItems={slashItems}
            accessory={<SessionSettingsBar sessionId={session.sessionId} />}
          />
        )}
        <ComposerFooter workspace={session.cwd} usage={contextUsage} />
      </View>
    </KeyboardAvoidingView>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  message: { padding: space.lg },
  loading: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: space.sm,
  },
  leadRow: { flexDirection: 'row', alignItems: 'center', gap: space.xs, paddingVertical: space.xs },
})
