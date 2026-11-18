import { describe, expect, test } from 'vitest'
import { snippetRuns } from './use-session-search'

describe('snippetRuns', () => {
  test('splits a snippet into plain and matched runs', () => {
    expect(snippetRuns('…the <mark>sidebar</mark> width is off')).toEqual([
      { text: '…the ', match: false, offset: 0 },
      { text: 'sidebar', match: true, offset: 5 },
      { text: ' width is off', match: false, offset: 12 },
    ])
  })

  test('drops other tags and collapses line breaks', () => {
    expect(snippetRuns('a\n  <b>b</b> <mark>c</mark>')).toEqual([
      { text: 'a b ', match: false, offset: 0 },
      { text: 'c', match: true, offset: 4 },
    ])
  })

  test('an empty snippet has no runs', () => {
    expect(snippetRuns('')).toEqual([])
  })
})
