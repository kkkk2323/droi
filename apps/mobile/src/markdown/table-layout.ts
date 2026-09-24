// Column widths for a Markdown table. React Native has no table layout, so
// each row lays out on its own; giving every cell of a column the same width
// is what lines the columns up. The width is estimated from the cell text
// rather than measured, so a table renders in one pass and stays steady while
// a reply streams; a cell that comes out wider than its estimate wraps.
import type { Nodes, Table } from 'mdast'

// Geist's Latin glyphs average a little over half an em; wide (CJK, fullwidth,
// emoji) glyphs are a full em.
const NARROW_EM = 0.6
const WIDE_EM = 1
// In ems of the table's text, so both grow with the reader's text size.
const MIN_EM = 4
const MAX_EM = 17

const WIDE =
  /[\u1100-\u115F\u2E80-\u303E\u3041-\u33FF\u3400-\u4DBF\u4E00-\u9FFF\uA000-\uA4CF\uAC00-\uD7A3\uF900-\uFAFF\uFE30-\uFE4F\uFF00-\uFF60\uFFE0-\uFFE6\u{1F300}-\u{1FAFF}\u{20000}-\u{3FFFD}]/u

export function columnWidths(
  table: Table,
  { fontSize, padding }: { fontSize: number; padding: number },
): number[] {
  const ems: number[] = []
  for (const row of table.children) {
    row.children.forEach((cell, c) => {
      const widest = Math.max(...visibleText(cell).split('\n').map(lineEm))
      ems[c] = Math.max(ems[c] ?? 0, widest)
    })
  }
  return Array.from(ems, (em = 0) => {
    const clamped = Math.min(Math.max(em, MIN_EM), MAX_EM)
    return Math.ceil(clamped * fontSize) + padding
  })
}

function visibleText(node: Nodes): string {
  if (node.type === 'break') return '\n'
  if ('value' in node && node.type !== 'html') return node.value
  if ('children' in node) return node.children.map(visibleText).join('')
  return ''
}

function lineEm(line: string): number {
  let em = 0
  for (const char of line) em += WIDE.test(char) ? WIDE_EM : NARROW_EM
  return em
}
