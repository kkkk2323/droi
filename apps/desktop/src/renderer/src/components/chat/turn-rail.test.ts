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
    expect(items).toEqual([
      { index: 1, prompt: 'first question', response: 'the answer' },
      { index: 5, prompt: 'second', response: '' },
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
})
