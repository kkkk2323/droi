// An assistant's reply: Markdown, with any `<json-render>` tree it carries
// drawn in place with the Phone App's own Views, as the web Client does
// (json-render.ts in the daemon layer reads the tags and the props).
import {
  cell,
  childrenOf,
  fraction,
  hasRenderTag,
  num,
  numbers,
  records,
  splitReply,
  str,
  strings,
  toneOf,
  type RenderSpec,
  type Tone,
} from '@droi/daemon-layer/json-render'
import { withKeys } from '@droi/daemon-layer/keys'
import {
  CircleCheck,
  CircleX,
  Info,
  Lightbulb,
  TrendingDown,
  TrendingUp,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react-native'
import { Fragment, useMemo, type ReactNode } from 'react'
import { ScrollView, StyleSheet, View } from 'react-native'
import Svg, { Polyline } from 'react-native-svg'
import { Text } from '../ui/primitives'
import { radius, space, type Colors } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { Markdown } from './markdown'

export function Reply({ text, streaming = false }: { text: string; streaming?: boolean }) {
  const colors = useColors()
  const segments = useMemo(() => (hasRenderTag(text) ? splitReply(text) : null), [text])
  if (!segments) return <Markdown text={text} streaming={streaming} />
  return (
    <View style={styles.reply}>
      {withKeys(segments, (s) => s.kind).map(({ key, item: segment }) => {
        if (segment.kind === 'markdown') {
          return <Markdown key={key} text={segment.text} streaming={streaming} />
        }
        if (segment.kind === 'render') return <RenderTree key={key} spec={segment.spec} />
        return streaming ? (
          <View
            key={key}
            aria-busy
            aria-label="Drawing output"
            style={[styles.pending, { backgroundColor: colors.muted }]}
          />
        ) : (
          <Markdown key={key} text={'```json\n' + segment.raw + '\n```'} />
        )
      })}
    </View>
  )
}

export function RenderTree({ spec }: { spec: RenderSpec }) {
  const colors = useColors()
  return <View style={styles.tree}>{node(spec, spec.root, new Set(), colors)}</View>
}

function toneColor(tone: Tone, colors: Colors): string {
  switch (tone) {
    case 'success':
      return colors.success
    case 'warning':
      return colors.attention
    case 'error':
      return colors.destructiveForeground
    case 'info':
      return colors.working
    case 'muted':
      return colors.mutedForeground
    default:
      return colors.foreground
  }
}

const TONE_ICON: Record<Tone, LucideIcon> = {
  default: Info,
  muted: Info,
  success: CircleCheck,
  warning: TriangleAlert,
  error: CircleX,
  info: Info,
}

// The CLI measures in terminal cells; a cell of spacing reads as 4pt here.
const cells = (n: number | null, max = 8): number => Math.min(max, Math.max(0, n ?? 0)) * 4

function node(spec: RenderSpec, id: string, seen: ReadonlySet<string>, colors: Colors): ReactNode {
  const element = spec.elements[id]
  if (!element) return null
  const below = new Set(seen).add(id)
  const kids = childrenOf(spec, id, below).map((child) => (
    <Fragment key={child}>{node(spec, child, below, colors)}</Fragment>
  ))
  const p = element.props
  switch (element.type) {
    case 'Box': {
      const bordered = str(p, 'borderStyle') !== '' && str(p, 'borderStyle') !== 'none'
      return (
        <View
          style={[
            {
              flexDirection: str(p, 'flexDirection') === 'row' ? 'row' : 'column',
              flexWrap: 'wrap',
              gap: cells(num(p, 'gap')),
              padding: cells(num(p, 'padding')),
            },
            bordered && [styles.bordered, { borderColor: colors.border }],
          ]}
        >
          {kids}
        </View>
      )
    }
    case 'Text':
      return (
        <Text
          size="sm"
          weight={p['bold'] === true ? 'semibold' : 'regular'}
          style={{ color: toneColor(toneOf(str(p, 'color')), colors) }}
        >
          {str(p, 'text')}
        </Text>
      )
    case 'Heading':
      return (
        <Text role="heading" size={num(p, 'level') === 1 ? 'base' : 'sm'} weight="semibold">
          {str(p, 'text')}
        </Text>
      )
    case 'Divider': {
      const title = str(p, 'title')
      return (
        <View role="separator" style={styles.divider}>
          <View style={[styles.rule, { backgroundColor: colors.border }]} />
          {title ? (
            <Text tone="muted" size="xs">
              {title}
            </Text>
          ) : null}
          {title ? <View style={[styles.rule, { backgroundColor: colors.border }]} /> : null}
        </View>
      )
    }
    case 'Newline':
      return <View style={styles.newline} />
    case 'Spacer':
      return <View style={styles.fill} />
    case 'Table':
      return <Table props={p} colors={colors} />
    case 'List': {
      const ordered = p['ordered'] === true
      return (
        <View role="list" style={styles.list}>
          {withKeys(strings(p, 'items'), (item) => item).map(({ key, item }, index) => (
            <View key={key} role="listitem" style={styles.listItem}>
              <Text tone="muted" size="sm">
                {ordered ? `${index + 1}.` : '•'}
              </Text>
              <Text size="sm" style={styles.fill}>
                {item}
              </Text>
            </View>
          ))}
        </View>
      )
    }
    case 'Card': {
      const title = str(p, 'title')
      return (
        <View
          role="region"
          aria-label={title || undefined}
          style={[
            styles.card,
            { borderColor: colors.border, backgroundColor: colors.card },
            { padding: cells(num(p, 'padding') ?? 3) },
          ]}
        >
          {title ? (
            <Text tone="muted" size="xs" weight="semibold">
              {title}
            </Text>
          ) : null}
          {kids}
        </View>
      )
    }
    case 'StatusLine': {
      const tone = toneOf(str(p, 'status'))
      const Icon = TONE_ICON[tone]
      return (
        <View style={styles.inline}>
          <View role="img" aria-label={str(p, 'status') || 'Status'}>
            <Icon size={14} color={toneColor(tone, colors)} strokeWidth={2} />
          </View>
          <Text size="sm">{str(p, 'text')}</Text>
        </View>
      )
    }
    case 'KeyValue':
      return (
        <View style={styles.inline}>
          <Text tone="muted" size="sm" style={styles.key}>
            {str(p, 'label')}
          </Text>
          <Text selectable size="sm" mono style={styles.fill}>
            {str(p, 'value')}
          </Text>
        </View>
      )
    case 'Badge':
      return (
        <Text
          size="xs"
          weight="medium"
          style={[
            styles.badge,
            { borderColor: colors.border, color: toneColor(toneOf(str(p, 'variant')), colors) },
          ]}
        >
          {str(p, 'label')}
        </Text>
      )
    case 'ProgressBar': {
      const value = fraction(num(p, 'progress') ?? 0, 1)
      const label = str(p, 'label')
      return (
        <View style={styles.inline}>
          {label ? (
            <Text tone="muted" size="sm">
              {label}
            </Text>
          ) : null}
          <View
            role="progressbar"
            aria-label={label || 'Progress'}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(value * 100)}
            style={[styles.track, { backgroundColor: colors.muted }]}
          >
            <View
              style={[styles.bar, { width: `${value * 100}%`, backgroundColor: colors.primary }]}
            />
          </View>
          <Text tone="muted" size="xs">
            {Math.round(value * 100)}%
          </Text>
        </View>
      )
    }
    case 'Metric': {
      const trend = str(p, 'trend')
      const Trend = trend === 'up' ? TrendingUp : trend === 'down' ? TrendingDown : null
      return (
        <View>
          <Text tone="muted" size="xs">
            {str(p, 'label')}
          </Text>
          <View style={styles.inline}>
            <Text size="lg" weight="semibold">
              {str(p, 'value')}
            </Text>
            {Trend ? (
              <View role="img" aria-label={`Trend ${trend}`}>
                <Trend size={16} color={colors.mutedForeground} />
              </View>
            ) : null}
          </View>
        </View>
      )
    }
    case 'Callout': {
      const kind = str(p, 'type')
      const tone = toneOf(kind)
      const Icon = kind === 'tip' ? Lightbulb : TONE_ICON[tone]
      const title = str(p, 'title')
      return (
        <View
          role="note"
          aria-label={title || kind || 'Note'}
          style={[styles.callout, { borderColor: colors.border, backgroundColor: colors.card }]}
        >
          <Icon size={14} color={toneColor(tone, colors)} strokeWidth={2} />
          <View style={styles.fill}>
            {title ? (
              <Text size="sm" weight="semibold">
                {title}
              </Text>
            ) : null}
            <Text tone="muted" size="sm">
              {str(p, 'content')}
            </Text>
            {kids}
          </View>
        </View>
      )
    }
    case 'Timeline':
      return (
        <View role="list" style={styles.list}>
          {withKeys(records(p, 'items'), (item) => str(item, 'title')).map(({ key, item }) => {
            const tone = toneOf(str(item, 'status'))
            return (
              <View key={key} role="listitem" style={styles.listItem}>
                <View
                  style={[
                    styles.dot,
                    {
                      backgroundColor:
                        tone === 'default' ? colors.mutedForeground : toneColor(tone, colors),
                    },
                  ]}
                />
                <View style={styles.fill}>
                  <Text size="sm" weight="medium">
                    {str(item, 'title')}
                    {str(item, 'status') ? (
                      <Text size="xs" style={{ color: toneColor(tone, colors) }}>
                        {'  '}
                        {str(item, 'status')}
                      </Text>
                    ) : null}
                  </Text>
                  {str(item, 'description') ? (
                    <Text tone="muted" size="sm">
                      {str(item, 'description')}
                    </Text>
                  ) : null}
                </View>
              </View>
            )
          })}
        </View>
      )
    case 'BarChart': {
      const data = records(p, 'data').map((d) => ({
        label: str(d, 'label'),
        value: num(d, 'value') ?? 0,
        tone: toneOf(str(d, 'color')),
      }))
      const max = Math.max(0, ...data.map((d) => d.value))
      const total = data.reduce((sum, d) => sum + Math.max(0, d.value), 0)
      const percent = p['showPercentage'] === true
      return (
        <View role="list" aria-label="Bar chart" style={styles.list}>
          {withKeys(data, (d) => d.label).map(({ key, item: d }) => {
            const shown = percent
              ? `${Math.round(fraction(d.value, total) * 100)}%`
              : String(d.value)
            return (
              <View
                key={key}
                role="listitem"
                aria-label={`${d.label}: ${shown}`}
                style={styles.inline}
              >
                <Text tone="muted" size="sm" numberOfLines={1} style={styles.barLabel}>
                  {d.label}
                </Text>
                <View style={[styles.track, { backgroundColor: colors.muted }]}>
                  <View
                    style={[
                      styles.bar,
                      {
                        width: `${fraction(d.value, max) * 100}%`,
                        backgroundColor:
                          d.tone === 'default' ? colors.foreground : toneColor(d.tone, colors),
                        opacity: d.tone === 'default' ? 0.7 : 1,
                      },
                    ]}
                  />
                </View>
                <Text size="xs">{shown}</Text>
              </View>
            )
          })}
        </View>
      )
    }
    case 'Sparkline': {
      const data = numbers(p, 'data')
      if (data.length < 2) return null
      const min = Math.min(...data)
      const max = Math.max(...data)
      const points = data
        .map(
          (v, i) =>
            `${((i / (data.length - 1)) * 120).toFixed(1)},${(22 - fraction(v - min, max - min) * 20).toFixed(1)}`,
        )
        .join(' ')
      const tone = toneOf(str(p, 'color'))
      return (
        <View
          role="img"
          aria-label={`Sparkline from ${data[0]} to ${data[data.length - 1]}, range ${min} to ${max}`}
        >
          <Svg width={120} height={24} viewBox="0 0 120 24">
            <Polyline
              points={points}
              fill="none"
              stroke={tone === 'default' ? colors.foreground : toneColor(tone, colors)}
              strokeWidth={1.5}
              strokeLinejoin="round"
            />
          </Svg>
        </View>
      )
    }
    default:
      // A component this Client does not know: what it holds still shows.
      return kids.length > 0 ? <View style={styles.tree}>{kids}</View> : null
  }
}

