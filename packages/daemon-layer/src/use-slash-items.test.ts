import { describe, expect, test } from 'vitest'
import {
  filterSlashItems,
  mergeSlashItems,
  pickedSlashItem,
  slashQuery,
  type SlashItem,
} from './use-slash-items'

const item = (name: string, description = '', kind: SlashItem['kind'] = 'command'): SlashItem => ({
  name,
  description,
  argumentHint: null,
  kind,
})

describe('slashQuery', () => {
  test('is the word after a leading slash while the caret is still inside it', () => {
    expect(slashQuery('/', 1)).toBe('')
    expect(slashQuery('/han', 4)).toBe('han')
    expect(slashQuery('/han', 2)).toBe('h')
  })

  test('is null once arguments start, or when the slash is not first', () => {
    expect(slashQuery('/handoff ', 9)).toBeNull()
    expect(slashQuery('/handoff foo', 12)).toBeNull()
    expect(slashQuery('hello /x', 8)).toBeNull()
    expect(slashQuery('', 0)).toBeNull()
  })
})

describe('filterSlashItems', () => {
  const items = [
    item('opsx-propose', 'Propose a change'),
    item('handoff', 'Compact the conversation', 'skill'),
    item('review', 'Review the diff for a proposal', 'skill'),
  ]

  test('prefix matches first, then names containing the query', () => {
    expect(filterSlashItems(items, 'pro').map((i) => i.name)).toEqual(['opsx-propose'])
    expect(filterSlashItems(items, 'rev').map((i) => i.name)).toEqual(['review'])
    expect(filterSlashItems(items, 'op').map((i) => i.name)).toEqual(['opsx-propose'])
    expect(filterSlashItems(items, '').map((i) => i.name)).toEqual([
      'opsx-propose',
      'handoff',
      'review',
    ])
  })

  test('caps the list', () => {
    const many = Array.from({ length: 20 }, (_, i) => item(`cmd${i}`))
    expect(filterSlashItems(many, 'cmd')).toHaveLength(8)
  })
})

describe('mergeSlashItems', () => {
  test('builtins come first and shadow a Daemon item of the same name', () => {
    const merged = mergeSlashItems(
      [item('compact', 'builtin')],
      [item('compact', 'workspace'), item('handoff', '', 'skill')],
    )
    expect(merged.map((i) => [i.name, i.description])).toEqual([
      ['compact', 'builtin'],
      ['handoff', ''],
    ])
  })

  test('without builtins only the Daemon items are offered', () => {
    expect(mergeSlashItems([], [item('handoff', '', 'skill')]).map((i) => i.name)).toEqual([
      'handoff',
    ])
    expect(mergeSlashItems([], [])).toEqual([])
  })
})

describe('pickedSlashItem', () => {
  const items = [item('handoff', '', 'skill'), item('compact')]

  test('is the known name a message starts with, once a space ends it', () => {
    expect(pickedSlashItem('/handoff carry on', items)).toEqual({
      item: items[0],
      rest: 'carry on',
    })
    expect(pickedSlashItem('/compact ', items)).toEqual({ item: items[1], rest: '' })
  })

  test('is nothing while the name is being typed, or for an unknown one', () => {
    expect(pickedSlashItem('/handoff', items)).toBeNull()
    expect(pickedSlashItem('/nope carry on', items)).toBeNull()
    expect(pickedSlashItem('say /handoff ', items)).toBeNull()
  })
})
