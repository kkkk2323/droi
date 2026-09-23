import { Streamdown } from 'streamdown'
import { cn } from '@/lib/utils'

/** Assistant prose. Streamdown tolerates the unterminated markdown of a live stream. */
export function Markdown({ text, className }: { text: string; className?: string }) {
  return (
    <Streamdown className={cn('prose-droi max-w-none text-[15px] leading-7', className)}>
      {text}
    </Streamdown>
  )
}
