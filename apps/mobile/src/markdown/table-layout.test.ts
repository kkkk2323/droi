import type { Table } from 'mdast'
import { describe, expect, test } from 'vitest'
import { parseMarkdown } from './parse'
import { columnWidths } from './table-layout'

const options = { fontSize: 13, padding: 16 }

function table(markdown: string): Table {
  const node = parseMarkdown(markdown).children[0]
  if (node?.type !== 'table') throw new Error('not a table')
  return node
}

describe('columnWidths', () => {
  test('gives one width per column, set by its widest cell in any row', () => {
    const header = columnWidths(table('| a much longer cell | b |\n| - | - |\n| x | y |'), options)
    const body = columnWidths(table('| a | b |\n| - | - |\n| a much longer cell | y |'), options)
    expect(header).toHaveLength(2)
    expect(header[0]).toBeGreaterThan(header[1]!)
    expect(body).toEqual(header)
  })

  test('counts CJK characters about twice as wide as Latin ones', () => {
    const [cjk, latin] = columnWidths(
      table('| 管理员邀请码白名单用户 | abcdefghij |\n| - | - |'),
      options,
    )
    expect(cjk! - options.padding).toBeGreaterThan((latin! - options.padding) * 1.5)
  })

  test('short columns get a floor and long ones a cap, both growing with the text size', () => {
    const [a, ab] = columnWidths(table('| a | ab |\n| - | - |'), options)
    expect(a).toBe(ab)
    const long = 'word '.repeat(60)
    const [capped, alsoCapped] = columnWidths(
      table(`| ${long} | ${long.repeat(2)} |\n| - | - |`),
      options,
    )
    expect(capped).toBe(alsoCapped)
    expect(capped).toBeGreaterThan(a!)
    const [doubled] = columnWidths(table(`| ${long} |\n| - |`), { ...options, fontSize: 26 })
    expect(doubled! - options.padding).toBe(2 * (capped! - options.padding))
  })

  test('measures the text a reader sees, not the Markdown around it', () => {
    const plain = columnWidths(table('| bold link |\n| - |'), options)
    const marked = columnWidths(
      table('| **bold** [link](https://example.com/a/very/long/path/that/does/not/show) |\n| - |'),
      options,
    )
    expect(marked).toEqual(plain)
  })
})
