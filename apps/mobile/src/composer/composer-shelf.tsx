// Above the composer: the messages the Kernel holds for a running turn, and those a
// stop took back. Under it: the Workspace and the context meter.
import { useCompactedNotice } from '@droi/daemon-layer/compaction'
import { formatTokens, type ContextUsage } from '@droi/daemon-layer/use-context-usage'
import {
  isPaused,
  isSteering,
  queuedText,
  useQueuedMessageActions,
  useQueuedMessages,
  type QueuedContent,
  type QueuedMessage,
} from '@droi/daemon-layer/use-queued-messages'
import {
  Check,
  ChevronDown,
  Clock,
  CornerDownLeft,
  Folder,
  Pause,
  Pencil,
  X,
} from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, StyleSheet, View } from 'react-native'
import Svg, { Circle as SvgCircle } from 'react-native-svg'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { COMPOSER_RADIUS } from './composer'
import { useColors } from '../ui/use-colors'

/** Up to this many queued messages show as a list; more fold into one row. */
const QUEUED_UNFOLDED = 2

export function ComposerShelf({
  sessionId,
  onTake,
}: {
  sessionId: string
  /** A paused message taken back off the shelf, for the composer. */
  onTake: (content: QueuedContent) => void
}) {
  const colors = useColors()
  const queued = useQueuedMessages(sessionId)
  const actions = useQueuedMessageActions(sessionId)
  if (queued.length === 0 && !actions.error) return null
  return (
    <View style={[styles.shelf, { borderColor: colors.border, backgroundColor: colors.card }]}>
      {actions.error ? (
        <Text role="alert" size="xs" style={[styles.pad, { color: colors.destructiveForeground }]}>
          {actions.error}
        </Text>
      ) : null}
      {queued.length > 0 ? (
        <QueuedMessages
          queued={queued}
          onRemove={(id) => void actions.remove(id)}
          onTake={(message) =>
            void actions.take(message).then((content) => {
              if (content) onTake(content)
            })
          }
        />
      ) : null}
    </View>
  )
}

function QueuedMessages({
  queued,
  onRemove,
  onTake,
}: {
  queued: QueuedMessage[]
  onRemove: (requestId: string) => void
  onTake: (message: QueuedMessage) => void
}) {
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const list = (
    <View role="list" aria-label="Queued messages">
      {queued.map((message) => (
        <View key={message.requestId} role="listitem" style={styles.row}>
          <QueuedIcon message={message} />
          <Text size="sm" numberOfLines={1} style={styles.fill}>
            {queuedText(message)}
          </Text>
          <Text tone="muted" size="xs">
            {isPaused(message) ? 'Paused' : isSteering(message) ? 'Next' : 'Queued'}
          </Text>
          {isPaused(message) ? (
            <Pressable
              role="button"
              aria-label="Edit queued message"
              hitSlop={8}
              onPress={() => onTake(message)}
              style={styles.remove}
            >
              <Pencil size={14} color={colors.mutedForeground} strokeWidth={1.75} />
            </Pressable>
          ) : null}
          <Pressable
            role="button"
            aria-label="Remove queued message"
            hitSlop={8}
            onPress={() => onRemove(message.requestId)}
            style={styles.remove}
          >
            <X size={14} color={colors.mutedForeground} strokeWidth={1.75} />
          </Pressable>
        </View>
      ))}
    </View>
  )
  if (queued.length <= QUEUED_UNFOLDED) return list
  const next = queued.find(isSteering) ?? queued[0]!
  return (
    <View>
      <Pressable
        role="button"
        aria-label={`Queued messages, ${queued.length}`}
        aria-expanded={open}
        onPress={() => setOpen(!open)}
        style={styles.row}
      >
        {open ? (
          <>
            <Clock size={12} color={colors.mutedForeground} />
            <Text tone="muted" size="sm" style={styles.fill}>
              Queued messages
            </Text>
          </>
        ) : (
          <>
            <QueuedIcon message={next} />
            <Text size="sm" numberOfLines={1} style={styles.fill}>
              {queuedText(next)}
            </Text>
          </>
        )}
        <Text tone="muted" size="xs">
          {queued.length} queued
        </Text>
        <Chevron open={open} />
      </Pressable>
      {open ? list : null}
    </View>
  )
}

