import { describe, expect, test } from 'vitest'
import { childrenOf, parseRenderSpec, splitReply, toneOf } from './json-render'

const TABLE =
  '{"root":"t","elements":{"t":{"type":"Table","props":{"columns":[{"header":"A","key":"a"}],"rows":[{"a":"1"}]},"children":[]}}}'

describe('splitReply', () => {
  test('a reply without the tag is one Markdown segment', () => {
    expect(splitReply('Just **text**')).toEqual([{ kind: 'markdown', text: 'Just **text**' }])
  })

  test('the tag on its own line splits the reply around a drawn tree', () => {
    const segments = splitReply(`Before\n\n<json-render>${TABLE}</json-render>\n\nAfter`)
    expect(segments.map((s) => s.kind)).toEqual(['markdown', 'render', 'markdown'])
    expect(segments[0]).toEqual({ kind: 'markdown', text: 'Before\n' })
    expect(segments[2]).toEqual({ kind: 'markdown', text: '\nAfter' })
    const render = segments[1]
    if (render?.kind !== 'render') throw new Error('expected a render segment')
    expect(render.spec.root).toBe('t')
    expect(render.spec.elements['t']?.type).toBe('Table')
  })

  test('a tag mid-line and one spanning lines both split', () => {
    expect(splitReply(`See <json-render>${TABLE}</json-render> below`).map((s) => s.kind)).toEqual([
      'markdown',
      'render',
      'markdown',
    ])
    expect(splitReply(`<json-render>\n${TABLE}\n</json-render>`).map((s) => s.kind)).toEqual([
      'render',
    ])
  })

  test('a tag inside a code fence is text about the format', () => {
    const text = '```html\n<json-render>{}</json-render>\n```'
    expect(splitReply(text)).toEqual([{ kind: 'markdown', text }])
  })

  test('a tag inside an inline code span is text about the format', () => {
    const text = 'Script 显示和 `<json-render>` 渲染都做完了。\n\nMore'
    expect(splitReply(text)).toEqual([{ kind: 'markdown', text }])
    expect(
      splitReply(`\`\`<json-render>\`\` then <json-render>${TABLE}</json-render>`).map(
        (s) => s.kind,
      ),
    ).toEqual(['markdown', 'render'])
  })

  test('a tag still open is pending; JSON that does not parse is shown as code', () => {
    expect(splitReply('Here:\n<json-render>{"root":').at(-1)).toEqual({
      kind: 'pending',
      raw: '{"root":',
    })
    expect(splitReply('<json-render>{oops}</json-render>')).toEqual([
      { kind: 'markdown', text: '```json\n{oops}\n```' },
    ])
  })
})

describe('parseRenderSpec', () => {
  test('fills in missing props and children, drops elements without a type', () => {
    expect(
      parseRenderSpec('{"root":"a","elements":{"a":{"type":"Text"},"b":{"props":{}}}}'),
    ).toEqual({ root: 'a', elements: { a: { type: 'Text', props: {}, children: [] } } })
  })

  test('a root that is not among the elements draws nothing', () => {
    expect(parseRenderSpec('{"root":"x","elements":{}}')).toBeNull()
    expect(parseRenderSpec('[]')).toBeNull()
  })
})

describe('childrenOf', () => {
  test('skips missing children and ones already drawn above', () => {
    const spec = parseRenderSpec(
      '{"root":"a","elements":{"a":{"type":"Box","children":["b","zz","a"]},"b":{"type":"Text"}}}',
    )!
    expect(childrenOf(spec, 'a', new Set(['a']))).toEqual(['b'])
  })
})

describe('toneOf', () => {
  test('colour and status names map to what they mean', () => {
    expect(toneOf('green')).toBe('success')
    expect(toneOf('Warning')).toBe('warning')
    expect(toneOf('red')).toBe('error')
    expect(toneOf('magenta')).toBe('default')
  })
})
