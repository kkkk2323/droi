// A Workstream's ways around (ADR 0036): an orchestrator's header lists its
// agents in a sheet, grouped by whether they wait on the user, work or idle,
// and an agent's title leads back to its orchestrator.
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import type { SessionRef } from '@droi/daemon-layer/subagents'
import { useSessionActivity } from '@droi/daemon-layer/use-session-activity'
import { agentCount, groupAgents, type AgentStatus } from '@droi/daemon-layer/workstreams'
import { ChevronLeft, CircleAlert, CircleDashed, Network } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, View } from 'react-native'
import { Spinner } from '../ui/activity'
import { Text } from '../ui/primitives'
import { Sheet } from '../ui/sheet'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { useConnectedComputer } from './connected-computer'

const GROUPS: ReadonlyArray<{ status: AgentStatus; label: string }> = [
  { status: 'waiting', label: 'Waiting on you' },
  { status: 'working', label: 'Working' },
  { status: 'idle', label: 'Idle' },
]

/** The orchestrator's header button: how many agents, how many wait on the user, and the list. */
export function AgentsButton({ agents }: { agents: readonly SessionSummary[] }) {
  const colors = useColors()
  const connected = useConnectedComputer()
  const activity = useSessionActivity()
  const [open, setOpen] = useState(false)
  const groups = groupAgents(agents, activity)
  const waiting = groups.waiting.length
  const label = `${agentCount(agents.length)}${waiting > 0 ? `, ${waiting} waiting on you` : ''}`
  return (
    <>
      <Pressable
        role="button"
        aria-label={label}
        aria-haspopup="dialog"
        onPress={() => setOpen(true)}
        style={({ pressed }) => [
          styles.button,
          pressed ? { backgroundColor: colors.accent } : null,
        ]}
      >
        <Network size={14} color={colors.mutedForeground} strokeWidth={1.75} />
        <Text tone="muted" size="xs">
          {agents.length}
        </Text>
        {waiting > 0 ? (
          <>
            <CircleAlert
              size={14}
              color={colors.attention}
              strokeWidth={2}
              style={styles.waiting}
            />
            <Text size="xs" style={{ color: colors.attention }}>
              {waiting}
            </Text>
          </>
        ) : null}
      </Pressable>
      <Sheet visible={open} title="Agents" onClose={() => setOpen(false)}>
        <ScrollView style={styles.list}>
          {agents.length === 0 ? (
            <Text tone="muted" size="sm" style={styles.empty}>
              No agents yet. The orchestrator starts them as it splits the work.
            </Text>
          ) : (
            GROUPS.map(({ status, label: groupLabel }) =>
              groups[status].length > 0 ? (
                <View key={status} role="group" aria-label={groupLabel} style={styles.group}>
                  <Text
                    role="heading"
                    size="xs"
                    weight="medium"
                    tone="muted"
                    style={styles.groupTitle}
                  >
                    {groupLabel}
                  </Text>
                  {groups[status].map((agent) => (
                    <AgentRow
                      key={agent.sessionId}
                      agent={agent}
                      status={status}
                      onPress={() => {
                        setOpen(false)
                        connected?.open(agent.sessionId)
                      }}
                    />
                  ))}
                </View>
              ) : null,
            )
          )}
        </ScrollView>
      </Sheet>
    </>
  )
}

function AgentRow({
  agent,
  status,
  onPress,
}: {
  agent: SessionSummary
  status: AgentStatus
  onPress: () => void
}) {
  const colors = useColors()
  return (
    <Pressable
      role="button"
      aria-label={agent.title}
      onPress={onPress}
      style={({ pressed }) => [styles.row, pressed ? { backgroundColor: colors.accent } : null]}
    >
      {status === 'waiting' ? (
        <CircleAlert size={14} color={colors.attention} strokeWidth={2} />
      ) : status === 'working' ? (
        <Spinner size={14} color={colors.working} />
      ) : (
        <CircleDashed size={14} color={colors.mutedForeground} strokeWidth={2} />
      )}
      <Text numberOfLines={1} style={styles.shrink}>
        {agent.title}
      </Text>
      {agent.worktree ? (
        <Text tone="muted" size="xs" mono numberOfLines={1} style={styles.branch}>
          {agent.worktree.branch}
        </Text>
      ) : null}
    </Pressable>
  )
}

/** An agent's header title: its orchestrator above, as the way back, and its own title. */
export function AgentTitle({ title, orchestrator }: { title: string; orchestrator: SessionRef }) {
  const colors = useColors()
  const connected = useConnectedComputer()
  const back = `Workstream: ${orchestrator.title}`
  return (
    <View role="navigation" aria-label="Workstream" style={styles.title}>
      <Pressable
        role="button"
        aria-label={back}
        hitSlop={4}
        onPress={() => connected?.open(orchestrator.sessionId)}
        style={styles.back}
      >
        <ChevronLeft size={12} color={colors.mutedForeground} strokeWidth={2} />
        <Text tone="muted" size="xs" numberOfLines={1} style={styles.shrink}>
          {back}
        </Text>
      </Pressable>
      <Text role="heading" weight="semibold" size="sm" numberOfLines={1}>
        {title}
      </Text>
    </View>
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
  list: { flexGrow: 0 },
  empty: { paddingHorizontal: space.lg, paddingVertical: space.sm },
  group: { marginBottom: space.sm },
  groupTitle: {
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
    paddingVertical: space.xs,
  },
  waiting: { marginLeft: space.xs },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 44,
    paddingHorizontal: space.lg,
    marginHorizontal: space.sm,
    borderRadius: radius.md,
  },
  shrink: { flexShrink: 1 },
  branch: { marginLeft: 'auto', flexShrink: 1, maxWidth: '45%' },
  title: { maxWidth: 240, justifyContent: 'center' },
  back: { flexDirection: 'row', alignItems: 'center', gap: 2, alignSelf: 'flex-start' },
})
