import type { ComponentProps } from 'react'
import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * A turning Loader2. The span turns, not the icon: Chromium does not hand an
 * SVG element's animation to the compositor, so a spinning SVG repaints on the
 * main thread every frame for as long as it shows.
 */
export function Spinner({ className, ...props }: ComponentProps<'span'>) {
  return (
    <span {...props} className={cn('inline-flex size-4 shrink-0 animate-spin', className)}>
      <Loader2 aria-hidden className="size-full" />
    </span>
  )
}
