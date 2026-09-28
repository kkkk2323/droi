import { fromMarkdown } from 'mdast-util-from-markdown'
import { gfmFromMarkdown } from 'mdast-util-gfm'
import { gfm } from 'micromark-extension-gfm'
import { describe, expect, test } from 'vitest'
import { parseMarkdown, repairStreaming } from './parse'

/** The tree a streaming text gets when it is parsed whole. */
function wholeParse(text: string) {
  return fromMarkdown(repairStreaming(text), {
    extensions: [gfm()],
    mdastExtensions: [gfmFromMarkdown()],
  })
}

const REPLIES = [
  [
    '## 修复方案',
    '',
    'First change `parse.ts` so the **settled** blocks are kept,',
    'then the *tail* is parsed again. See https://example.com/docs.',
    'Setext heading',
    '==============',
    '',
    '- one',
    '- two with `code`',
    '  - nested',
    '',
    '- three, which makes the list loose',
    '',
    '1. first',
    '2. second',
    '',
    '- [x] done',
    '- [ ] ~~dropped~~ next',
    '',
    '> a quote',
    'with a lazy line',
    '',
    '---',
    '',
    '| 状态 | Count |',
    '| :- | -: |',
    '| ok | 1 |',
    '| **bold** | 2 |',
    '',
    '```ts',
    'const a = 1',
    '',
    'function f(x: number) {',
    '  return x * 2',
    '}',
    '```',
    '',
    '~~~~',
    'tilde fence with ``` inside',
    '~~~~',
    '',
    '<details>',
    '<summary>More</summary>',
    '</details>',
    '',
    '    indented code',
    '',
    'Last paragraph with a [link](https://example.com) and _emphasis_.',
  ].join('\n'),
  [
    'See [the docs][docs] and a note[^1].',
    '',
    'More text.',
    '',
    '[docs]: https://example.com',
    '[^1]: The note.',
  ].join('\n'),
  'Before\n\n* item\n* **bold item\n\nAfter the list `x` and *more',
]

describe('parseMarkdown while streaming', () => {
  test.each(REPLIES.map((reply, i) => [i, reply] as const))(
    'every prefix of reply %i parses as the whole prefix does',
    (_, reply) => {
      for (let end = 1; end <= reply.length; end++) {
        const prefix = reply.slice(0, end)
        expect(parseMarkdown(prefix, { streaming: true }).children, prefix).toEqual(
          wholeParse(prefix).children,
        )
      }
    },
  )

  test('keeps the blocks above the one the line being written may still join', () => {
    const first = parseMarkdown('# Title\n\nOne.\n\nTwo.\n\nTh', { streaming: true })
    const next = parseMarkdown('# Title\n\nOne.\n\nTwo.\n\nThree', { streaming: true })
    expect(next.children[0]).toBe(first.children[0])
    expect(next.children[1]).toBe(first.children[1])
    expect(next.children[2]).not.toBe(first.children[2])
  })

  test('a text that changed rather than grew is parsed whole', () => {
    parseMarkdown('# Old\n\nText that', { streaming: true })
    const tree = parseMarkdown('# New\n\nText', { streaming: true })
    expect(tree.children).toEqual(wholeParse('# New\n\nText').children)
  })
})

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
