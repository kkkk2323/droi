// Rich terminal output in an assistant's reply: a `<json-render>` tag holding
// one line of JSON that describes a small tree of display components (a
// table, a status line, a bar chart). The Droid CLI draws it in the terminal;
// the Clients split the reply around it and draw the tree with their own
// components. A tag inside a code fence is text about the format, not output.

export interface RenderElement {
  type: string
  props: Record<string, unknown>
  children: string[]
}

export interface RenderSpec {
  root: string
  elements: Record<string, RenderElement>
}

export type ReplySegment =
  | { kind: 'markdown'; text: string }
  | { kind: 'render'; spec: RenderSpec }
  /** A tag still open: the reply is streaming, or it ended without closing it. */
  | { kind: 'pending'; raw: string }

const OPEN = '<json-render>'
const CLOSE = '</json-render>'
const FENCE = /^\s{0,3}(```|~~~)/

export function hasRenderTag(text: string): boolean {
  return text.includes(OPEN)
}

export function splitReply(text: string): ReplySegment[] {
  if (!hasRenderTag(text)) return [{ kind: 'markdown', text }]
  const segments: ReplySegment[] = []
  let markdown: string[] = []
  let capturing: string | null = null
  let fence: string | null = null

  const flush = () => {
    const joined = markdown.join('\n')
    if (joined.trim()) segments.push({ kind: 'markdown', text: joined })
    markdown = []
  }
  const close = (raw: string) => {
    flush()
    const spec = parseRenderSpec(raw)
    // What cannot be drawn is still shown, as the JSON it is.
    segments.push(
      spec
        ? { kind: 'render', spec }
        : { kind: 'markdown', text: '```json\n' + raw.trim() + '\n```' },
    )
  }

  for (const whole of text.split('\n')) {
    let line = whole
    if (capturing !== null) {
      const end = line.indexOf(CLOSE)
      if (end < 0) {
        capturing += '\n' + line
        continue
      }
      close(capturing + '\n' + line.slice(0, end))
      capturing = null
      line = line.slice(end + CLOSE.length)
      if (!line.trim()) continue
    }
    const marker = FENCE.exec(line)?.[1]
    if (marker) {
      if (fence === null) fence = marker
      else if (marker === fence) fence = null
      markdown.push(line)
      continue
    }
    if (fence !== null) {
      markdown.push(line)
      continue
    }
    for (;;) {
      const start = openTag(line)
      if (start < 0) {
        if (line.trim() || line === whole) markdown.push(line)
        break
      }
      const before = line.slice(0, start)
      if (before.trim()) markdown.push(before)
      const after = line.slice(start + OPEN.length)
      const end = after.indexOf(CLOSE)
      if (end < 0) {
        capturing = after
        break
      }
      close(after.slice(0, end))
      line = after.slice(end + CLOSE.length)
      if (!line.trim()) break
    }
  }
  if (capturing !== null) {
    flush()
    segments.push({ kind: 'pending', raw: capturing })
  }
  flush()
  return segments
}

/** Where the line's first tag opens; one inside an inline code span is text. */
function openTag(line: string): number {
  const spans = codeSpans(line)
  for (let at = line.indexOf(OPEN); at >= 0; at = line.indexOf(OPEN, at + 1)) {
    if (!spans.some(([start, end]) => at > start && at < end)) return at
  }
  return -1
}

/** The line's code spans: a backtick run up to the next run of the same length. */
function codeSpans(line: string): Array<[number, number]> {
  const spans: Array<[number, number]> = []
  let open: { at: number; length: number } | null = null
  for (const run of line.matchAll(/`+/g)) {
    if (open === null) open = { at: run.index, length: run[0].length }
    else if (run[0].length === open.length) {
      spans.push([open.at, run.index])
      open = null
    }
  }
  return spans
}

export function parseRenderSpec(raw: string): RenderSpec | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw.trim())
  } catch {
    return null
  }
  if (!isObject(parsed)) return null
  const root = parsed['root']
  const elements = parsed['elements']
  if (typeof root !== 'string' || !isObject(elements)) return null
  const normalized: Record<string, RenderElement> = {}
  for (const [id, value] of Object.entries(elements)) {
    if (!isObject(value) || typeof value['type'] !== 'string') continue
    normalized[id] = {
      type: value['type'],
      props: isObject(value['props']) ? value['props'] : {},
      children: Array.isArray(value['children'])
        ? value['children'].filter((c): c is string => typeof c === 'string')
        : [],
    }
  }
  if (!normalized[root]) return null
  return { root, elements: normalized }
}

/**
 * The elements under `id` that exist, each drawn once: a spec that lists an
 * element twice, or loops back to an ancestor, still draws a finite tree.
 */
export function childrenOf(spec: RenderSpec, id: string, seen: ReadonlySet<string>): string[] {
  const element = spec.elements[id]
  if (!element) return []
  return element.children.filter((child) => spec.elements[child] && !seen.has(child))
}

// Prop readers: the model writes the props, so each takes what it can and
// falls back instead of throwing.

export function str(props: Record<string, unknown>, key: string): string {
  const value = props[key]
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

export function num(props: Record<string, unknown>, key: string): number | null {
  const value = props[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() && Number.isFinite(Number(value)))
    return Number(value)
  return null
}

export function records(
  props: Record<string, unknown>,
  key: string,
): Array<Record<string, unknown>> {
  const value = props[key]
  return Array.isArray(value) ? value.filter(isObject) : []
}

export function strings(props: Record<string, unknown>, key: string): string[] {
  const value = props[key]
  if (!Array.isArray(value)) return []
  return value.filter((v) => typeof v === 'string' || typeof v === 'number').map((v) => String(v))
}

export function numbers(props: Record<string, unknown>, key: string): number[] {
  const value = props[key]
  if (!Array.isArray(value)) return []
  return value.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
}

/** A cell's text: strings and numbers as they are, anything else as JSON. */
export function cell(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return JSON.stringify(value)
}

/**
 * The meaning behind a colour or status name, so each Client draws it with
 * its own tokens. Colour carries status only; anything else reads as plain.
 */
export type Tone = 'default' | 'muted' | 'success' | 'warning' | 'error' | 'info'

const TONES: Record<string, Tone> = {
  success: 'success',
  ok: 'success',
  done: 'success',
  completed: 'success',
  green: 'success',
  warning: 'warning',
  warn: 'warning',
  pending: 'warning',
  yellow: 'warning',
  error: 'error',
  danger: 'error',
  failed: 'error',
  red: 'error',
  info: 'info',
  tip: 'info',
  note: 'info',
  blue: 'info',
  cyan: 'info',
  gray: 'muted',
  grey: 'muted',
  dim: 'muted',
  muted: 'muted',
  secondary: 'muted',
}

export function toneOf(name: string): Tone {
  return TONES[name.trim().toLowerCase()] ?? 'default'
}

/** A fraction for a bar, clamped to 0..1. */
export function fraction(value: number, max: number): number {
  if (!(max > 0)) return 0
  return Math.min(1, Math.max(0, value / max))
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
