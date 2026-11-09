import { describe, expect, test } from 'vitest'
import {
  leafCalls,
  readScriptRun,
  scriptSource,
  scriptSummary,
  scriptToolNames,
} from './script-runs'
import type { ToolCall, ToolResult } from './transcript'

const result = (extra: Partial<ToolResult> = {}): ToolResult => ({
  text: '',
  images: [],
  isError: false,
  interrupted: false,
  blocked: false,
  notes: '',
  details: undefined,
  ...extra,
})

const script = (
  code: string,
  done: Partial<ToolResult> | null = null,
  nested: ToolCall[] = [],
): ToolCall => ({
  use: { id: 'run-1', name: 'codemode', input: { code } },
  result: done === null ? null : result(done),
  nested,
})

const leaf = (name: string): ToolCall => ({
  use: { id: name, name, input: {} },
  result: result({ text: 'ok' }),
})

describe('readScriptRun', () => {
  test('a finished script says what it said, and how many calls it made', () => {
    expect(readScriptRun(script('x', { text: 'two files' }, [leaf('read'), leaf('bash')]))).toEqual(
      {
        output: 'two files',
        images: [],
        status: 'completed',
        error: null,
        stats: '2 calls',
        logPath: null,
        value: null,
      },
    )
  })

  test('a failed script carries its error; a stopped one was cancelled', () => {
    expect(
      readScriptRun(script('x', { isError: true, text: 'ReferenceError: x is not defined' })),
    ).toMatchObject({
      status: 'failed',
      error: 'ReferenceError: x is not defined',
    })
    expect(readScriptRun(script('x', { interrupted: true, text: 'partial' }))).toMatchObject({
      status: 'cancelled',
      output: 'partial',
    })
    expect(readScriptRun(script('x', { blocked: true, notes: 'Denied.' }))).toMatchObject({
      status: 'failed',
      error: 'Denied.',
    })
  })

  test('no result yet', () => {
    expect(readScriptRun(script('x'))).toMatchObject({ status: null, output: '' })
  })
})

describe('scriptSource', () => {
  test("the script's code", () => {
    expect(scriptSource(script('text("hi")'))).toEqual({ script: 'text("hi")', inputs: [] })
    expect(scriptSource(leaf('read'))).toBeNull()
  })
})

describe('scriptToolNames', () => {
  test('each tool once, in order, MCP tools by their whole name', () => {
    expect(
      scriptToolNames(
        'const a = await tools.read({})\nawait tools.bash({})\nawait tools.memory___memory_search({})\nawait tools.read({})',
      ),
    ).toEqual(['read', 'bash', 'memory___memory_search'])
  })
})

describe('scriptSummary', () => {
  test('a script names its tools', () => {
    expect(scriptSummary(script('await tools.read({}); await tools.edit({})'))).toBe('read · edit')
    expect(scriptSummary(leaf('read'))).toBe('')
  })
})

describe('leafCalls', () => {
  test('a script counts as the calls it made', () => {
    const read = leaf('read')
    const grep = leaf('grep')
    expect(leafCalls([script('', null, [read, read]), grep])).toEqual([read, read, grep])
  })
})
