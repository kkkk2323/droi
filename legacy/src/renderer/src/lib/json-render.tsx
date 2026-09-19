import { cn } from '@/lib/utils'

interface UIElement {
  type: string
  props: Record<string, unknown>
  children?: string[]
}

export interface JsonRenderSpec {
  root: string
  elements: Record<string, UIElement>
}

interface RenderContext {
  spec: JsonRenderSpec
}

function renderElement(id: string, ctx: RenderContext): React.ReactNode {
  const el = ctx.spec.elements[id]
  if (!el) return null
  const childNodes = el.children?.map((cid) => renderElement(cid, ctx))
  return (
    <ElementRenderer key={id} element={el}>
      {childNodes}
    </ElementRenderer>
  )
}

function ElementRenderer({
  element,
  children,
}: {
  element: UIElement
  children?: React.ReactNode[]
}) {
  const p = element.props ?? {}
  switch (element.type) {
    case 'Box':
      return <BoxComponent props={p}>{children}</BoxComponent>
    case 'Text':
      return <TextComponent props={p} />
    case 'Heading':
      return <HeadingComponent props={p} />
    case 'Divider':
    case 'Separator':
      return <DividerComponent props={p} />
    case 'Newline':
      return <br />
    case 'Spacer':
      return <div style={{ height: `${((p.size as number) ?? 1) * 8}px` }} />
    case 'Badge':
      return <BadgeComponent props={p} />
    case 'Card':
      return <CardComponent props={p}>{children}</CardComponent>
    case 'StatusLine':
      return <StatusLineComponent props={p} />
    case 'KeyValue':
      return <KeyValueComponent props={p} />
    case 'Metric':
      return <MetricComponent props={p} />
    case 'ProgressBar':
      return <ProgressBarComponent props={p} />
    case 'BarChart':
      return <BarChartComponent props={p} />
    case 'Sparkline':
      return <SparklineComponent props={p} />
    case 'Table':
      return <TableComponent props={p} />
    case 'List':
      return <ListComponent props={p} />
    case 'Timeline':
      return <TimelineComponent props={p} />
    case 'Callout':
      return <CalloutComponent props={p} />
    default:
      return <div>{children}</div>
  }
}

type P = Record<string, unknown>

function BoxComponent({ props: p, children }: { props: P; children?: React.ReactNode }) {
  const dir = (p.flexDirection as string) ?? 'column'
  const gap = p.gap as number | undefined
  const padding = p.padding as number | undefined
  return (
    <div
      className={cn('flex', dir === 'row' ? 'flex-row flex-wrap' : 'flex-col')}
      style={{
        ...(gap != null ? { gap: `${gap * 8}px` } : {}),
        ...(padding != null ? { padding: `${padding * 8}px` } : {}),
      }}
    >
      {children}
    </div>
  )
}

function TextComponent({ props: p }: { props: P }) {
  const text = (p.text as string) ?? ''
  const bold = p.bold as boolean | undefined
  const color = p.color as string | undefined
  return (
    <span className={cn('text-sm', bold && 'font-bold')} style={color ? { color } : undefined}>
      {text}
    </span>
  )
}

function HeadingComponent({ props: p }: { props: P }) {
  const text = (p.text as string) ?? ''
  const level = (p.level as string) ?? 'h2'
  const cls =
    level === 'h1'
      ? 'text-xl font-bold'
      : level === 'h2'
        ? 'text-lg font-semibold'
        : 'text-base font-medium'
  return <div className={cls}>{text}</div>
}

function DividerComponent({ props: p }: { props: P }) {
  const title = p.title as string | undefined
  if (title) {
    return (
      <div className="flex items-center gap-2 my-1">
        <div className="flex-1 h-px bg-border" />
        <span className="text-xs text-muted-foreground">{title}</span>
        <div className="flex-1 h-px bg-border" />
      </div>
    )
  }
  return <div className="h-px bg-border my-1" />
}

function BadgeComponent({ props: p }: { props: P }) {
  const label = (p.label as string) ?? ''
  const variant = (p.variant as string) ?? 'default'
  const colors: Record<string, string> = {
    info: 'bg-blue-500/15 text-blue-500',
    success: 'bg-emerald-500/15 text-emerald-500',
    warning: 'bg-amber-500/15 text-amber-500',
    error: 'bg-red-500/15 text-red-500',
    default: 'bg-muted text-foreground',
  }
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium',
        colors[variant] ?? colors.default,
      )}
    >
      {label}
    </span>
  )
}