function Table({ props, colors }: { props: Record<string, unknown>; colors: Colors }) {
  const rows = records(props, 'rows')
  const declared = records(props, 'columns')
  const columns = declared.length
    ? declared.map((c) => ({ key: str(c, 'key'), header: str(c, 'header') || str(c, 'key') }))
    : Object.keys(rows[0] ?? {}).map((key) => ({ key, header: key }))
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false}>
      <View role="table" style={[styles.table, { borderColor: colors.border }]}>
        <View
          role="row"
          style={[styles.tr, { backgroundColor: colors.muted, borderBottomColor: colors.border }]}
        >
          {columns.map((column) => (
            <Text key={column.key} role="columnheader" size="xs" weight="medium" style={styles.td}>
              {column.header}
            </Text>
          ))}
        </View>
        {withKeys(rows, (row) => JSON.stringify(row)).map(({ key, item: row }) => (
          <View key={key} role="row" style={[styles.tr, { borderBottomColor: colors.border }]}>
            {columns.map((column) => (
              <Text key={column.key} role="cell" selectable size="xs" style={styles.td}>
                {cell(row[column.key])}
              </Text>
            ))}
          </View>
        ))}
      </View>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  reply: { gap: space.sm },
  pending: { height: 64, borderRadius: radius.xl },
  tree: { gap: space.sm },
  bordered: { borderWidth: StyleSheet.hairlineWidth, borderRadius: radius.lg },
  divider: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  rule: { flex: 1, height: StyleSheet.hairlineWidth },
  newline: { height: space.sm },
  fill: { flex: 1 },
  list: { gap: space.xs },
  listItem: { flexDirection: 'row', alignItems: 'flex-start', gap: space.sm },
  card: { borderWidth: StyleSheet.hairlineWidth, borderRadius: radius.xl, gap: space.sm },
  inline: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  key: { width: 112 },
  badge: {
    alignSelf: 'flex-start',
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.sm,
    paddingHorizontal: 6,
    paddingVertical: 1,
    overflow: 'hidden',
  },
  track: { flex: 1, height: 6, borderRadius: radius.full, overflow: 'hidden' },
  bar: { height: '100%', borderRadius: radius.full },
  barLabel: { width: 80 },
  callout: {
    flexDirection: 'row',
    gap: space.sm,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.lg,
    padding: space.sm,
  },
  dot: { width: 8, height: 8, borderRadius: 4, marginTop: 6 },
  table: { borderWidth: StyleSheet.hairlineWidth, borderRadius: radius.lg, overflow: 'hidden' },
  tr: { flexDirection: 'row', borderBottomWidth: StyleSheet.hairlineWidth },
  td: { width: 140, paddingHorizontal: space.sm, paddingVertical: 6 },
})
