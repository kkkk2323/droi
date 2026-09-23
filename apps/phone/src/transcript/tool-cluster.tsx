// A run of tool calls as one quiet cluster, like the web Client's: a header
// that folds the list, and rows with a success or failure mark that open to
// the input and the result (a line diff for Edit and Create).
import {
  readToolResult,
  toolInputText,
  toolSummary,
  truncateLines,
} from '@droi/daemon-layer/tool-calls'
import type { ToolCall } from '@droi/daemon-layer/transcript'
import type { DiffLine } from '@droi/daemon-layer/tool-calls'
import {
  Check,
  ChevronRight,
  CircleX,
  FileEdit,
  FilePlus,
  FileText,
  FolderSearch,
  Globe,
  Search,
  Terminal,
  Wrench,
  type LucideIcon,
} from 'lucide-react-native'
import { useState } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, View } from 'react-native'
import { Text } from '../ui/primitives'
import { fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

const ICONS: Record<string, LucideIcon> = {
  Execute: Terminal,
  Read: FileText,
  Edit: FileEdit,
  Create: FilePlus,
  ApplyPatch: FileEdit,
  Grep: Search,
  Glob: FolderSearch,
  LS: FolderSearch,
  FetchUrl: Globe,
  WebSearch: Globe,
}

const RESULT_PREVIEW_LINES = 40

export function ToolCluster({ calls }: { calls: ToolCall[] }) {
  const colors = useColors()
  const [open, setOpen] = useState(true)
  const pending = calls.filter((c) => c.result === null).length
  const what = calls.length === 1 ? (calls[0]?.use.name ?? 'a tool') : `${calls.length} tools`
  return (
    <View>
      <Pressable
        role="button"
        aria-expanded={open}
        onPress={() => setOpen(!open)}
        style={styles.clusterHeader}
      >
        {pending > 0 ? <ActivityIndicator size="small" color={colors.mutedForeground} /> : null}
        <Text tone="muted" size="xs" weight="medium">
          {pending > 0 ? `Running ${what}` : `Used ${what}`}
        </Text>
        <ChevronRight
          size={12}
          color={colors.mutedForeground}
          style={{ transform: [{ rotate: open ? '90deg' : '0deg' }] }}
        />
      </Pressable>
      {open ? (
        <View style={[styles.rows, { borderLeftColor: colors.border }]}>
          {calls.map((call) => (
            <ToolRow key={call.use.id} call={call} />
          ))}
        </View>
      ) : null}
    </View>
  )
}

function ToolRow({ call }: { call: ToolCall }) {
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const Icon = ICONS[call.use.name] ?? Wrench
  const summary = toolSummary(call)
  const { text, pending, isError, diff, status } = readToolResult(call)
  const tint = isError ? colors.destructiveForeground : colors.mutedForeground
  return (
    <View>
      <Pressable
        role="button"
        aria-label={`${call.use.name}: ${summary}`}
        aria-expanded={open}
        onPress={() => setOpen(!open)}
        style={({ pressed }) => [styles.row, pressed ? { backgroundColor: colors.muted } : null]}
      >
        <Icon size={14} color={tint} strokeWidth={1.75} />
        <Text
          size="sm"
          weight="medium"
          style={isError ? { color: colors.destructiveForeground } : null}
        >
          {call.use.name}
        </Text>
        <Text tone="muted" size="xs" mono numberOfLines={1} style={styles.summary}>
          {summary}
        </Text>
        {diff ? (
          <Text size="xs" mono>
            <Text size="xs" mono style={{ color: colors.success }}>
              +{diff.added}
            </Text>{' '}
            <Text size="xs" mono style={{ color: colors.destructiveForeground }}>
              −{diff.removed}
            </Text>
          </Text>
        ) : null}
        {pending ? (
          <View role="status" aria-label="Running">
            <ActivityIndicator size="small" color={colors.mutedForeground} />
          </View>
        ) : isError ? (
          <View role="img" aria-label="Failed">
            <CircleX size={14} color={colors.destructiveForeground} strokeWidth={2} />
          </View>
        ) : (
          <View role="img" aria-label="Succeeded">
            <Check size={14} color={colors.success} strokeWidth={2} />
          </View>
        )}
      </Pressable>
      {open ? (
        <View
          role="region"
          aria-label={`${call.use.name} details`}
          style={[styles.detail, { borderColor: colors.border, backgroundColor: colors.card }]}
        >
          <Text selectable size="xs" mono style={styles.detailText}>
            {toolInputText(call)}
          </Text>
          {diff ? (
            <DiffView lines={diff.lines} />
          ) : status ? (
            <Text
              size="xs"
              mono
              style={[
                styles.detailText,
                { color: status.success ? colors.success : colors.destructiveForeground },
              ]}
            >
              {status.message ?? (status.success ? 'Succeeded' : 'Failed')}
            </Text>
          ) : text ? (
            <Text
              selectable
              size="xs"
              mono
              style={[
                styles.detailText,
                { color: isError ? colors.destructiveForeground : colors.mutedForeground },
              ]}
            >
              {truncateLines(text, RESULT_PREVIEW_LINES)}
            </Text>
          ) : null}
        </View>
      ) : null}
    </View>
  )
}

function DiffView({ lines }: { lines: DiffLine[] }) {
  const width = String(Math.max(1, ...lines.map((l) => Math.max(l.old ?? 0, l.new ?? 0)))).length
  const pad = (n: number | null) => String(n ?? '').padStart(width, ' ')
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false}>
      <View role="group" aria-label="Diff" style={styles.diff}>
        {lines.map((line) => (
          <View
            key={`${line.type}:${line.old ?? ''}:${line.new ?? ''}`}
            role="none"
            aria-label={`${line.type === 'added' ? 'Added' : line.type === 'removed' ? 'Removed' : 'Unchanged'}: ${line.content}`}
            style={[
              styles.diffLine,
              line.type === 'added'
                ? { backgroundColor: 'rgba(16, 185, 129, 0.12)' }
                : line.type === 'removed'
                  ? { backgroundColor: 'rgba(244, 63, 94, 0.12)' }
                  : null,
            ]}
          >
            <Text size="xs" mono tone="muted" style={styles.gutter}>
              {pad(line.old)} {pad(line.new)}
            </Text>
            <Text size="xs" mono style={styles.sign}>
              {line.type === 'added' ? '+' : line.type === 'removed' ? '−' : ' '}
            </Text>
            <Text size="xs" mono style={{ fontFamily: fonts.mono }}>
              {line.content}
            </Text>
          </View>
        ))}
      </View>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  clusterHeader: { flexDirection: 'row', alignItems: 'center', gap: space.xs, height: 24 },
  rows: { borderLeftWidth: StyleSheet.hairlineWidth, marginLeft: space.xs, paddingLeft: space.sm },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 32,
    borderRadius: radius.md,
    paddingHorizontal: space.xs,
  },
  summary: { flex: 1 },
  detail: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.lg,
    marginBottom: space.xs,
    overflow: 'hidden',
  },
  detailText: { padding: space.sm, lineHeight: 18 },
  diff: { paddingBottom: space.xs },
  diffLine: { flexDirection: 'row', paddingRight: space.md },
  gutter: { paddingHorizontal: space.sm, opacity: 0.6 },
  sign: { width: 14 },
})
