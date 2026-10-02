import { Fragment, useMemo, type CSSProperties, type ReactNode } from 'react'
import {
  CircleAlert,
  CircleCheck,
  CircleX,
  Info,
  Lightbulb,
  TrendingDown,
  TrendingUp,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
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
import { Markdown } from './markdown'

/**
 * An assistant's reply: Markdown, with any `<json-render>` tree it carries
 * drawn in place (json-render.ts in the daemon layer reads the tags).
 */
export function Reply({ text, streaming }: { text: string; streaming: boolean }) {
  const segments = useMemo(() => (hasRenderTag(text) ? splitReply(text) : null), [text])
  if (!segments) return <Markdown text={text} />
  return (
    <>
      {withKeys(segments, (s) => s.kind).map(({ key, item: segment }) => {
        if (segment.kind === 'markdown') return <Markdown key={key} text={segment.text} />
        if (segment.kind === 'render') return <RenderTree key={key} spec={segment.spec} />
        return streaming ? (
          <div
            key={key}
            aria-busy
            aria-label="Drawing output"
            className="my-3 h-16 animate-pulse rounded-xl bg-muted"
          />
        ) : (
          <Markdown key={key} text={'```json\n' + segment.raw + '\n```'} />
        )
      })}
    </>
  )
}

export function RenderTree({ spec }: { spec: RenderSpec }) {
  return (
    <div data-json-render="" className="my-3 flex flex-col gap-2 text-[13px] leading-5">
      {node(spec, spec.root, new Set())}
    </div>
  )
}

const TONE_TEXT: Record<Tone, string> = {
  default: '',
  muted: 'text-muted-foreground',
  success: 'text-success',
  warning: 'text-attention-foreground',
  error: 'text-destructive-foreground',
  info: 'text-info-foreground',
}

const TONE_FILL: Record<Tone, string> = {
  default: 'bg-foreground/70',
  muted: 'bg-muted-foreground/60',
  success: 'bg-success',
  warning: 'bg-attention',
  error: 'bg-destructive',
  info: 'bg-info',
}

const TONE_ICON: Record<Tone, LucideIcon> = {
  default: Info,
  muted: Info,
  success: CircleCheck,
  warning: TriangleAlert,
  error: CircleX,
  info: Info,
}

// The CLI measures in terminal cells; a cell of spacing reads as 4px here.
const cells = (n: number | null, max = 8): number => Math.min(max, Math.max(0, n ?? 0)) * 4

function node(spec: RenderSpec, id: string, seen: ReadonlySet<string>): ReactNode {
  const element = spec.elements[id]
  if (!element) return null
  const below = new Set(seen).add(id)
  const kids = childrenOf(spec, id, below).map((child) => (
    <Fragment key={child}>{node(spec, child, below)}</Fragment>
  ))
  const p = element.props
  switch (element.type) {
    case 'Box': {
      const style: CSSProperties = {
        flexDirection: str(p, 'flexDirection') === 'row' ? 'row' : 'column',
        gap: cells(num(p, 'gap')),
        padding: cells(num(p, 'padding')),
      }
      const bordered = str(p, 'borderStyle') !== '' && str(p, 'borderStyle') !== 'none'
      return (
        <div
          style={style}
          className={cn('flex min-w-0 flex-wrap', bordered && 'rounded-lg border')}
        >
          {kids}
        </div>
      )
    }
    case 'Text':
      return (
        <span
          className={cn(
            'min-w-0 whitespace-pre-wrap',
            TONE_TEXT[toneOf(str(p, 'color'))],
            p['bold'] === true && 'font-semibold',
          )}
        >
          {str(p, 'text')}
          {kids}
        </span>
      )
    case 'Heading': {
      const level = Math.min(3, Math.max(1, num(p, 'level') ?? 2))
      const Tag = (['h3', 'h4', 'h5'] as const)[level - 1]!
      return (
        <Tag className={cn('font-semibold', level === 1 ? 'text-[15px]' : 'text-[13.5px]')}>
          {str(p, 'text')}
        </Tag>
      )
    }
    case 'Divider': {
      const title = str(p, 'title')
      return title ? (
        <div role="separator" className="flex items-center gap-2 text-[11px] text-muted-foreground">
          <span className="h-px flex-1 bg-border" />
          {title}
          <span className="h-px flex-1 bg-border" />
        </div>
      ) : (
        <hr className="border-border" />
      )
    }
    case 'Newline':
      return <div aria-hidden className="h-2" />
    case 'Spacer':
      return <div aria-hidden className="flex-1" />
    case 'Table':
      return <Table props={p} />
    case 'List': {
      const items = strings(p, 'items')
      const ordered = p['ordered'] === true
      const Tag = ordered ? 'ol' : 'ul'
      return (
        <Tag className={cn('flex flex-col gap-0.5 pl-5', ordered ? 'list-decimal' : 'list-disc')}>
          {withKeys(items, (item) => item).map(({ key, item }) => (
            <li key={key}>{item}</li>
          ))}
        </Tag>
      )
    }
    case 'Card': {
      const title = str(p, 'title')
      return (
        <section
          aria-label={title || undefined}
          className="flex flex-col gap-2 rounded-xl border bg-card/60"
          style={{ padding: cells(num(p, 'padding') ?? 3) }}
        >
          {title ? (
            <h4 className="text-[12px] font-semibold text-muted-foreground">{title}</h4>
          ) : null}
          {kids}
        </section>
      )
    }
    case 'StatusLine': {
      const tone = toneOf(str(p, 'status'))
      const Icon = TONE_ICON[tone]
      return (
        <div className="flex items-center gap-1.5">
          <Icon
            role="img"
            aria-label={str(p, 'status') || 'Status'}
            className={cn('size-3.5 shrink-0', TONE_TEXT[tone])}
          />
          <span>{str(p, 'text')}</span>
        </div>
      )
    }
    case 'KeyValue':
      return (
        <div className="flex gap-3">
          <span className="w-32 shrink-0 text-muted-foreground">{str(p, 'label')}</span>
          <span className="min-w-0 font-mono text-[12.5px] break-words">{str(p, 'value')}</span>
        </div>
      )
    case 'Badge': {
      const tone = toneOf(str(p, 'variant'))
      return (
        <span
          className={cn(
            'inline-flex h-5 w-fit items-center rounded-md border px-1.5 text-[11px] font-medium',
            TONE_TEXT[tone],
          )}
        >
          {str(p, 'label')}
        </span>
      )
    }
    case 'ProgressBar': {
      const value = fraction(num(p, 'progress') ?? 0, 1)
      const label = str(p, 'label')
      return (
        <div className="flex items-center gap-2">
          {label ? <span className="shrink-0 text-muted-foreground">{label}</span> : null}
          <div
            role="progressbar"
            aria-label={label || 'Progress'}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(value * 100)}
            className="h-1.5 min-w-16 flex-1 overflow-hidden rounded-full bg-muted"
          >
            <div className="h-full rounded-full bg-primary" style={{ width: `${value * 100}%` }} />
          </div>
          <span className="w-9 shrink-0 text-right text-[11.5px] text-muted-foreground tabular-nums">
            {Math.round(value * 100)}%
          </span>
        </div>
      )
    }
    case 'Metric': {
      const trend = str(p, 'trend')
      const Trend = trend === 'up' ? TrendingUp : trend === 'down' ? TrendingDown : null
      return (
        <div className="flex flex-col">
          <span className="text-[11.5px] text-muted-foreground">{str(p, 'label')}</span>
          <span className="flex items-center gap-1.5 text-lg font-semibold tabular-nums">
            {str(p, 'value')}
            {Trend ? (
              <Trend
                role="img"
                aria-label={`Trend ${trend}`}
                className="size-4 text-muted-foreground"
              />
            ) : null}
          </span>
        </div>
      )
    }
    case 'Callout': {
      const kind = str(p, 'type')
      const tone = toneOf(kind)
      const Icon = kind === 'tip' ? Lightbulb : tone === 'default' ? CircleAlert : TONE_ICON[tone]
      const title = str(p, 'title')
      return (
        <aside
          aria-label={title || kind || 'Note'}
          className="flex gap-2 rounded-lg border bg-card/60 px-3 py-2"
        >
          <Icon aria-hidden className={cn('mt-0.5 size-3.5 shrink-0', TONE_TEXT[tone])} />
          <div className="min-w-0">
            {title ? <p className="font-semibold">{title}</p> : null}
            <p className="whitespace-pre-wrap text-muted-foreground">{str(p, 'content')}</p>
            {kids}
          </div>
        </aside>
      )
    }
    case 'Timeline':
      return <Timeline props={p} />
    case 'BarChart':
      return <BarChart props={p} />
    case 'Sparkline':
      return <Sparkline props={p} />
    default:
      // A component this Client does not know: what it holds still shows.
      return kids.length > 0 ? <div className="flex flex-col gap-2">{kids}</div> : null
  }
}

