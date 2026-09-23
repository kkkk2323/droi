// The header's way to subagents, like the web Client's: a Session that called
// subagents lists them in a sheet, and a subagent's title leads back to its
// caller and switches to another subagent of the same caller.
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import {
  isRunning,
  useSubagentLinks,
  type SessionRef,
  type SubagentRun,
} from '@droi/daemon-layer/subagents'
import { Bot, Check, ChevronDown, ChevronLeft, CircleDashed } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, View } from 'react-native'
import { Spinner } from '../ui/activity'
import { Text } from '../ui/primitives'
import { Sheet } from '../ui/sheet'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function SubagentsButton({ subagents }: { subagents: readonly SessionSummary[] }) {
  const colors = useColors()
  const links = useSubagentLinks()
  const [open, setOpen] = useState(false)
  if (!links || subagents.length === 0) return null
  const running = subagents.filter((s) => isRunning(links.runs.get(s.sessionId)?.status)).length
  const count = `${subagents.length} ${subagents.length === 1 ? 'subagent' : 'subagents'}`
  return (
    <>
      <Pressable
        role="button"
        aria-label={running > 0 ? `${count}, ${running} running` : count}
        aria-haspopup="dialog"
        onPress={() => setOpen(true)}
        style={({ pressed }) => [
          styles.button,
          pressed ? { backgroundColor: colors.accent } : null,
        ]}
      >
        {running > 0 ? (
          <Spinner size={13} color={colors.working} />
        ) : (
          <Bot size={14} color={colors.mutedForeground} strokeWidth={1.75} />
        )}
        <Text tone="muted" size="xs">
          {subagents.length}
        </Text>
      </Pressable>
      <Sheet visible={open} title="Subagents" onClose={() => setOpen(false)}>
        <SubagentList
          subagents={subagents}
          runs={links.runs}
          onPick={(sessionId) => {
            setOpen(false)
            links.open(sessionId)
          }}
        />
      </Sheet>
    </>
  )
}

/** A subagent's header title: its caller above, and itself as a switch between siblings. */
export function SubagentTitle({
  sessionId,
  title,
  trail,
  siblings,
}: {
  sessionId: string
  title: string
  trail: readonly SessionRef[]
  siblings: readonly SessionSummary[]
}) {
  const colors = useColors()
  const links = useSubagentLinks()
  const [open, setOpen] = useState(false)
  const caller = trail[trail.length - 1]
  return (
    <View role="navigation" aria-label="Session hierarchy" style={styles.title}>
      {caller ? (
        <Pressable
          role="button"
          aria-label={caller.title}
          hitSlop={4}
          onPress={() => links?.open(caller.sessionId)}
          style={styles.caller}
        >
          <ChevronLeft size={12} color={colors.mutedForeground} strokeWidth={2} />
          <Text tone="muted" size="xs" numberOfLines={1} style={styles.shrink}>
            {caller.title}
          </Text>
        </Pressable>
      ) : null}
      <Pressable
        role="button"
        aria-label={title}
        aria-haspopup="dialog"
        onPress={() => setOpen(true)}
        style={styles.current}
      >
        <Text weight="semibold" size="sm" numberOfLines={1} style={styles.shrink}>
          {title}
        </Text>
        <ChevronDown size={14} color={colors.mutedForeground} strokeWidth={2} />
      </Pressable>
      <Sheet visible={open} title="Subagents" onClose={() => setOpen(false)}>
        <SubagentList
          subagents={siblings}
          runs={links?.runs}
          current={sessionId}
          onPick={(id) => {
            setOpen(false)
            if (id !== sessionId) links?.open(id)
          }}
        />
      </Sheet>
    </View>
  )
}

function SubagentList({
  subagents,
  runs,
  current,
  onPick,
}: {
  subagents: readonly SessionSummary[]
  runs: ReadonlyMap<string, SubagentRun> | undefined
  current?: string
  onPick: (sessionId: string) => void
}) {
  const colors = useColors()
  return (
    <ScrollView style={styles.list}>
      {subagents.map((s) => {
        const status = runs?.get(s.sessionId)?.status
        const checked = s.sessionId === current
        return (
          <Pressable
            key={s.sessionId}
            role={current === undefined ? 'button' : 'radio'}
            aria-checked={current === undefined ? undefined : checked}
            aria-label={s.title}
            onPress={() => onPick(s.sessionId)}
            style={({ pressed }) => [
              styles.row,
              pressed || checked ? { backgroundColor: colors.accent } : null,
            ]}
          >
            {isRunning(status) ? (
              <View role="img" aria-label="Running">
                <Spinner size={14} color={colors.working} />
              </View>
            ) : status === 'completed' ? (
              <View role="img" aria-label="Completed">
                <Check size={14} color={colors.success} strokeWidth={2} />
              </View>
            ) : (
              <CircleDashed size={14} color={colors.mutedForeground} strokeWidth={2} />
            )}
            <Text numberOfLines={1} weight={checked ? 'medium' : 'regular'} style={styles.shrink}>
              {s.title}
            </Text>
          </Pressable>
        )
      })}
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  button: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    height: 30,
    paddingHorizontal: space.sm,
    borderRadius: radius.md,
  },
  title: { flex: 1, minWidth: 0, justifyContent: 'center' },
  caller: { flexDirection: 'row', alignItems: 'center', gap: 2, alignSelf: 'flex-start' },
  current: { flexDirection: 'row', alignItems: 'center', gap: 4, alignSelf: 'flex-start' },
  shrink: { flexShrink: 1 },
  list: { flexGrow: 0 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 44,
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
    borderRadius: radius.lg,
  },
})
