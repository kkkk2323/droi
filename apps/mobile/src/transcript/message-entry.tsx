// The transcript's rows: the user's message as a bubble, and each block of an
// assistant turn (Markdown, an image, folded reasoning, quiet tool rows) as a
// row of its own. A turn is split so the list can keep a block the reader is
// on in place while a later block of the same turn grows.
import {
  formatTurnEnd,
  type TranscriptBlock,
  type TranscriptEntry,
  type TurnEnd,
} from '@droi/daemon-layer/transcript'
import { memo } from 'react'
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

export const UserMessage = memo(function UserMessage({ entry }: { entry: TranscriptEntry }) {
  const colors = useColors()
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
})

export const AssistantBlock = memo(function AssistantBlock({
  block,
  turnStreaming,
  first,
  last,
  isError,
  turnEnd,
}: {
  block: TranscriptBlock
  /** The turn this block belongs to is still being written. */
  turnStreaming: boolean
  first: boolean
  /** The turn's last block, which carries how the turn ended. */
  last: boolean
  isError: boolean
  /** When the turn ended and how long it took, shown under its last block. */
  turnEnd: TurnEnd | null
}) {
  const colors = useColors()
  return (
    <View
      role="article"
      aria-label="Assistant"
      style={[styles.assistant, first ? styles.assistantFirst : null]}
    >
      {renderBlock(block, turnStreaming, last, colors.border)}
      {last && isError ? (
        <Text size="sm" style={{ color: colors.destructiveForeground }}>
          The turn ended with an error.
        </Text>
      ) : null}
      {last && turnEnd && turnEnd.endedAt ? (
        <Text tone="muted" size="xs">
          {formatTurnEnd(turnEnd)}
        </Text>
      ) : null}
    </View>
  )
})

function renderBlock(
  block: TranscriptBlock,
  turnStreaming: boolean,
  last: boolean,
  border: string,
) {
  switch (block.kind) {
    case 'text':
      return <Markdown text={block.text} streaming={turnStreaming && last} />
    case 'image':
      return (
        <Image
          source={{ uri: block.src }}
          accessibilityLabel="Image from Droid"
          resizeMode="contain"
          style={[styles.assistantImage, { borderColor: border }]}
        />
      )
    case 'thinking':
      return (
        <ThinkingSection
          text={block.text}
          durationMs={block.durationMs}
          isStreaming={turnStreaming}
        />
      )
    case 'tools':
      return <ToolCluster calls={block.calls} />
    case 'subagent':
      return <SubagentCard call={block.call} />
  }
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
  // The turn's blocks sit space.sm apart, with space.sm above and below the turn.
  assistant: { gap: space.sm, paddingBottom: space.sm },
  assistantFirst: { paddingTop: space.sm },
  assistantImage: {
    width: '100%',
    height: 240,
    borderRadius: radius.xl,
    borderWidth: StyleSheet.hairlineWidth,
  },
})