function Table({ props }: { props: Record<string, unknown> }) {
  const rows = records(props, 'rows')
  const declared = records(props, 'columns')
  const columns = declared.length
    ? declared.map((c) => ({ key: str(c, 'key'), header: str(c, 'header') || str(c, 'key') }))
    : Object.keys(rows[0] ?? {}).map((key) => ({ key, header: key }))
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full border-collapse text-left text-[12.5px]">
        <thead>
          <tr className="border-b bg-muted/40">
            {columns.map((column) => (
              <th
                key={column.key}
                scope="col"
                className="px-2.5 py-1.5 font-medium whitespace-nowrap"
              >
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {withKeys(rows, (row) => JSON.stringify(row)).map(({ key, item: row }) => (
            <tr key={key} className="border-b last:border-b-0">
              {columns.map((column) => (
                <td key={column.key} className="px-2.5 py-1.5 align-top">
                  {cell(row[column.key])}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function BarChart({ props }: { props: Record<string, unknown> }) {
  const data = records(props, 'data').map((d) => ({
    label: str(d, 'label'),
    value: num(d, 'value') ?? 0,
    tone: toneOf(str(d, 'color')),
  }))
  const max = Math.max(0, ...data.map((d) => d.value))
  const total = data.reduce((sum, d) => sum + Math.max(0, d.value), 0)
  const percent = props['showPercentage'] === true
  return (
    <div
      role="list"
      aria-label="Bar chart"
      className="grid grid-cols-[minmax(0,auto)_1fr_auto] items-center gap-x-2.5 gap-y-1"
    >
      {withKeys(data, (d) => d.label).map(({ key, item: d }) => {
        const shown = percent ? `${Math.round(fraction(d.value, total) * 100)}%` : String(d.value)
        return (
          <div key={key} role="listitem" aria-label={`${d.label}: ${shown}`} className="contents">
            <span className="truncate text-muted-foreground">{d.label}</span>
            <span className="h-2 overflow-hidden rounded-full bg-muted">
              <span
                className={cn('block h-full rounded-full', TONE_FILL[d.tone])}
                style={{ width: `${fraction(d.value, max) * 100}%` }}
              />
            </span>
            <span className="text-right text-[11.5px] tabular-nums">{shown}</span>
          </div>
        )
      })}
    </div>
  )
}

function Sparkline({ props }: { props: Record<string, unknown> }) {
  const data = numbers(props, 'data')
  if (data.length < 2) return null
  const min = Math.min(...data)
  const max = Math.max(...data)
  const width = 120
  const height = 24
  const points = data
    .map((v, i) => {
      const x = (i / (data.length - 1)) * width
      const y = height - 2 - fraction(v - min, max - min) * (height - 4)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
  const tone = toneOf(str(props, 'color'))
  return (
    <svg
      role="img"
      aria-label={`Sparkline from ${data[0]} to ${data[data.length - 1]}, range ${min} to ${max}`}
      viewBox={`0 0 ${width} ${height}`}
      className={cn('h-6 w-[120px]', TONE_TEXT[tone] || 'text-foreground/70')}
    >
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
      />
    </svg>
  )
}

function Timeline({ props }: { props: Record<string, unknown> }) {
  const items = records(props, 'items')
  return (
    <ol className="flex flex-col">
      {withKeys(items, (item) => str(item, 'title')).map(({ key, item }, index) => {
        const tone = toneOf(str(item, 'status'))
        const last = index === items.length - 1
        return (
          <li key={key} className="relative flex gap-2.5 pb-2 last:pb-0">
            <span className="relative flex w-2 shrink-0 justify-center">
              <span
                aria-hidden
                className={cn(
                  'mt-1.5 size-2 rounded-full',
                  TONE_FILL[tone === 'default' ? 'muted' : tone],
                )}
              />
              {last ? null : (
                <span aria-hidden className="absolute top-4 bottom-[-4px] w-px bg-border" />
              )}
            </span>
            <div className="min-w-0">
              <p className="font-medium">
                {str(item, 'title')}
                {str(item, 'status') ? (
                  <span
                    className={cn(
                      'ml-1.5 text-[11px] font-normal',
                      TONE_TEXT[tone] || 'text-muted-foreground',
                    )}
                  >
                    {str(item, 'status')}
                  </span>
                ) : null}
              </p>
              {str(item, 'description') ? (
                <p className="text-muted-foreground">{str(item, 'description')}</p>
              ) : null}
            </div>
          </li>
        )
      })}
    </ol>
  )
}
