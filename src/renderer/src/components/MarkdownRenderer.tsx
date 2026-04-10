import { useMemo } from 'react'
import { JsonRenderRenderer } from '@/lib/json-render'
import type { JsonRenderSpec } from '@/lib/json-render'
import { Streamdown } from 'streamdown'
import { cn } from '@/lib/utils'

const JSON_RENDER_OPEN = '<json-render>'
const JSON_RENDER_CLOSE = '</json-render>'

interface ParsedBlock {
  type: 'text' | 'json-render'
  content: string
  spec?: JsonRenderSpec
}

function parseSpec(raw: string): JsonRenderSpec | null {
  try {
    const parsed = JSON.parse(raw.trim())
    if (
      typeof parsed?.root !== 'string' ||
      !parsed.root ||
      typeof parsed?.elements !== 'object' ||
      !(parsed.root in parsed.elements)
    ) {
      return null
    }
    return parsed as JsonRenderSpec
  } catch {
    return null
  }
}

function extractJsonRenderBlocks(content: string): ParsedBlock[] {
  const blocks: ParsedBlock[] = []
  let cursor = 0

  while (cursor < content.length) {
    const openIdx = content.indexOf(JSON_RENDER_OPEN, cursor)
    if (openIdx === -1) {
      const text = content.slice(cursor)
      if (text.trim()) blocks.push({ type: 'text', content: text })
      break
    }

    if (openIdx > cursor) {
      const text = content.slice(cursor, openIdx)
      if (text.trim()) blocks.push({ type: 'text', content: text })
    }

    const closeIdx = content.indexOf(JSON_RENDER_CLOSE, openIdx + JSON_RENDER_OPEN.length)
    if (closeIdx === -1) {
      const text = content.slice(openIdx)
      if (text.trim()) blocks.push({ type: 'text', content: text })
      break
    }

    const jsonContent = content.slice(openIdx + JSON_RENDER_OPEN.length, closeIdx)
    const spec = parseSpec(jsonContent)

    if (spec) {
      blocks.push({ type: 'json-render', content: jsonContent, spec })
    } else {
      blocks.push({
        type: 'text',
        content: content.slice(openIdx, closeIdx + JSON_RENDER_CLOSE.length),
      })
    }

    cursor = closeIdx + JSON_RENDER_CLOSE.length
  }

  return blocks
}

function hasJsonRender(content: string): boolean {
  if (!content.includes(JSON_RENDER_OPEN)) return false
  return extractJsonRenderBlocks(content).some((b) => b.type === 'json-render')
}

interface MarkdownRendererProps {
  content: string
  isStreaming?: boolean
  className?: string
}

export function MarkdownRenderer({
  content,
  isStreaming = false,
  className,
}: MarkdownRendererProps) {
  if (!content.trim()) return null

  const containsJsonRender = useMemo(() => hasJsonRender(content), [content])

  if (!containsJsonRender) {
    return (
      <div className={cn('min-w-0 overflow-hidden break-words', className)}>
        <Streamdown
          animated={{
            animation: 'blurIn',
            duration: 250,
            easing: 'ease-out',
          }}
          caret="block"
          className="text-base"
          isAnimating={isStreaming}
          mode={isStreaming ? 'streaming' : 'static'}
        >
          {content}
        </Streamdown>
      </div>
    )
  }

  const blocks = useMemo(() => extractJsonRenderBlocks(content), [content])

  return (
    <div className={cn('min-w-0 overflow-hidden break-words', className)}>
      {blocks.map((block, i) => {
        const isLast = i === blocks.length - 1

        if (block.type === 'text') {
          return (
            <Streamdown
              key={`text-${i}`}
              animated={{
                animation: 'blurIn',
                duration: 250,
                easing: 'ease-out',
              }}
              caret={isLast && isStreaming ? 'block' : undefined}
              className="text-base"
              isAnimating={isStreaming && isLast}
              mode={isStreaming && isLast ? 'streaming' : 'static'}
            >
              {block.content}
            </Streamdown>
          )
        }

        if (block.spec) {
          return (
            <div key={`spec-${i}`} className="my-3 rounded-lg border border-border bg-sidebar p-3">
              <JsonRenderRenderer spec={block.spec} />
            </div>
          )
        }

        return null
      })}
    </div>
  )
}
