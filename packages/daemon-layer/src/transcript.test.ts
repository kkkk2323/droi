import { describe, expect, test } from 'vitest'
import type { Entry, LiveDoc, PiMessage } from './kernel-wire'
import {
  buildTranscript,
  formatDuration,
  formatTurnEnd,
  reuseUnchanged,
  toolResultImages,
  toolResultText,
  turnEnds,
} from './transcript'

let nextId = 100
const entry = (kind: string, model: PiMessage[], data?: Record<string, unknown>): Entry => ({
  id: nextId++,
  conversationId: 7,
  kind,
  model,
  ...(data ? { data } : {}),
})
const user = (content: PiMessage['content'], timestamp = 1): Entry =>
  entry('pi.user', [{ role: 'user', content, timestamp }])
const assistant = (content: unknown[], extra: Partial<PiMessage> = {}, timestamp = 2): Entry =>
  entry('pi.assistant', [
    {
      role: 'assistant',
      content: content as PiMessage['content'],
      stopReason: 'stop',
      timestamp,
      ...extra,
    },
  ])
const call = (id: string, name: string, args: Record<string, unknown> = {}) => ({
  type: 'toolCall',
  id,
  name,
  arguments: args,
})
const result = (
  id: string,
  name: string,
  text: string,
  extra: Partial<PiMessage> = {},
  data?: Record<string, unknown>,
): Entry =>
  entry(
    'pi.tool-result',
    [
      {
        role: 'toolResult',
        toolCallId: id,
        toolName: name,
        content: [{ type: 'text', text }],
        isError: false,
        timestamp: 3,
        ...extra,
      },
    ],
    data,
  )

