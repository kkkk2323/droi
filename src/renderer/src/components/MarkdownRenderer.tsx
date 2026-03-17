import { Markdown } from '@lobehub/ui'
import { cn } from '@/lib/utils'

interface MarkdownRendererProps {
  content: string
  isStreaming?: boolean
  variant?: 'default' | 'chat'
  className?: string
}

export function MarkdownRenderer({
  content,
  isStreaming = false,
  variant = 'chat',
  className,
}: MarkdownRendererProps) {
  if (!content.trim()) return null

  return (
    <div className={cn('min-w-0 overflow-hidden break-words', className)}>
      <Markdown
        fontSize={16}
        enableImageGallery
        enableLatex
        enableMermaid
        animated={isStreaming}
        fullFeaturedCodeBlock
        variant={variant}
      >
        {content}
      </Markdown>
    </div>
  )
}