function CardComponent({ props: p, children }: { props: P; children?: React.ReactNode }) {
  const title = p.title as string | undefined
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      {title && <div className="font-semibold mb-2">{title}</div>}
      {children}
    </div>
  )
}

const STATUS_STYLES: Record<string, { dot: string; color: string; bg: string }> = {
  success: {
    dot: '\u25CF',
    color: 'text-emerald-500',
    bg: 'bg-emerald-500/10 border-emerald-500/20',
  },
  error: { dot: '\u25CF', color: 'text-red-500', bg: 'bg-red-500/10 border-red-500/20' },
  warning: { dot: '\u25B2', color: 'text-amber-500', bg: 'bg-amber-500/10 border-amber-500/20' },
  info: { dot: '\u2139', color: 'text-blue-500', bg: 'bg-blue-500/10 border-blue-500/20' },
}

function StatusLineComponent({ props: p }: { props: P }) {
  const text = (p.text as string) ?? ''
  const status = (p.status as string) ?? 'info'
  const s = STATUS_STYLES[status] ?? STATUS_STYLES.info
  return (
    <div
      className={cn('inline-flex items-center gap-2 rounded-md border px-2.5 py-1 text-sm', s.bg)}
    >
      <span className={cn('text-xs', s.color)}>{s.dot}</span>
      <span>{text}</span>
    </div>
  )
}

function KeyValueComponent({ props: p }: { props: P }) {
  const label = (p.label as string) ?? ''
  const value = String(p.value ?? '')
  return (
    <div className="flex items-baseline justify-between gap-4 py-0.5">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-sm font-medium">{value}</span>
    </div>
  )
}

function MetricComponent({ props: p }: { props: P }) {
  const label = (p.label as string) ?? ''
  const value = (p.value as string) ?? ''
  const trend = p.trend as string | undefined
  const detail = p.detail as string | undefined
  const trendColor =
    trend === 'up' ? 'text-emerald-500' : trend === 'down' ? 'text-red-500' : undefined
  const trendPrefix = trend === 'up' ? '\u2191' : trend === 'down' ? '\u2193' : ''
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="flex items-baseline gap-1.5">
        <span className="text-xl font-semibold tracking-tight">{value}</span>
        {(trend || detail) && (
          <span className={cn('text-sm font-medium', trendColor)}>
            {trendPrefix}
            {detail ?? ''}
          </span>
        )}
      </div>
    </div>
  )
}

function ProgressBarComponent({ props: p }: { props: P }) {
  const progress = Math.max(0, Math.min(1, (p.progress as number) ?? 0))
  const label = p.label as string | undefined
  return (
    <div className="flex flex-col gap-1">
      {label && <span className="text-xs text-muted-foreground">{label}</span>}
      <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
        <div className="h-full rounded-full bg-primary" style={{ width: `${progress * 100}%` }} />
      </div>
    </div>
  )
}

interface ChartItem {
  label: string
  value: number
  color?: string
}

function BarChartComponent({ props: p }: { props: P }) {
  const data = (p.data as ChartItem[]) ?? []
  if (data.length === 0) return null
  const maxVal = Math.max(...data.map((d) => d.value), 1)
  return (
    <div className="flex flex-col gap-1.5">
      {data.map((item, i) => {
        const pct = (item.value / maxVal) * 100
        return (
          <div key={i} className="flex items-center gap-2 text-sm">
            <span className="w-20 shrink-0 truncate text-muted-foreground">{item.label}</span>
            <div className="flex-1 h-5 rounded-sm overflow-hidden bg-muted">
              <div
                className="h-full rounded-sm bg-primary"
                style={{
                  width: `${pct}%`,
                  ...(item.color ? { backgroundColor: item.color } : {}),
                }}
              />
            </div>
            <span className="w-12 shrink-0 text-right font-mono text-xs tabular-nums">
              {item.value}
            </span>
          </div>
        )
      })}
    </div>
  )
}

