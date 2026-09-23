import { describe, expect, test } from 'vitest'
import { parseMarkdown, repairStreaming } from './parse'

describe('parseMarkdown', () => {
  test('reads GitHub tables, task lists and code blocks', () => {
    const tree = parseMarkdown(
      '# Plan\n\n- [x] done\n- [ ] next\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n```ts\nconst x = 1\n```\n',
    )
    expect(tree.children.map((node) => node.type)).toEqual(['heading', 'list', 'table', 'code'])
    const list = tree.children[1]
    expect(list?.type === 'list' && list.children.map((item) => item.checked)).toEqual([
      true,
      false,
    ])
    const code = tree.children[3]
    expect(code?.type === 'code' && [code.lang, code.value]).toEqual(['ts', 'const x = 1'])
  })

  test('a streaming reply cut inside a fence renders as a code block', () => {
    const tree = parseMarkdown('Here:\n\n```ts\nconst x', { streaming: true })
    expect(tree.children.map((node) => node.type)).toEqual(['paragraph', 'code'])
  })
})

describe('repairStreaming', () => {
  test('closes an open fence with the same marker', () => {
    expect(repairStreaming('```js\nlet a')).toBe('```js\nlet a\n```')
    expect(repairStreaming('~~~~\nx\n')).toBe('~~~~\nx\n~~~~')
  })

  test('leaves closed fences alone', () => {
    const text = '```\na\n```\nafter'
    expect(repairStreaming(text)).toBe(text)
  })

  test('closes inline code, bold and italic on the last line', () => {
    expect(repairStreaming('run `npm te')).toBe('run `npm te`')
    expect(repairStreaming('this is **impor')).toBe('this is **impor**')
    expect(repairStreaming('an *aside')).toBe('an *aside*')
    expect(repairStreaming('`**` is fine')).toBe('`**` is fine')
  })

  test('a list bullet is not an open italic', () => {
    expect(repairStreaming('* item')).toBe('* item')
  })
})