function QueuedIcon({ message }: { message: QueuedMessage }) {
  const colors = useColors()
  if (isPaused(message)) return <Pause size={12} color={colors.mutedForeground} />
  return isSteering(message) ? (
    <CornerDownLeft size={12} color={colors.mutedForeground} />
  ) : (
    <Clock size={12} color={colors.mutedForeground} />
  )
}

function Chevron({ open }: { open: boolean }) {
  const colors = useColors()
  return (
    <ChevronDown
      size={14}
      color={colors.mutedForeground}
      style={{ transform: [{ rotate: open ? '180deg' : '0deg' }] }}
    />
  )
}

/** The Workspace's name and how full the context is, under the composer. */
export function ComposerFooter({
  sessionId,
  workspace,
  usage,
}: {
  sessionId: string
  workspace: string | null
  usage: ContextUsage | null
}) {
  const colors = useColors()
  const compacted = useCompactedNotice(sessionId)
  return (
    <View style={styles.footer}>
      {workspace ? (
        <View style={styles.workspace}>
          <Folder size={12} color={colors.mutedForeground} strokeWidth={1.75} />
          <Text tone="muted" size="xs" numberOfLines={1}>
            {workspace.split('/').filter(Boolean).pop() ?? workspace}
          </Text>
        </View>
      ) : null}
      {compacted ? (
        <View role="status" style={styles.workspace}>
          <Check size={12} color={colors.success} strokeWidth={2} />
          <Text size="xs" numberOfLines={1} style={{ color: colors.success }}>
            {compacted.removedCount > 0
              ? `Compacted, ${compacted.removedCount} ${compacted.removedCount === 1 ? 'message' : 'messages'} summarised`
              : 'Compacted'}
          </Text>
        </View>
      ) : null}
      <ContextMeter usage={usage} />
    </View>
  )
}

function ContextMeter({ usage }: { usage: ContextUsage | null }) {
  const colors = useColors()
  if (!usage) return null
  const percent = Math.round(usage.ratio * 100)
  const r = 5.5
  const circumference = 2 * Math.PI * r
  const figures = `${formatTokens(usage.usedTokens)} / ${formatTokens(usage.budgetTokens)} · ${percent}%`
  return (
    <View
      role="meter"
      aria-label="Context used"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      aria-valuetext={`${percent}% of context used: ${formatTokens(usage.usedTokens)} of ${formatTokens(usage.budgetTokens)} tokens`}
      style={styles.meter}
    >
      <Svg width={14} height={14} viewBox="0 0 14 14" style={{ transform: [{ rotate: '-90deg' }] }}>
        <SvgCircle cx={7} cy={7} r={r} fill="none" strokeWidth={2} stroke={colors.border} />
        <SvgCircle
          cx={7}
          cy={7}
          r={r}
          fill="none"
          strokeWidth={2}
          stroke={usage.ratio > 0.8 ? colors.attention : colors.mutedForeground}
          strokeDasharray={`${circumference * usage.ratio} ${circumference}`}
        />
      </Svg>
      <Text tone="muted" size="xs">
        {figures}
      </Text>
    </View>
  )
}

const styles = StyleSheet.create({
  shelf: {
    // Tucked against the composer's straight top edge, between its rounded
    // corners, so the two read as one piece (as in the web Client).
    marginHorizontal: space.md + COMPOSER_RADIUS,
    borderWidth: 1,
    borderBottomWidth: 0,
    borderTopLeftRadius: radius.xl,
    borderTopRightRadius: radius.xl,
    paddingVertical: space.xs,
  },
  pad: { paddingHorizontal: space.md },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 44,
    paddingHorizontal: space.md,
  },
  list: { paddingBottom: space.xs },
  fill: { flex: 1 },
  remove: {
    padding: space.sm,
    minWidth: 36,
    minHeight: 36,
    alignItems: 'center',
    justifyContent: 'center',
  },
  footer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.md,
    minHeight: 28,
    // In line with the text inside the composer above, clear of the edges.
    paddingHorizontal: space.xl,
  },
  workspace: { flexDirection: 'row', alignItems: 'center', gap: space.xs, flexShrink: 1 },
  meter: { marginLeft: 'auto', flexDirection: 'row', alignItems: 'center', gap: space.xs },
})
