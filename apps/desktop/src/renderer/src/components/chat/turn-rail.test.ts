import { describe, expect, test } from 'vitest'
import type { TranscriptEntry } from '@droi/daemon-layer/transcript'
import { activeItem, railItems } from './turn-rail'

function entry(role: TranscriptEntry['role'], ...texts: string[]): TranscriptEntry {
  return {
    id: Math.random().toString(36),
    role,
    blocks: texts.map((text, i) => ({ kind: 'text', id: String(i), text })),
    createdAt: 0,
    isError: false,
  }
}

describe('railItems', () => {
  test('one item per user message, with the start of the first reply that has text', () => {
    const items = railItems([
      entry('assistant', 'leading'),
      entry('user', 'first\n  question'),
      entry('assistant'),
      entry('assistant', 'the  answer'),
      entry('assistant', 'more'),
      entry('user', 'second'),
    ])
    expect(items).toMatchObject([
      { index: 1, loaded: true, prompt: 'first question', response: 'the answer' },
      { index: 5, loaded: true, prompt: 'second', response: '' },
    ])
  })

  test('messages from before the loaded ones go ahead of their row, with no reply yet', () => {
    const earlier = entry('user', 'from the compacted Session')
    const older = entry('user', 'not loaded')
    const items = railItems(
      [
        earlier,
        entry('assistant', 'old reply'),
        entry('assistant', 'middle of a reply'),
        entry('user', 'loaded'),
      ],
      [older],
      2,
    )
    expect(
      items.map(({ id, ...item }) => ({ id: id === older.id ? 'older' : 'other', ...item })),
    ).toEqual([
      {
        id: 'other',
        index: 0,
        loaded: true,
        prompt: 'from the compacted Session',
        response: 'old reply',
      },
      { id: 'older', index: 2, loaded: false, prompt: 'not loaded', response: '' },
      { id: 'other', index: 3, loaded: true, prompt: 'loaded', response: '' },
    ])
  })

  test('with nothing loaded after them, they come last', () => {
    const items = railItems([entry('user', 'a')], [entry('user', 'b')], 1)
    expect(items.map((item) => [item.prompt, item.loaded])).toEqual([
      ['a', true],
      ['b', false],
    ])
  })

  test('a message with only images still gets a mark', () => {
    const image: TranscriptEntry = {
      ...entry('user'),
      blocks: [{ kind: 'image', id: '0', src: 'data:' }],
    }
    expect(railItems([image])[0]?.prompt).toBe('Attached image')
  })
})

describe('activeItem', () => {
  const items = railItems([
    entry('user', 'a'),
    entry('assistant', 'x'),
    entry('user', 'b'),
    entry('assistant', 'y'),
  ])
  test('is the turn holding the row', () => {
    expect(activeItem(items, 0)).toBe(0)
    expect(activeItem(items, 1)).toBe(0)
    expect(activeItem(items, 3)).toBe(1)
  })
  test('is the last turn while the row is unknown', () => {
    expect(activeItem(items, null)).toBe(1)
  })
  test('a row before the first loaded message belongs to the last one not loaded', () => {
    const partial = railItems(
      [entry('assistant', 'end of a reply'), entry('user', 'c')],
      [entry('user', 'a'), entry('user', 'b')],
    )
    expect(activeItem(partial, 0)).toBe(1)
    expect(activeItem(partial, 1)).toBe(2)
  })
})
