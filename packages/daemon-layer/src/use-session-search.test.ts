import { describe, expect, test } from 'vitest'
import { markedRuns } from './use-session-search'

describe('markedRuns', () => {
  test('marks every place the text has the query, in any case', () => {
    expect(markedRuns('the Sidebar width, the sidebar', 'sidebar')).toEqual([
      { text: 'the ', match: false, offset: 0 },
      { text: 'Sidebar', match: true, offset: 4 },
      { text: ' width, the ', match: false, offset: 11 },
      { text: 'sidebar', match: true, offset: 23 },
    ])
  })

  test('collapses line breaks', () => {
    expect(markedRuns('a\n  b c', 'c')).toEqual([
      { text: 'a b ', match: false, offset: 0 },
      { text: 'c', match: true, offset: 4 },
    ])
  })

  test('text without the query is one plain run; empty text has none', () => {
    expect(markedRuns('nothing here', 'zz')).toEqual([
      { text: 'nothing here', match: false, offset: 0 },
    ])
    expect(markedRuns('', 'x')).toEqual([])
  })
})
