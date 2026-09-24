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
    <span
      {...props}
      ref={inPhase}
      className={cn('inline-flex size-4 shrink-0 animate-spin', className)}
    >
      <Loader2 aria-hidden className="size-full" />
    </span>
  )
}

/**
 * A CSS animation starts when its element mounts, so spinners that appear at
 * different moments (a column of tool rows) point different ways. Starting
 * every one at the document timeline's zero keeps them turning as one.
 */
function inPhase(el: HTMLSpanElement | null) {
  for (const animation of el?.getAnimations?.() ?? []) animation.startTime = 0
}
