import { describe, expect, test } from 'vitest'
import type { TranscriptEntry } from './transcript'
import { unloadedUserMessages } from './user-messages'

function user(id: string): TranscriptEntry {
  return { id, role: 'user', blocks: [], createdAt: 0, isError: false }
}

describe('unloadedUserMessages', () => {
  const all = [user('a'), user('b'), user('c')]

  test('are the ones ahead of the first loaded message', () => {
    expect(unloadedUserMessages(all, [user('b'), user('c')]).map((e) => e.id)).toEqual(['a'])
  })

  test('are all of them when none is loaded', () => {
    expect(unloadedUserMessages(all, [user('new')]).map((e) => e.id)).toEqual(['a', 'b', 'c'])
  })

  test('are none when the first is loaded', () => {
    expect(unloadedUserMessages(all, [user('a'), user('c')])).toEqual([])
  })
})
