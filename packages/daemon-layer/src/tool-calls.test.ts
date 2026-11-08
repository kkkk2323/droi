import { describe, expect, test } from 'vitest'
import {
  approvalDetail,
  clusterLabel,
  createdFileDiff,
  parseEditDiff,
  parseStatusResult,
  readToolResult,
  toolDisplayName,
  toolSummary,
  toolSummaryParts,
} from './tool-calls'
import type { ToolCall, ToolResult } from './transcript'

const call = (name: string, input: Record<string, unknown>): ToolCall => ({
  use: { id: 'c1', name, input },
  result: null,
})

const result = (extra: Partial<ToolResult> = {}): ToolResult => ({
  text: 'ok',
  images: [],
  isError: false,
  interrupted: false,
  blocked: false,
  notes: '',
  details: undefined,
  ...extra,
})

describe('toolDisplayName', () => {
  test('splits an MCP tool into its server and tool', () => {
    expect(toolDisplayName('memory___memory_list')).toEqual({
      server: 'memory',
      tool: 'memory_list',
    })
    expect(toolDisplayName('exa___web_search_exa')).toEqual({
      server: 'exa',
      tool: 'web_search_exa',
    })
  })

  test("leaves the Kernel's own tools whole", () => {
    expect(toolDisplayName('bash')).toEqual({ server: null, tool: 'bash' })
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
    expect(toolSummaryParts(call('memory___memory_list', { scope: 'project' }))).toEqual([
      { key: 'scope', value: 'project' },
    ])
    expect(
      toolSummaryParts(
        call('memory___memory_add', {
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
      toolSummary(call('memory___memory_list', { scope: 'project', category: 'insight' })),
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

describe('parseEditDiff', () => {
  test("reads Pi's edit diff into lines with their numbers", () => {
    const diff = ['  1 a', '-2 b', '+2 c', '+3 d', '    ...', ' 9 z'].join('\n')
    expect(parseEditDiff({ diff, patch: '…', firstChangedLine: 2 })).toEqual({
      added: 2,
      removed: 1,
      lines: [
        { type: 'unchanged', content: 'a', old: 1, new: null },
        { type: 'removed', content: 'b', old: 2, new: null },
        { type: 'added', content: 'c', old: null, new: 2 },
        { type: 'added', content: 'd', old: null, new: 3 },
        { type: 'unchanged', content: '…', old: null, new: null },
        { type: 'unchanged', content: 'z', old: 9, new: null },
      ],
    })
  })

  test('other details are left alone', () => {
    expect(parseEditDiff(undefined)).toBeNull()
    expect(parseEditDiff({ truncation: {} })).toBeNull()
    expect(parseEditDiff({ diff: '' })).toBeNull()
  })

  test("an edit's row reads its diff; a failed one its error", () => {
    const edit = {
      ...call('edit', { path: 'a.ts' }),
      result: result({ details: { diff: '+1 x' } }),
    }
    expect(readToolResult(edit).diff?.added).toBe(1)
    const failed = {
      ...edit,
      result: result({ isError: true, text: 'no match', details: { diff: '+1 x' } }),
    }
    expect(readToolResult(failed)).toMatchObject({ isError: true, diff: null, text: 'no match' })
    const stopped = { ...edit, result: result({ interrupted: true, text: 'Interrupted' }) }
    expect(readToolResult(stopped)).toMatchObject({ interrupted: true, isError: false, diff: null })
  })
})

describe('createdFileDiff', () => {
  test("shows a write's content as numbered added lines, ignoring the final newline", () => {
    expect(createdFileDiff(call('write', { path: 'a.ts', content: 'one\ntwo\n' }))).toEqual({
      lines: [
        { type: 'added', content: 'one', old: null, new: 1 },
        { type: 'added', content: 'two', old: null, new: 2 },
      ],
      added: 2,
      removed: 0,
    })
  })

  test('is null for other tools and for a write without content', () => {
    expect(createdFileDiff(call('edit', { content: 'x' }))).toBeNull()
    expect(createdFileDiff(call('write', { path: 'a.ts' }))).toBeNull()
  })
})

describe('approvalDetail', () => {
  test('shows the command, the script, or the file', () => {
    expect(approvalDetail('bash', { command: 'rm -rf build' })).toBe('$ rm -rf build')
    expect(approvalDetail('bash_background', { command: 'npm run dev' })).toBe('$ npm run dev')
    expect(approvalDetail('codemode', { code: 'await tools.read({})' })).toBe(
      'await tools.read({})',
    )
    expect(approvalDetail('write', { path: 'a.ts', content: 'x' })).toBe('a.ts')
  })

  test('falls back to all the arguments', () => {
    expect(approvalDetail('exa___search', { query: 'x' })).toBe('{\n  "query": "x"\n}')
  })
})

describe('clusterLabel', () => {
  const done = (name: string, nested?: ToolCall[]): ToolCall => ({
    use: { id: name, name, input: {} },
    result: result(),
    ...(nested ? { nested } : {}),
  })

  test('a script counts as the calls it made', () => {
    expect(clusterLabel([done('codemode', [done('read'), done('bash')]), done('write')])).toEqual({
      label: 'Used 3 tools',
      pending: false,
    })
    expect(clusterLabel([done('codemode', [done('read')])]).label).toBe('Used read')
    expect(clusterLabel([done('codemode', [])]).label).toBe('Used codemode')
  })

  test("pending while a script's call is still running", () => {
    expect(clusterLabel([done('codemode', [call('bash', {})])])).toEqual({
      label: 'Running bash',
      pending: true,
    })
  })
})