describe('buildTranscript', () => {
  test('a tool round joins its answer, with each call carrying its result', () => {
    const entries = buildTranscript([
      user('run it'),
      assistant(
        [
          { type: 'thinking', thinking: 'plan', durationMs: 42 },
          call('c1', 'bash', { command: 'ls' }),
        ],
        { stopReason: 'toolUse' },
      ),
      result('c1', 'bash', 'a\nb\n'),
      assistant([{ type: 'text', text: 'done' }], {}, 4),
    ])
    expect(entries.map((e) => e.role)).toEqual(['user', 'assistant'])
    expect(entries[0]!.blocks).toEqual([{ kind: 'text', id: expect.any(String), text: 'run it' }])
    const answer = entries[1]!
    expect(answer.blocks.map((b) => b.kind)).toEqual(['thinking', 'tools', 'text'])
    expect(answer.createdAt).toBe(4)
    const tools = answer.blocks[1]
    if (tools?.kind !== 'tools') throw new Error('expected tools')
    expect(tools.calls[0]!.use).toEqual({ id: 'c1', name: 'bash', input: { command: 'ls' } })
    expect(toolResultText(tools.calls[0]!.result)).toBe('a\nb')
  })

  test('calls with nothing between them are one cluster across entries', () => {
    const entries = buildTranscript([
      user('go'),
      assistant([call('a', 'read')], { stopReason: 'toolUse' }),
      result('a', 'read', 'x'),
      assistant([call('b', 'read')], { stopReason: 'toolUse' }),
      result('b', 'read', 'y'),
    ])
    const blocks = entries[1]!.blocks
    expect(blocks).toHaveLength(1)
    if (blocks[0]?.kind !== 'tools') throw new Error('expected tools')
    expect(blocks[0].calls.map((c) => c.use.id)).toEqual(['a', 'b'])
  })

  test('a call still running has no result, and one on a stopped answer was interrupted', () => {
    const running = buildTranscript([
      user('go'),
      assistant([call('a', 'bash')], { stopReason: 'toolUse' }),
    ])
    const block = running[1]!.blocks[0]
    if (block?.kind !== 'tools') throw new Error('expected tools')
    expect(block.calls[0]!.result).toBeNull()
    const stopped = buildTranscript([
      user('go'),
      assistant([call('a', 'bash')], { stopReason: 'aborted' }),
    ])
    const cut = stopped[1]!.blocks[0]
    if (cut?.kind !== 'tools') throw new Error('expected tools')
    expect(cut.calls[0]!.result).toMatchObject({ interrupted: true, isError: false })
  })

  test("the harness's rendering of its diagnostics is left out, and its ending is read", () => {
    const diagnostics = [{ severity: 'info', code: 'aborted', message: 'The run was stopped.' }]
    const entries = buildTranscript([
      user('go'),
      assistant([call('a', 'bash')], { stopReason: 'toolUse' }),
      entry(
        'pi.tool-result',
        [
          {
            role: 'toolResult',
            toolCallId: 'a',
            toolName: 'bash',
            isError: true,
            content: [
              { type: 'text', text: 'partial' },
              { type: 'text', text: '<harness>\n[info] The run was stopped.\n</harness>' },
            ],
          },
        ],
        { diagnostics },
      ),
    ])
    const block = entries[1]!.blocks[0]
    if (block?.kind !== 'tools') throw new Error('expected tools')
    expect(block.calls[0]!.result).toMatchObject({
      text: 'partial',
      notes: 'The run was stopped.',
      interrupted: true,
      isError: false,
    })
  })

  test('pictures, sent and handed back, are data URLs', () => {
    const entries = buildTranscript([
      user([
        { type: 'text', text: 'see' },
        { type: 'image', data: 'AAAA', mimeType: 'image/png' },
      ]),
      assistant([call('a', 'read', { path: 'x.png' })], { stopReason: 'toolUse' }),
      entry('pi.tool-result', [
        {
          role: 'toolResult',
          toolCallId: 'a',
          toolName: 'read',
          content: [{ type: 'image', data: 'BBBB', mimeType: 'image/jpeg' }],
        },
      ]),
    ])
    expect(entries[0]!.blocks[1]).toEqual({
      kind: 'image',
      id: expect.any(String),
      src: 'data:image/png;base64,AAAA',
    })
    const block = entries[1]!.blocks[0]
    if (block?.kind !== 'tools') throw new Error('expected tools')
    expect(toolResultImages(block.calls[0]!.result)).toEqual(['data:image/jpeg;base64,BBBB'])
  })

  test("a script's calls hang under it: those it made, and those it is making", () => {
    const done = buildTranscript([
      user('go'),
      assistant([call('s', 'codemode', { code: 'await tools.read({path: "a"})' })], {
        stopReason: 'toolUse',
      }),
      result('s', 'codemode', 'out', {
        details: {
          calls: [{ id: 1, name: 'read', arguments: { path: 'a' }, status: 'ok', output: 'A' }],
        },
      }),
    ])
    const block = done[1]!.blocks[0]
    if (block?.kind !== 'tools') throw new Error('expected tools')
    expect(block.calls[0]!.nested).toEqual([
      {
        use: { id: 's/c1', name: 'read', input: { path: 'a' } },
        result: expect.objectContaining({ text: 'A' }),
      },
    ])
    const live: LiveDoc = {
      run: { taskId: 1, inputs: [], turns: 1 },
      nestedTools: [
        {
          parentCallId: 's',
          callId: 'n1',
          name: 'bash',
          arguments: { command: 'ls' },
          status: 'running',
        },
      ],
    }
    const running = buildTranscript(
      [user('go'), assistant([call('s', 'codemode', { code: 'x' })], { stopReason: 'toolUse' })],
      live,
    )
    const runningBlock = running[1]!.blocks[0]
    if (runningBlock?.kind !== 'tools') throw new Error('expected tools')
    expect(runningBlock.calls[0]!.nested).toEqual([
      { use: { id: 'n1', name: 'bash', input: { command: 'ls' } }, result: null },
    ])
  })

  test('an answer after a compaction or a note opens its own row', () => {
    const entries = buildTranscript([
      user('go'),
      assistant([{ type: 'text', text: 'one' }]),
      entry('pi.compaction', [{ role: 'user', content: 'summary' }]),
      assistant([{ type: 'text', text: 'two' }]),
      entry('droi.note', [], { text: 'Imported from droid', at: 5 }),
      assistant([{ type: 'text', text: 'three' }]),
    ])
    expect(entries.map((e) => (e.blocks[0] as { text: string }).text)).toEqual([
      'go',
      'one',
      'two',
      'Imported from droid',
      'three',
    ])
    expect(entries[3]!.note).toBe(true)
  })

  test('the answer being written closes the transcript while the run lasts', () => {
    const stored = [
      user('go'),
      assistant([call('a', 'read')], { stopReason: 'toolUse' }),
      result('a', 'read', 'x'),
    ]
    const live: LiveDoc = {
      run: { taskId: 9, inputs: [], turns: 1 },
      generation: {
        attempt: 1,
        message: { role: 'assistant', content: [{ type: 'text', text: 'Hel' }] },
      } as LiveDoc['generation'],
    }
    const writing = buildTranscript(stored, live)
    expect(writing).toHaveLength(2)
    expect(writing[1]!.streaming).toBe(true)
    expect(writing[1]!.blocks.map((b) => b.kind)).toEqual(['tools', 'text'])
    // The stored answer's own row is not changed by the one being written.
    const without = buildTranscript(stored)
    expect(without[1]!.blocks).toHaveLength(1)
    expect(buildTranscript(stored, { ...live, run: null })).toHaveLength(2)
  })

  test('a run that ended without an answer says why, and a stopped one says nothing', () => {
    const failed = buildTranscript([user('go')], {
      run: null,
      runFailure: { taskId: 3, reason: 'model_error', detail: 'Rate limited' },
    })
    expect(failed[1]).toMatchObject({ isError: true, blocks: [{ text: 'Rate limited' }] })
    expect(
      buildTranscript([user('go')], { runFailure: { taskId: 3, reason: 'aborted' } }),
    ).toHaveLength(1)
    const empty = buildTranscript([user('go'), assistant([], { stopReason: 'aborted' })])
    expect(empty[1]).toMatchObject({ blocks: [{ text: 'Stopped' }] })
  })

  test('a user message with nothing to show is no row, but still ends the turn before it', () => {
    const entries = buildTranscript([
      user('go'),
      assistant([{ type: 'text', text: 'one' }]),
      user([]),
      assistant([{ type: 'text', text: 'two' }]),
    ])
    expect(entries).toHaveLength(3)
  })
})

