// A Task call, like the web Client's card: the subagent and its task, how the
// run went, a way into the subagent's Session, and the prompt and report
// folded underneath.
import {
  formatRunDuration,
  subagentName,
  useSubagentLink,
  type TaskState,
} from '@droi/daemon-layer/subagents'
import type { ToolCall } from '@droi/daemon-layer/transcript'
import {
  ArrowUpRight,
  Bot,
  Check,
  ChevronDown,
  ChevronRight,
  CircleDashed,
  CircleSlash,
  CircleX,
} from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { Pressable, StyleSheet, View } from 'react-native'
import { Markdown } from '../markdown/markdown'
import { Spinner } from '../ui/activity'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const STATE_LABELS: Record<TaskState, string> = {
  pending: 'Starting',
  running: 'Running',
  launched: 'Started in background',
  completed: 'Completed',
  failed: 'Failed',
  cancelled: 'Cancelled',
}

export function SubagentCard({ call }: { call: ToolCall }) {
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const link = useSubagentLink(call)
  const name = subagentName(link.request.subagentType)
  const facts = [
    link.run?.toolUseCount != null ? plural(link.run.toolUseCount, 'tool') : null,
    link.run?.durationMs != null ? formatRunDuration(link.run.durationMs) : null,
  ].filter((fact): fact is string => fact !== null)

  return (
    <View
      role="group"
      aria-label={`${name}: ${link.request.description || 'Task'}`}
      style={[styles.card, { borderColor: colors.border, backgroundColor: colors.card }]}
    >
      <View style={styles.head}>
        <View style={[styles.badge, { backgroundColor: colors.accent }]}>
          <Bot size={16} color={colors.mutedForeground} strokeWidth={1.75} />
        </View>
        <View style={styles.what}>
          <Text size="sm" numberOfLines={1}>
            <Text size="sm" weight="medium">
              {name}
            </Text>
            {link.request.description ? (
              <Text size="sm" tone="muted">
                {'  '}
                {link.request.description}
              </Text>
            ) : null}
          </Text>
          <View style={styles.facts}>
            <StateMark state={link.state} />
            {facts.map((fact) => (
              <Text key={fact} tone="muted" size="xs">
                · {fact}
              </Text>
            ))}
          </View>
        </View>
        {link.open ? (
          <Pressable
            role="button"
            aria-label="Open subagent session"
            hitSlop={6}
            onPress={link.open}
            style={({ pressed }) => [
              styles.open,
              pressed ? { backgroundColor: colors.accent } : null,
            ]}
          >
            <Text tone="muted" size="xs" weight="medium">
              Open
            </Text>
            <ArrowUpRight size={14} color={colors.mutedForeground} strokeWidth={2} />
          </Pressable>
        ) : null}
      </View>
      <Pressable
        role="button"
        aria-label="Details"
        aria-expanded={open}
        onPress={() => setOpen(!open)}
        style={({ pressed }) => [
          styles.toggle,
          { borderTopColor: colors.border },
          pressed ? { backgroundColor: colors.accent } : null,
        ]}
      >
        {open ? (
          <ChevronDown size={12} color={colors.mutedForeground} />
        ) : (
          <ChevronRight size={12} color={colors.mutedForeground} />
        )}
        <Text tone="muted" size="xs">
          Details
        </Text>
      </Pressable>
      {open ? (
        <View style={[styles.details, { borderTopColor: colors.border }]}>
          <Section title="Prompt">
            <Text selectable size="sm" style={styles.prompt}>
              {link.request.prompt}
            </Text>
          </Section>
          {link.report ? (
            <Section title="Report">
              <Markdown text={link.report} />
            </Section>
          ) : null}
        </View>
      ) : null}
    </View>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <View role="region" aria-label={title} style={styles.section}>
      <Text tone="muted" size="xs" weight="medium" style={styles.sectionTitle}>
        {title.toUpperCase()}
      </Text>
      {children}
    </View>
  )
}

function StateMark({ state }: { state: TaskState }) {
  const colors = useColors()
  const running = state === 'running' || state === 'pending'
  const color = running
    ? colors.working
    : state === 'completed'
      ? colors.success
      : state === 'failed'
        ? colors.destructiveForeground
        : colors.mutedForeground
  return (
    <View role={running ? 'status' : undefined} style={styles.state}>
      {running ? (
        <Spinner size={12} color={color} />
      ) : state === 'completed' ? (
        <Check size={12} color={color} strokeWidth={2} />
      ) : state === 'failed' ? (
        <CircleX size={12} color={color} strokeWidth={2} />
      ) : state === 'cancelled' ? (
        <CircleSlash size={12} color={color} strokeWidth={2} />
      ) : (
        <CircleDashed size={12} color={color} strokeWidth={2} />
      )}
      <Text size="xs" style={{ color }}>
        {STATE_LABELS[state]}
      </Text>
    </View>
  )
}

function plural(count: number, word: string): string {
  return `${count} ${count === 1 ? word : `${word}s`}`
}

const styles = StyleSheet.create({
  card: { borderWidth: 1, borderRadius: radius.xl, overflow: 'hidden' },
  head: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingVertical: space.sm,
    paddingLeft: space.sm,
    paddingRight: space.xs,
  },
  badge: {
    width: 32,
    height: 32,
    borderRadius: radius.lg,
    alignItems: 'center',
    justifyContent: 'center',
  },
  what: { flex: 1, minWidth: 0, gap: 2 },
  facts: { flexDirection: 'row', alignItems: 'center', gap: 4, flexWrap: 'wrap' },
  state: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  open: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 2,
    height: 30,
    paddingHorizontal: space.sm,
    borderRadius: radius.md,
  },
  toggle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    height: 30,
    paddingHorizontal: space.md,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  details: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    gap: space.sm,
  },
  section: { gap: 2 },
  sectionTitle: { letterSpacing: 0.5 },
  prompt: { lineHeight: 20 },
})
