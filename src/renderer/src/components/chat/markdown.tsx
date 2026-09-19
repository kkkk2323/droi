import { Streamdown } from 'streamdown'
import { cn } from '@/lib/utils'

/** Assistant prose. Streamdown tolerates the unterminated markdown of a live stream. */
export function Markdown({ text, className }: { text: string; className?: string }) {
  return (
    <Streamdown
      className={cn(
        'max-w-none text-sm leading-relaxed [&_code]:font-mono [&_code]:text-[0.85em] [&_p]:my-2 [&_ul]:my-2 [&_ol]:my-2 [&_li]:my-0.5 [&_h1]:text-base [&_h2]:text-base [&_h3]:text-sm [&_a]:underline [&_pre]:text-xs',
        className,
      )}
    >
      {text}
    </Streamdown>
  )
}
