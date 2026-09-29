import { describe, expect, test } from 'vitest'
import type { FactoryDroidMessage } from '@factory/droid-sdk'
import {
  buildTranscript,
  formatDuration,
  formatTurnEnd,
  reuseUnchanged,
  toolResultImages,
  toolResultText,
  turnEnds,
} from './transcript'

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

  test('skips empty messages and hidden messages', () => {
    const entries = buildTranscript([
      message('assistant', []),
      message('user', [{ type: 'text', text: 'hidden' }], { isUserVisible: false }),
      message('assistant', [{ type: 'text', text: '' }]),
      // The Daemon's record of a hook run: a user message with nothing in it.
      message('user', [], { hookEventName: 'UserPromptSubmit', hookStatus: 'completed' } as never),
    ])
    expect(entries).toEqual([])
  })

  test("a hook record between two assistant messages does not split the assistant's entry", () => {
    const entries = buildTranscript([
      message('assistant', [{ type: 'tool_use', id: 'a', name: 'Read', input: {} }]),
      message('user', [], { hookEventName: 'UserPromptSubmit' } as never),
      message('assistant', [{ type: 'text', text: 'done' }]),
    ])
    expect(entries.map((e) => e.role)).toEqual(['assistant'])
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

  test('a tool result carries the pictures it handed back, the text without them', () => {
    const result = {
      type: 'tool_result',
      toolUseId: 'x',
      content: [
        { type: 'text', text: 'Image file: shot.png' },
        { type: 'image', source: { type: 'base64', mediaType: 'image/png', data: 'AAAA' } },
        { type: 'image', source: { type: 'url', url: 'https://example.com/a.png' } },
      ],
    } as never
    expect(toolResultText(result)).toBe('Image file: shot.png')
    expect(toolResultImages(result)).toEqual(['data:image/png;base64,AAAA'])
    expect(
      toolResultImages({ type: 'tool_result', toolUseId: 'x', content: 'x' } as never),
    ).toEqual([])
    expect(toolResultImages(null)).toEqual([])
  })
})

describe('turnEnds', () => {
  const at = (role: 'user' | 'assistant', createdAt: number, content: unknown[]) =>
    message(role, content, { createdAt, updatedAt: createdAt })
  const text = (t: string) => [{ type: 'text', text: t }]
  const tool = (id: string) => [{ type: 'tool_use', id, name: 'Read', input: {} }]

  test('a reply followed by a user message closes the turn, with the time since the prompt', () => {
    const entries = buildTranscript([
      at('user', 1_000, text('go')),
      at('assistant', 5_000, text('done')),
      at('user', 9_000, text('thanks')),
      at('assistant', 9_500, text('welcome')),
    ])
    const ends = turnEnds(entries, true)
    expect(ends.get(entries[1]!.id)).toEqual({ endedAt: 5_000, startedAt: 1_000 })
    // The last entry is still being written.
    expect(ends.has(entries[3]!.id)).toBe(false)
    expect(turnEnds(entries, false).get(entries[3]!.id)).toEqual({
      endedAt: 9_500,
      startedAt: 9_000,
    })
  })

  test('a user message that steers a turn in progress does not close it', () => {
    const entries = buildTranscript([
      at('user', 1_000, text('go')),
      at('assistant', 2_000, tool('a')),
      at('user', 3_000, text('also check b')),
      at('assistant', 8_000, [...tool('b'), ...text('both done')]),
    ])
    const ends = turnEnds(entries, false)
    expect(ends.has(entries[1]!.id)).toBe(false)
    // The turn's duration counts from the prompt that started it.
    expect(ends.get(entries[3]!.id)).toEqual({ endedAt: 8_000, startedAt: 1_000 })
  })

  test('the last entry closes once the Daemon rests, even on tool calls', () => {
    const entries = buildTranscript([
      at('user', 1_000, text('go')),
      at('assistant', 2_000, tool('a')),
    ])
    expect(turnEnds(entries, true).size).toBe(0)
    expect(turnEnds(entries, false).get(entries[1]!.id)).toEqual({
      endedAt: 2_000,
      startedAt: 1_000,
    })
  })

  test('a turn with no prompt before it has a time but no duration', () => {
    const entries = buildTranscript([at('assistant', 2_000, text('hi'))])
    expect(turnEnds(entries, false).get(entries[0]!.id)).toEqual({
      endedAt: 2_000,
      startedAt: null,
    })
  })
})

describe('formatTurnEnd', () => {
  const now = new Date(2026, 8, 29, 23, 30)
  const endedAt = new Date(2026, 8, 29, 23, 13).getTime()

  // The clock time follows the locale (23:13 or 11:13 PM).
  const time = String.raw`^\d{1,2}:13(?: PM)?`

  test('the time alone without a start, or when the turn took under a second', () => {
    expect(formatTurnEnd({ endedAt, startedAt: null }, now)).toMatch(new RegExp(`${time}$`))
    expect(formatTurnEnd({ endedAt, startedAt: endedAt - 400 }, now)).not.toContain('took')
  })

  test('the time and the duration', () => {
    expect(formatTurnEnd({ endedAt, startedAt: endedAt - 252_000 }, now)).toMatch(
      new RegExp(`${time} · took 4m 12s$`),
    )
  })
})

describe('formatDuration', () => {
  test('picks the unit for the size', () => {
    expect(formatDuration(640)).toBe('640 ms')
    expect(formatDuration(12_400)).toBe('12s')
    expect(formatDuration(252_000)).toBe('4m 12s')
    expect(formatDuration(3_780_000)).toBe('1h 03m')
  })
})

describe('reuseUnchanged', () => {
  // The SDK copies every message on each read but keeps the blocks inside.
  const copies = (messages: FactoryDroidMessage[]) => messages.map((m) => ({ ...m }))
  const use = { type: 'tool_use', id: 'call-1', name: 'Read', input: {} }
  const image = { type: 'image', source: { type: 'base64', mediaType: 'image/png', data: 'AAAA' } }
  const asked = message('user', [{ type: 'text', text: 'look' }, image])
  const reading = message('assistant', [use])
  const answered = message('tool', [{ type: 'tool_result', toolUseId: 'call-1', content: 'x' }])

  test('keeps every entry, and the list, when nothing changed', () => {
    const first = buildTranscript([asked, reading, answered])
    const again = reuseUnchanged(first, buildTranscript(copies([asked, reading, answered])))
    expect(again).toBe(first)
  })

  test('replaces only the entry that changed', () => {
    const first = buildTranscript([asked, reading])
    const next = reuseUnchanged(first, buildTranscript(copies([asked, reading, answered])))
    expect(next).not.toBe(first)
    expect(next[0]).toBe(first[0])
    expect(next[1]).not.toBe(first[1])
  })

  test('a streamed text keeps earlier entries and renews the growing one', () => {
    const reply = (text: string) => message('assistant', [{ type: 'text', text }], { id: 'reply' })
    const first = buildTranscript([asked, reply('Hel')])
    const next = reuseUnchanged(first, buildTranscript([asked, reply('Hello')]))
    expect(next[0]).toBe(first[0])
    expect(next[1]).not.toBe(first[1])
    expect(next[1]!.blocks[0]).toMatchObject({ text: 'Hello' })
  })
})
