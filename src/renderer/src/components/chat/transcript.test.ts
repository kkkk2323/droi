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
    expect(entries.map((e) => e.role)).toEqual(['user', 'assistant', 'assistant'])
    const [, assistant] = entries
    expect(assistant!.blocks.map((b) => b.kind)).toEqual(['thinking', 'tool'])
    const tool = assistant!.blocks[1]
    if (tool?.kind !== 'tool') throw new Error('expected tool block')
    expect(toolResultText(tool.call.result)).toBe('a\nb')
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
