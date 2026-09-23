import { describe, expect, test } from 'vitest'
import type { FactoryDroidMessage } from '@factory/droid-sdk'
import { buildTranscript, toolResultText } from './transcript'

const message = (
  role: 'user' | 'assistant' | 'tool',
  content: unknown[],
  extra: Partial<FactoryDroidMessage> = {},
): FactoryDroidMessage =>
  ({
    id: `${role}-${Math.random()}`,
    role,
    content,
    createdAt: 1,
    updatedAt: 1,
    ...extra,
  }) as FactoryDroidMessage

describe('buildTranscript', () => {
  test('attaches tool results to their tool calls and drops tool messages', () => {
    const entries = buildTranscript([
      message('user', [{ type: 'text', text: 'run it' }]),
      message('assistant', [
        { type: 'thinking', thinking: 'plan', durationMs: 42 },
        { type: 'tool_use', id: 'call-1', name: 'Execute', input: { command: 'ls' } },
      ]),
      message('tool', [{ type: 'tool_result', toolUseId: 'call-1', content: 'a\nb' }]),
      message('assistant', [{ type: 'text', text: 'done' }]),
    ])
    expect(entries.map((e) => e.role)).toEqual(['user', 'assistant'])
    const [, assistant] = entries
    expect(assistant!.blocks.map((b) => b.kind)).toEqual(['thinking', 'tools', 'text'])
    const tools = assistant!.blocks[1]
    if (tools?.kind !== 'tools') throw new Error('expected tools block')
    expect(toolResultText(tools.calls[0]!.result)).toBe('a\nb')
  })

  test('inline images become image blocks with a data URL', () => {
    const entries = buildTranscript([
      message('user', [
        { type: 'text', text: 'see' },
        { type: 'image', source: { type: 'base64', mediaType: 'image/png', data: 'AAAA' } },
      ]),
    ])
    expect(entries[0]!.blocks).toEqual([
      { kind: 'text', id: expect.any(String), text: 'see' },
      { kind: 'image', id: expect.any(String), src: 'data:image/png;base64,AAAA' },
    ])
  })

  test('a Task call stands on its own and splits the tool run around it', () => {
    const entries = buildTranscript([
      message('assistant', [
        { type: 'tool_use', id: 'a', name: 'Read', input: {} },
        { type: 'tool_use', id: 't', name: 'Task', input: { subagent_type: 'explorer' } },
        { type: 'tool_use', id: 'b', name: 'Grep', input: {} },
      ]),
      message('tool', [{ type: 'tool_result', toolUseId: 't', content: 'report' }]),
    ])
    const blocks = entries[0]!.blocks
    expect(blocks.map((b) => b.kind)).toEqual(['tools', 'subagent', 'tools'])
    const task = blocks[1]
    if (task?.kind !== 'subagent') throw new Error('expected subagent block')
    expect(toolResultText(task.call.result)).toBe('report')
  })

  test('consecutive tool calls form one cluster; text splits clusters', () => {
    const entries = buildTranscript([
      message('assistant', [
        { type: 'tool_use', id: 'a', name: 'Read', input: {} },
        { type: 'tool_use', id: 'b', name: 'Grep', input: {} },
        { type: 'text', text: 'found it' },
        { type: 'tool_use', id: 'c', name: 'Edit', input: {} },
      ]),
    ])
    const blocks = entries[0]!.blocks
    expect(blocks.map((b) => (b.kind === 'tools' ? b.calls.length : b.kind))).toEqual([
      2,
      'text',
      1,
    ])
  })

  test('consecutive assistant messages merge into one entry; a user turn splits them', () => {
    const entries = buildTranscript([
      message('assistant', [{ type: 'tool_use', id: 'a', name: 'Read', input: {} }], {
        createdAt: 1,
      }),
      message('assistant', [{ type: 'tool_use', id: 'b', name: 'Grep', input: {} }], {
        createdAt: 2,
      }),
      message('assistant', [{ type: 'text', text: 'done' }], { createdAt: 3 }),
      message('user', [{ type: 'text', text: 'thanks' }]),
      message('assistant', [{ type: 'text', text: 'welcome' }]),
    ])
    expect(entries.map((e) => e.role)).toEqual(['assistant', 'user', 'assistant'])
    const first = entries[0]!
    expect(first.blocks.map((b) => (b.kind === 'tools' ? b.calls.length : b.kind))).toEqual([
      2,
      'text',
    ])
    expect(first.createdAt).toBe(3)
  })

  test('skips empty assistant messages and hidden messages', () => {
    const entries = buildTranscript([
      message('assistant', []),
      message('user', [{ type: 'text', text: 'hidden' }], { isUserVisible: false }),
      message('assistant', [{ type: 'text', text: '' }]),
    ])
    expect(entries).toEqual([])
  })

  test('tool results may be block arrays', () => {
    expect(
      toolResultText({
        type: 'tool_result',
        toolUseId: 'x',
        content: [{ type: 'text', text: 'hi' }],
      } as never),
    ).toBe('hi')
  })
})
