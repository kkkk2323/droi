// An open Session: its transcript, and what came before a compaction when
// the reader asks for it. Loading it subscribes this phone to its
// notifications, which is also what makes its activity show in the list.
import { LOAD_STATE } from '@droi/daemon-layer/sdk-enums'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { useSession } from '@droi/daemon-layer/use-session'
import { ChevronUp } from 'lucide-react-native'
import { useState } from 'react'
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native'
import { TranscriptView } from '../transcript/transcript-view'
import { Text } from '../ui/primitives'
import { ScreenHeader } from '../ui/screen-header'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SessionScreen({
  session,
  parent,
  drawerOpen,
  onOpenDrawer,
}: {
  session: SessionSummary
  /** The Session this one continues after a compaction, when listed. */
  parent: SessionSummary | null
  drawerOpen: boolean
  onOpenDrawer: () => void
}) {
  const colors = useColors()
  const view = useSession(session.sessionId)
  const loaded = view.loadState === LOAD_STATE.loaded
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
    <View style={styles.fill}>
      <ScreenHeader title={session.title} drawerOpen={drawerOpen} onOpenDrawer={onOpenDrawer} />
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
          workingState={view.workingState}
          lead={lead}
        />
      )}
    </View>
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