function SparklineComponent({ props: p }: { props: P }) {
  const data = (p.data as number[]) ?? []
  if (data.length < 2) return null
  const min = Math.min(...data)
  const max = Math.max(...data)
  const range = max - min || 1
  const w = 120
  const h = 24
  const pts = data
    .map((v, i) => {
      const x = (i / (data.length - 1)) * w
      const y = h - ((v - min) / range) * h
      return `${x},${y}`
    })
    .join(' ')
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="inline-block h-6 w-32" preserveAspectRatio="none">
      <polyline
        fill="none"
        stroke={(p.color as string) ?? 'currentColor'}
        strokeWidth="2"
        points={pts}
        className="text-primary"
      />
    </svg>
  )
}

interface TableColumn {
  header: string
  key: string
  width?: number
  align?: string
}

function TableComponent({ props: p }: { props: P }) {
  const columns = (p.columns as TableColumn[]) ?? []
  const rows = (p.rows as Record<string, unknown>[]) ?? []
  if (columns.length === 0) return null
  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-border">
          {columns.map((col) => (
            <th
              key={col.key}
              className={cn(
                'py-1 px-2 text-left text-xs font-medium text-muted-foreground',
                col.align === 'right' && 'text-right',
                col.align === 'center' && 'text-center',
              )}
            >
              {col.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr key={i} className="border-b border-border/50 last:border-0">
            {columns.map((col) => (
              <td
                key={col.key}
                className={cn(
                  'py-1 px-2',
                  col.align === 'right' && 'text-right',
                  col.align === 'center' && 'text-center',
                )}
              >
                {String(row[col.key] ?? '')}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function ListComponent({ props: p }: { props: P }) {
  const items = (p.items as string[]) ?? []
  const ordered = p.ordered as boolean | undefined
  const Tag = ordered ? 'ol' : 'ul'
  return (
    <Tag className={cn('ml-4 space-y-0.5 text-sm', ordered ? 'list-decimal' : 'list-disc')}>
      {items.map((item, i) => (
        <li key={i}>{item}</li>
      ))}
    </Tag>
  )
}

interface TimelineItem {
  title: string
  description?: string
  date?: string
  status?: string
}

const TIMELINE_STYLES: Record<string, string> = {
  completed: 'bg-emerald-500',
  current: 'bg-blue-500',
  success: 'bg-emerald-500',
  error: 'bg-red-500',
  warning: 'bg-amber-500',
  info: 'bg-blue-500',
  upcoming: 'bg-muted-foreground/40',
}

function TimelineComponent({ props: p }: { props: P }) {
  const items = (p.items as TimelineItem[]) ?? []
  return (
    <div className="flex flex-col">
      {items.map((item, i) => (
        <div key={i} className="flex gap-3 pb-3 last:pb-0">
          <div className="flex flex-col items-center">
            <div
              className={cn(
                'h-2.5 w-2.5 rounded-full shrink-0 mt-1',
                TIMELINE_STYLES[item.status ?? 'upcoming'] ?? 'bg-muted-foreground/40',
              )}
            />
            {i < items.length - 1 && <div className="w-px flex-1 bg-border mt-1" />}
          </div>
          <div className="flex-1 min-w-0">
            <div className="text-sm font-medium">{item.title}</div>
            {item.description && (
              <div className="text-xs text-muted-foreground">{item.description}</div>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}

function CalloutComponent({ props: p }: { props: P }) {
  const type = (p.type as string) ?? 'info'
  const title = p.title as string | undefined
  const content = (p.content as string) ?? ''
  const styles: Record<string, string> = {
    info: 'border-blue-500/30 bg-blue-500/5',
    warning: 'border-amber-500/30 bg-amber-500/5',
    error: 'border-red-500/30 bg-red-500/5',
    success: 'border-emerald-500/30 bg-emerald-500/5',
    tip: 'border-emerald-500/30 bg-emerald-500/5',
    important: 'border-purple-500/30 bg-purple-500/5',
  }
  return (
    <div className={cn('rounded-lg border p-3', styles[type] ?? styles.info)}>
      {title && <div className="font-medium text-sm mb-1">{title}</div>}
      {content && <div className="text-sm text-muted-foreground">{content}</div>}
    </div>
  )
}

export function JsonRenderRenderer({ spec }: { spec: JsonRenderSpec }) {
  if (!spec.root || !spec.elements[spec.root]) return null
  return <>{renderElement(spec.root, { spec })}</>
}