describe('turnEnds', () => {
  const text = (t: string) => [{ type: 'text', text: t }]

  test('a reply followed by a user message closes the turn, with the time since the prompt', () => {
    const entries = buildTranscript([
      user('go', 1_000),
      assistant(text('done'), {}, 5_000),
      user('thanks', 9_000),
      assistant(text('welcome'), {}, 9_500),
    ])
    const ends = turnEnds(entries, true)
    expect(ends.get(entries[1]!.id)).toEqual({ endedAt: 5_000, startedAt: 1_000 })
    expect(ends.has(entries[3]!.id)).toBe(false)
    expect(turnEnds(entries, false).get(entries[3]!.id)).toEqual({
      endedAt: 9_500,
      startedAt: 9_000,
    })
  })

  test('the last entry closes once the Kernel rests, even on tool calls', () => {
    const entries = buildTranscript([
      user('go', 1_000),
      assistant([call('a', 'read')], { stopReason: 'toolUse' }, 2_000),
    ])
    expect(turnEnds(entries, true).size).toBe(0)
    expect(turnEnds(entries, false).get(entries[1]!.id)).toEqual({
      endedAt: 2_000,
      startedAt: 1_000,
    })
  })
})

describe('formatTurnEnd', () => {
  const now = new Date(2026, 8, 29, 23, 30)
  const endedAt = new Date(2026, 8, 29, 23, 13).getTime()
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
  const asked = user([
    { type: 'text', text: 'look' },
    { type: 'image', data: 'AAAA', mimeType: 'image/png' },
  ])
  const reading = assistant([call('c', 'read')], { stopReason: 'toolUse' })
  const answered = result('c', 'read', 'x')
  const copy = (e: Entry): Entry => JSON.parse(JSON.stringify(e))

  test('keeps every entry, and the list, when nothing changed', () => {
    const first = buildTranscript([asked, reading, answered])
    expect(reuseUnchanged(first, buildTranscript([asked, reading, answered].map(copy)))).toBe(first)
  })

  test('replaces only the entry that changed', () => {
    const first = buildTranscript([asked, reading])
    const next = reuseUnchanged(first, buildTranscript([asked, reading, answered].map(copy)))
    expect(next[0]).toBe(first[0])
    expect(next[1]).not.toBe(first[1])
  })

  test('a streamed answer keeps earlier entries and renews the growing one', () => {
    const live = (t: string): LiveDoc => ({
      run: { taskId: 1, inputs: [], turns: 1 },
      generation: {
        attempt: 1,
        message: { role: 'assistant', content: [{ type: 'text', text: t }] },
      } as LiveDoc['generation'],
    })
    const first = buildTranscript([asked], live('Hel'))
    const next = reuseUnchanged(first, buildTranscript([asked], live('Hello')))
    expect(next[0]).toBe(first[0])
    expect(next[1]!.blocks[0]).toMatchObject({ text: 'Hello' })
  })
})
