// A Script, or a WaitForScript picking its run up again, as a row like any
// tool's, with the calls the run made while it watched listed under it, as in
// the web Client. The row opens to the program, its inputs, and what the
// model read back.
import {
  readScriptRun,
  scriptSource,
  scriptSummary,
  type ScriptRun,
} from '@droi/daemon-layer/script-runs'
import { truncateLines } from '@droi/daemon-layer/tool-calls'
import type { ToolCall } from '@droi/daemon-layer/transcript'
import { Braces, Check, CircleAlert, CircleX, Clock } from 'lucide-react-native'
import type { ReactNode } from 'react'
import { Image, Pressable, ScrollView, StyleSheet, View } from 'react-native'
import { Spinner } from '../ui/activity'
import { Text } from '../ui/primitives'
import { radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { Folded, useFold } from './fold'

const OUTPUT_PREVIEW_LINES = 40

export function ScriptGroup({ call, children }: { call: ToolCall; children: ReactNode }) {
  const colors = useColors()
  const fold = useFold()
  const run = readScriptRun(call)
  const summary = scriptSummary(call)
  const failed = run.status === 'failed' || run.status === 'cancelled'
  const tint = failed ? colors.destructiveForeground : colors.mutedForeground
  return (
    <View>
      <Pressable
        role="button"
        aria-label={`${call.use.name}: ${summary}`}
        aria-expanded={fold.open}
        onPress={fold.toggle}
        style={({ pressed }) => [styles.row, pressed ? { backgroundColor: colors.muted } : null]}
      >
        <Braces size={14} color={tint} strokeWidth={1.75} />
        <Text
          size="sm"
          weight="medium"
          style={failed ? { color: colors.destructiveForeground } : null}
        >
          Script
        </Text>
        <Text tone="muted" size="xs" mono numberOfLines={1} style={styles.summary}>
          {summary}
        </Text>
        <RunStatus call={call} run={run} />
      </Pressable>
      <Folded fold={fold}>
        <ScriptDetail call={call} run={run} />
      </Folded>
      {call.nested && call.nested.length > 0 ? (
        <View
          role="group"
          aria-label="Calls made by Script"
          style={[styles.calls, { borderLeftColor: colors.border }]}
        >
          {children}
        </View>
      ) : null}
    </View>
  )
}

function RunStatus({ call, run }: { call: ToolCall; run: ScriptRun }) {
  const colors = useColors()
  if (call.result === null) {
    return (
      <View role="status" aria-label="Running">
        <Spinner size={14} color={colors.mutedForeground} />
      </View>
    )
  }
  const [label, Icon, color] =
    run.status === 'running'
      ? (['Still running', Clock, colors.mutedForeground] as const)
      : run.status === 'stalled'
        ? (['Stalled', CircleAlert, colors.attention] as const)
        : run.status === 'failed'
          ? (['Failed', CircleX, colors.destructiveForeground] as const)
          : run.status === 'cancelled'
            ? (['Cancelled', CircleX, colors.destructiveForeground] as const)
            : (['Succeeded', Check, colors.success] as const)
  return (
    <View role="img" aria-label={label}>
      <Icon size={14} color={color} strokeWidth={2} />
    </View>
  )
}

function ScriptDetail({ call, run }: { call: ToolCall; run: ScriptRun }) {
  const colors = useColors()
  const source = scriptSource(call)
  return (
    <View style={[styles.detail, { borderColor: colors.border, backgroundColor: colors.card }]}>
      {source ? (
        <Section label="Source">
          <SourceView script={source.script} />
        </Section>
      ) : null}
      {source?.inputs.map((input) => (
        <Section key={input.name} label={`inputs.${input.name}`}>
          <Text selectable tone="muted" size="xs" mono style={styles.body}>
            {truncateLines(input.text, OUTPUT_PREVIEW_LINES)}
          </Text>
        </Section>
      ))}
      {run.output || run.value || run.error || run.images.length > 0 ? (
        <Section label="Output">
          {run.output ? (
            <Text selectable tone="muted" size="xs" mono style={styles.body}>
              {truncateLines(run.output, OUTPUT_PREVIEW_LINES)}
            </Text>
          ) : null}
          {run.value ? (
            <Text selectable size="xs" mono style={styles.body}>
              {truncateLines(run.value, OUTPUT_PREVIEW_LINES)}
            </Text>
          ) : null}
          {run.error ? (
            <Text
              selectable
              size="xs"
              mono
              style={[styles.body, { color: colors.destructiveForeground }]}
            >
              {run.error}
            </Text>
          ) : null}
          {run.images.map((src) => (
            <Image
              key={src}
              source={{ uri: src }}
              alt="Picture from Script"
              accessibilityLabel="Picture from Script"
              resizeMode="contain"
              style={[styles.picture, { borderColor: colors.border }]}
            />
          ))}
        </Section>
      ) : null}
      {run.stats || run.logPath ? (
        <View style={[styles.footer, { borderTopColor: colors.border }]}>
          {run.stats ? (
            <Text tone="muted" size="xs">
              {run.stats}
            </Text>
          ) : null}
          {run.logPath ? (
            <Text selectable tone="muted" size="xs" mono numberOfLines={1} ellipsizeMode="head">
              {run.logPath}
            </Text>
          ) : null}
        </View>
      ) : null}
    </View>
  )
}

function Section({ label, children }: { label: string; children: ReactNode }) {
  return (
    <View role="region" aria-label={label}>
      <Text tone="muted" size="xs" weight="medium" style={styles.label}>
        {label.toUpperCase()}
      </Text>
      {children}
    </View>
  )
}

/** A program with its line numbers, the ones a permission request points at marked. */
export function SourceView({
  script,
  highlight,
}: {
  script: string
  highlight?: ReadonlySet<number>
}) {
  const colors = useColors()
  const lines = script.replace(/\n$/, '').split('\n')
  const width = String(lines.length).length
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false}>
      <View role="group" aria-label="Script source" style={styles.source}>
        {lines.map((line, index) => {
          const number = index + 1
          const marked = highlight?.has(number) === true
          return (
            <View
              key={number}
              role="none"
              aria-label={marked ? `Line ${number}, asked about: ${line}` : undefined}
              style={[styles.line, marked ? { backgroundColor: `${colors.attention}1f` } : null]}
            >
              <Text
                size="xs"
                mono
                tone="muted"
                style={[styles.gutter, marked ? { color: colors.attention, opacity: 1 } : null]}
              >
                {String(number).padStart(width, ' ')}
              </Text>
              <Text size="xs" mono>
                {line}
              </Text>
            </View>
          )
        })}
      </View>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 32,
    borderRadius: radius.md,
    paddingHorizontal: space.xs,
  },
  summary: { flex: 1 },
  calls: {
    borderLeftWidth: StyleSheet.hairlineWidth,
    borderStyle: 'dashed',
    marginLeft: space.sm,
    paddingLeft: space.sm,
  },
  detail: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.lg,
    marginBottom: space.xs,
    overflow: 'hidden',
    paddingBottom: space.xs,
  },
  label: { paddingHorizontal: space.sm, paddingTop: space.sm, paddingBottom: 2, fontSize: 10 },
  body: { paddingHorizontal: space.sm, paddingBottom: space.xs, lineHeight: 18 },
  source: { paddingBottom: space.xs },
  line: { flexDirection: 'row', paddingRight: space.md },
  gutter: { paddingHorizontal: space.sm, opacity: 0.6 },
  footer: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: space.sm,
    paddingTop: space.xs,
    gap: 2,
  },
  picture: {
    height: 220,
    marginHorizontal: space.sm,
    marginBottom: space.sm,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
  },
})
