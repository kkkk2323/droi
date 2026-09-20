import { describe, expect, test } from 'vitest'
import { filterSlashItems, slashQuery, type SlashItem } from './use-slash-items'

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
