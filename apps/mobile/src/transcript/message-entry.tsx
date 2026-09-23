// One transcript entry: the user's message as a bubble, or an assistant turn
// as Markdown, images, folded reasoning and quiet tool rows.
import type { TranscriptBlock, TranscriptEntry } from '@droi/daemon-layer/transcript'
import { Image, StyleSheet, View } from 'react-native'
import { Markdown } from '../markdown/markdown'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { SubagentCard } from './subagent-card'
import { ThinkingSection } from './thinking-section'
import { ToolCluster } from './tool-cluster'

type TextBlock = Extract<TranscriptBlock, { kind: 'text' }>
type ImageBlock = Extract<TranscriptBlock, { kind: 'image' }>

export function MessageEntry({
  entry,
  isStreaming,
  showTime,
}: {
  entry: TranscriptEntry
  isStreaming: boolean
  showTime: boolean
}) {
  const colors = useColors()
  if (entry.role === 'user') {
    const text = entry.blocks
      .filter((b): b is TextBlock => b.kind === 'text')
      .map((b) => b.text)
      .join('\n')
    const images = entry.blocks.filter((b): b is ImageBlock => b.kind === 'image')
    return (
      <View role="article" aria-label="You" style={styles.user}>
        {images.map((image) => (
          <Image
            key={image.id}
            source={{ uri: image.src }}
            alt="Attached image"
            accessibilityLabel="Attached image"
            resizeMode="contain"
            style={[styles.userImage, { borderColor: colors.border }]}
          />
        ))}
        {text ? (
          <View style={[styles.bubble, { backgroundColor: colors.secondary }]}>
            <Text selectable style={styles.bubbleText}>
              {text}
            </Text>
          </View>
        ) : null}
      </View>
    )
  }

  const lastIndex = entry.blocks.length - 1
  return (
    <View role="article" aria-label="Assistant" style={styles.assistant}>
      {entry.blocks.map((block, index) => {
        switch (block.kind) {
          case 'text':
            return (
              <Markdown
                key={block.id}
                text={block.text}
                streaming={isStreaming && index === lastIndex}
              />
            )
          case 'image':
            return (
              <Image
                key={block.id}
                source={{ uri: block.src }}
                accessibilityLabel="Image from Droid"
                resizeMode="contain"
                style={[styles.assistantImage, { borderColor: colors.border }]}
              />
            )
          case 'thinking':
            return (
              <ThinkingSection
                key={block.id}
                text={block.text}
                durationMs={block.durationMs}
                isStreaming={isStreaming}
              />
            )
          case 'tools':
            return <ToolCluster key={block.id} calls={block.calls} />
          case 'subagent':
            return <SubagentCard key={block.id} call={block.call} />
        }
      })}
      {entry.isError ? (
        <Text size="sm" style={{ color: colors.destructiveForeground }}>
          The turn ended with an error.
        </Text>
      ) : null}
      {showTime && entry.createdAt ? (
        <Text tone="muted" size="xs">
          {formatTimestamp(entry.createdAt)}
        </Text>
      ) : null}
    </View>
  )
}

function formatTimestamp(ms: number, now = new Date()): string {
  const date = new Date(ms)
  const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  if (date.toDateString() === now.toDateString()) return time
  return `${date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} ${time}`
}

const styles = StyleSheet.create({
  user: { alignItems: 'flex-end', gap: space.xs, paddingVertical: space.sm },
  bubble: {
    maxWidth: '85%',
    borderRadius: 18,
    paddingHorizontal: space.lg,
    paddingVertical: 10,
  },
  bubbleText: { lineHeight: 22 },
  userImage: {
    width: 180,
    height: 180,
    borderRadius: radius.xl,
    borderWidth: StyleSheet.hairlineWidth,
  },
  assistant: { gap: space.sm, paddingVertical: space.sm },
  assistantImage: {
    width: '100%',
    height: 240,
    borderRadius: radius.xl,
    borderWidth: StyleSheet.hairlineWidth,
  },
})
