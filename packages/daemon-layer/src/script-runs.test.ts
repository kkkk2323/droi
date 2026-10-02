import { describe, expect, test } from 'vitest'
import {
  leafCalls,
  readScriptRun,
  scriptSource,
  scriptSummary,
  scriptToolNames,
} from './script-runs'
import type { ToolCall } from './transcript'

const call = (
  name: string,
  input: Record<string, unknown>,
  content: unknown = null,
  isError = false,
): ToolCall =>
  ({
    use: { type: 'tool_use', id: 'run-1', name, input },
    result: content === null ? null : { type: 'tool_result', toolUseId: 'run-1', content, isError },
  }) as unknown as ToolCall

const LOG = '/home/dev/.factory/artifacts/scripts/s1/run-1.log'

describe('readScriptRun', () => {
  test('splits what the model read from the closing line and the status', () => {
    const run = readScriptRun(
      call('WaitForScript', { toolCallId: 'run-1' }, [
        { type: 'text', text: '# probe workspace\nline two\n\nslept' },
        {
          type: 'text',
          text: `[Script completed · 3 calls · 86 B in sandbox · 33 B emitted (38%) · retained: r1 Read README.md, r2 Execute Sleep · log: ${LOG}]`,
        },
        { type: 'text', text: '{"toolCallId":"run-1","status":"completed","result":null}' },
      ]),
    )
    expect(run).toEqual({
      output: '# probe workspace\nline two\n\nslept',
      images: [],
      status: 'completed',
      error: null,
      stats: '3 calls · 86 B in sandbox · 33 B emitted (38%)',
      logPath: LOG,
      value: null,
    })
  })

  test('a run still going after the first minute answers with its status alone', () => {
    const run = readScriptRun(
      call('Script', { script: '' }, '{"toolCallId":"run-1","status":"running"}'),
    )
    expect(run).toMatchObject({ status: 'running', output: '', stats: null, logPath: null })
  })

  test('a failed run carries its error, a returned value is shown as JSON', () => {
    expect(
      readScriptRun(
        call('Script', { script: '' }, [
          {
            type: 'text',
            text: '[Script failed at 2:5 · 1 call completed · 0 B emitted · log: /l]',
          },
          {
            type: 'text',
            text: '{"toolCallId":"run-1","status":"failed","error":"ReferenceError: x is not defined"}',
          },
        ]),
      ),
    ).toMatchObject({
      status: 'failed',
      error: 'ReferenceError: x is not defined',
      stats: '1 call completed · 0 B emitted',
      logPath: '/l',
    })
    expect(
      readScriptRun(
        call('Script', { script: '' }, [
          {
            type: 'text',
            text: '{"toolCallId":"run-1","status":"completed","result":{"files":2}}',
          },
        ]),
      ).value,
    ).toBe('{\n  "files": 2\n}')
  })

  test('a call the Daemon refused reads as a failure in its own words', () => {
    expect(
      readScriptRun(call('Script', { script: '' }, 'Script is not available', true)),
    ).toMatchObject({ status: 'failed', error: 'Script is not available', output: '' })
  })

  test('no result yet', () => {
    expect(readScriptRun(call('Script', { script: '' }))).toMatchObject({
      status: null,
      output: '',
    })
  })
})

describe('scriptSource', () => {
  test('the program and its named inputs', () => {
    expect(
      scriptSource(call('Script', { script: 'text(inputs.body)', inputs: { body: 'hi', n: 3 } })),
    ).toEqual({ script: 'text(inputs.body)', inputs: [{ name: 'body', text: 'hi' }] })
    expect(scriptSource(call('WaitForScript', { toolCallId: 'run-1' }))).toBeNull()
  })
})

describe('scriptToolNames', () => {
  test('each tool once, in order, MCP tools by their own name', () => {
    expect(
      scriptToolNames(
        'const a = await tools.Read({})\nawait tools.Execute({})\nawait tools.droi_memory.memory_search({})\nawait tools.Read({})',
      ),
    ).toEqual(['Read', 'Execute', 'memory_search'])
  })
})

describe('scriptSummary', () => {
  test('a Script names its tools; a wait says it continues or stops the run', () => {
    expect(
      scriptSummary(call('Script', { script: 'await tools.Grep({}); await tools.Edit({})' })),
    ).toBe('Grep · Edit')
    expect(scriptSummary(call('WaitForScript', { toolCallId: 'run-1' }))).toBe('continued')
    expect(scriptSummary(call('WaitForScript', { toolCallId: 'run-1', kill: true }))).toBe(
      'stopped',
    )
  })
})

describe('leafCalls', () => {
  test('a Script counts as the calls it made', () => {
    const read = call('Read', {})
    const script = { ...call('Script', { script: '' }), nested: [read, read] }
    const grep = call('Grep', {})
    expect(leafCalls([script, grep])).toEqual([read, read, grep])
  })
})
