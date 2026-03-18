import { Streamdown } from 'streamdown'
import { cn } from '@/lib/utils'

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
