import { describe, expect, test } from 'vitest'
import {
  createdFileDiff,
  parseDiffResult,
  parseStatusResult,
  permissionDetail,
  toolDisplayName,
  toolSummary,
  toolSummaryParts,
} from './tool-calls'
import type { ToolCall } from './transcript'

const call = (name: string, input: Record<string, unknown>): ToolCall =>
  ({ use: { type: 'tool_use', id: 'c1', name, input }, result: null }) as unknown as ToolCall

describe('toolDisplayName', () => {
  test('splits an MCP tool into its server and tool', () => {
    expect(toolDisplayName('droi-memory___memory_list')).toEqual({
      server: 'droi-memory',
      tool: 'memory_list',
    })
    expect(toolDisplayName('exa___web_search_exa')).toEqual({
      server: 'exa',
      tool: 'web_search_exa',
    })
  })

  test("leaves the Daemon's own tools whole", () => {
    expect(toolDisplayName('Execute')).toEqual({ server: null, tool: 'Execute' })
    expect(toolDisplayName('___odd')).toEqual({ server: null, tool: '___odd' })
    expect(toolDisplayName('odd___')).toEqual({ server: null, tool: 'odd___' })
  })
})

describe('toolSummaryParts', () => {
  test('a headline input stands alone', () => {
    expect(toolSummaryParts(call('Execute', { command: 'ls -la\npwd', summary: 'List' }))).toEqual([
      { key: null, value: 'List' },
    ])
    expect(toolSummaryParts(call('Read', { file_path: '/w/a.ts', limit: 40 }))).toEqual([
      { key: null, value: '/w/a.ts' },
    ])
    expect(toolSummaryParts(call('Skill', { skill: 'grilling' }))).toEqual([
      { key: null, value: 'grilling' },
    ])
  })

  test('any other tool shows its first short inputs as key: value', () => {
    expect(toolSummaryParts(call('droi-memory___memory_list', { scope: 'project' }))).toEqual([
      { key: 'scope', value: 'project' },
    ])
    expect(
      toolSummaryParts(
        call('droi-memory___memory_add', {
          scope: 'project',
          category: 'insight',
          text: `${'x'.repeat(70)}\nsecond line`,
          extra: 'not shown',
        }),
      ),
    ).toEqual([
      { key: 'scope', value: 'project' },
      { key: 'category', value: 'insight' },
      { key: 'text', value: `${'x'.repeat(57)}…` },
    ])
  })

  test('skips blanks and nested values, keeps numbers and booleans', () => {
    expect(
      toolSummaryParts(call('t', { a: '  ', b: { c: 1 }, d: [1], limit: 10, block: false })),
    ).toEqual([
      { key: 'limit', value: '10' },
      { key: 'block', value: 'false' },
    ])
    expect(toolSummaryParts(call('t', {}))).toEqual([])
  })
})

describe('toolSummary', () => {
  test('joins the parts into one line for labels and the Phone App', () => {
    expect(toolSummary(call('Execute', { command: 'ls' }))).toBe('ls')
    expect(
      toolSummary(call('droi-memory___memory_list', { scope: 'project', category: 'insight' })),
    ).toBe('scope: project · category: insight')
    expect(toolSummary(call('t', {}))).toBe('')
  })
})

describe('parseStatusResult', () => {
  test("reads Create's bare success object", () => {
    expect(parseStatusResult('{"success":true,"file_path":"/w/a.ts"}')).toEqual({
      success: true,
      message: null,
    })
    expect(parseStatusResult('{"success":false,"error":"EACCES: permission denied"}')).toEqual({
      success: false,
      message: 'EACCES: permission denied',
    })
  })

  test('anything with more to say is left as it came', () => {
    expect(parseStatusResult('{"success":true,"stdout":"hi"}')).toBeNull()
    expect(parseStatusResult('[Process exited with code 0]')).toBeNull()
    expect(parseStatusResult('{"success":"yes"}')).toBeNull()
    expect(parseStatusResult('{"success": not json')).toBeNull()
  })
})

describe('parseDiffResult', () => {
  test("reads the Daemon's Edit result into lines with both numbers", () => {
    const result = parseDiffResult(
      JSON.stringify({
        success: true,
        file_path: '/w/a.ts',
        diffLines: [
          { type: 'unchanged', content: 'a', lineNumber: { old: 1, new: 1 } },
          { type: 'removed', content: 'b', lineNumber: { old: 2 } },
          { type: 'added', content: 'c', lineNumber: { new: 2 } },
          { type: 'added', content: 'd', lineNumber: { new: 3 } },
        ],
      }),
    )
    expect(result).toEqual({
      added: 2,
      removed: 1,
      lines: [
        { type: 'unchanged', content: 'a', old: 1, new: 1 },
        { type: 'removed', content: 'b', old: 2, new: null },
        { type: 'added', content: 'c', old: null, new: 2 },
        { type: 'added', content: 'd', old: null, new: 3 },
      ],
    })
  })

  test('other results are left alone', () => {
    expect(parseDiffResult('[Process exited with code 0]')).toBeNull()
    expect(parseDiffResult('{"success":true}')).toBeNull()
    expect(parseDiffResult('{"diffLines": not json')).toBeNull()
  })
})

describe('createdFileDiff', () => {
  test("shows a Create's content as numbered added lines, ignoring the final newline", () => {
    expect(createdFileDiff(call('Create', { file_path: 'a.ts', content: 'one\ntwo\n' }))).toEqual({
      lines: [
        { type: 'added', content: 'one', old: null, new: 1 },
        { type: 'added', content: 'two', old: null, new: 2 },
      ],
      added: 2,
      removed: 0,
    })
  })

  test('is null for other tools and for a Create without content', () => {
    expect(createdFileDiff(call('Edit', { content: 'x' }))).toBeNull()
    expect(createdFileDiff(call('Create', { file_path: 'a.ts' }))).toBeNull()
  })
})

describe('permissionDetail', () => {
  test('shows the full command the Daemon worked out, before the raw input', () => {
    expect(permissionDetail({ fullCommand: 'npm test -- --watch' }, { command: 'npm test' })).toBe(
      '$ npm test -- --watch',
    )
  })

  test('shows the file an edit touches', () => {
    expect(permissionDetail({ filePath: '/w/a.ts' }, { file_path: '/w/a.ts' })).toBe('/w/a.ts')
  })

  test('falls back to the input: its command, else all of it', () => {
    expect(permissionDetail(undefined, { command: 'ls' })).toBe('$ ls')
    expect(permissionDetail(null, { url: 'https://x' })).toBe('{"url":"https://x"}')
  })
})
