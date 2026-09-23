// Above the composer: the Session's task list and the messages the Daemon
// holds for a running turn. Under it: the Workspace and the context meter.
import { formatTokens, type ContextUsage } from '@droi/daemon-layer/use-context-usage'
import {
  queuedText,
  useQueuedMessageActions,
  useQueuedMessages,
  type QueuedMessage,
} from '@droi/daemon-layer/use-queued-messages'
import { useTodos, type TodoItem } from '@droi/daemon-layer/use-todos'
import {
  ChevronDown,
  Circle,
  CircleCheck,
  Clock,
  CornerDownLeft,
  Folder,
  ListChecks,
  X,
} from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, StyleSheet, View } from 'react-native'
import { Spinner } from '../ui/activity'
import Svg, { Circle as SvgCircle } from 'react-native-svg'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { COMPOSER_RADIUS } from './composer'
import { useColors } from '../ui/use-colors'

/** Up to this many queued messages show as a list; more fold into one row. */
const QUEUED_UNFOLDED = 2

export function ComposerShelf({ sessionId }: { sessionId: string }) {
  const colors = useColors()
  const todos = useTodos(sessionId)
  const queued = useQueuedMessages(sessionId)
  const actions = useQueuedMessageActions(sessionId)
  const done = todos.filter((t) => t.status === 'completed').length
  // A finished list has nothing left to steer; it stays in the transcript.
  const showTodos = todos.length > 0 && done < todos.length
  if (!showTodos && queued.length === 0 && !actions.error) return null
  return (
    <View
      style={[styles.shelf, { borderColor: colors.border, backgroundColor: colors.background }]}
    >
      {showTodos ? <TodoPanel todos={todos} done={done} /> : null}
      {actions.error ? (
        <Text role="alert" size="xs" style={[styles.pad, { color: colors.destructiveForeground }]}>
          {actions.error}
        </Text>
      ) : null}
      {queued.length > 0 ? (
        <QueuedMessages queued={queued} onRemove={(id) => void actions.remove(id)} />
      ) : null}
    </View>
  )
}

function TodoPanel({ todos, done }: { todos: TodoItem[]; done: number }) {
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const current =
    todos.find((t) => t.status === 'in_progress') ?? todos.find((t) => t.status === 'pending')
  return (
    <View>
      <Pressable
        role="button"
        aria-label={`Tasks, ${done} of ${todos.length} done`}
        aria-expanded={open}
        onPress={() => setOpen(!open)}
        style={styles.row}
      >
        <ListChecks size={14} color={colors.mutedForeground} strokeWidth={1.75} />
        <Text size="sm" numberOfLines={1} style={styles.fill}>
          {current?.content ?? 'Tasks'}
        </Text>
        <Text tone="muted" size="xs">
          {done}/{todos.length}
        </Text>
        <Chevron open={open} />
      </Pressable>
      {open ? (
        <View role="list" aria-label="Tasks" style={styles.list}>
          {todos.map((todo) => (
            <View
              key={todo.id}
              role="listitem"
              aria-label={`${statusLabel(todo.status)}: ${todo.content}`}
              style={styles.row}
            >
              <StatusIcon status={todo.status} />
              <Text
                size="sm"
                tone={todo.status === 'completed' ? 'muted' : 'default'}
                style={styles.fill}
              >
                {todo.content}
              </Text>
            </View>
          ))}
        </View>
      ) : null}
    </View>
  )
}

function statusLabel(status: TodoItem['status']): string {
  return status === 'completed' ? 'Done' : status === 'in_progress' ? 'In progress' : 'Pending'
}

function StatusIcon({ status }: { status: TodoItem['status'] }) {
  const colors = useColors()
  if (status === 'completed')
    return <CircleCheck size={14} color={colors.success} strokeWidth={2} />
  if (status === 'in_progress') return <Spinner size={14} color={colors.working} />
  return <Circle size={14} color={colors.mutedForeground} strokeWidth={1.5} />
}

function isSteer(message: QueuedMessage): boolean {
  return message.kind !== 'daemon_queued_end_of_loop'
}

function QueuedMessages({
  queued,
  onRemove,
}: {
  queued: QueuedMessage[]
  onRemove: (requestId: string) => void
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
            {isSteer(message) ? 'Next' : 'Queued'}
          </Text>
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
  const next = queued.find(isSteer) ?? queued[0]!
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
  return isSteer(message) ? (
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
  workspace,
  usage,
}: {
  workspace: string | null
  usage: ContextUsage | null
}) {
  const colors = useColors()
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
    borderWidth: StyleSheet.hairlineWidth,
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
    minHeight: 32,
    paddingHorizontal: space.md,
  },
  list: { paddingBottom: space.xs },
  fill: { flex: 1 },
  remove: { padding: space.xs },
  footer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.md,
    height: 28,
    paddingHorizontal: space.sm,
  },
  workspace: { flexDirection: 'row', alignItems: 'center', gap: space.xs, flexShrink: 1 },
  meter: { marginLeft: 'auto', flexDirection: 'row', alignItems: 'center', gap: space.xs },
})
