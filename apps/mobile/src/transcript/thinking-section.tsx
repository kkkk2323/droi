// Reasoning, folded away by default like the web Client's.
import { ChevronRight } from 'lucide-react-native'
import { Pressable, StyleSheet, View } from 'react-native'
import { Markdown } from '../markdown/markdown'
import { Text } from '../ui/primitives'
import { space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { Folded, useFold } from './fold'

export function ThinkingSection({
  text,
  durationMs,
  isStreaming,
}: {
  text: string
  durationMs: number | undefined
  isStreaming: boolean
}) {
  const colors = useColors()
  const fold = useFold()
  const label = durationMs
    ? `Reasoned for ${formatDuration(durationMs)}`
    : isStreaming
      ? 'Reasoning…'
      : 'Reasoning'
  return (
    <View>
      <Pressable
        role="button"
        aria-expanded={fold.open}
        onPress={fold.toggle}
        style={styles.trigger}
      >
        <Text tone="muted" size="sm">
          {label}
        </Text>
        <ChevronRight
          size={14}
          color={colors.mutedForeground}
          style={{ transform: [{ rotate: fold.open ? '90deg' : '0deg' }] }}
        />
      </Pressable>
      <Folded fold={fold}>
        <View style={[styles.panel, { borderLeftColor: colors.border }]}>
          <Markdown text={text} muted />
        </View>
      </Folded>
    </View>
  )
}

function formatDuration(ms: number): string {
  if (ms < 1_000) return `${ms} ms`
  const seconds = Math.round(ms / 1_000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

const styles = StyleSheet.create({
  trigger: { flexDirection: 'row', alignItems: 'center', gap: space.xs, paddingVertical: space.xs },
  panel: { borderLeftWidth: 2, paddingLeft: space.md, marginTop: space.xs },
})
