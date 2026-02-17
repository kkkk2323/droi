import * as React from 'react'
import { ScrollArea as BaseScrollArea } from '@base-ui/react/scroll-area'
import { cn } from '../../lib/utils'

function ScrollArea({
  className,
  children,
  viewportRef,
  ...props
}: React.ComponentProps<'div'> & {
  viewportRef?: React.Ref<HTMLDivElement>
}) {
  return (
    <BaseScrollArea.Root className={cn('relative overflow-hidden', className)} {...props}>
      <BaseScrollArea.Viewport ref={viewportRef} className="h-full w-full">
        {children}
      </BaseScrollArea.Viewport>
      <BaseScrollArea.Scrollbar
        orientation="vertical"
        className="flex w-2 touch-none p-px transition-opacity duration-150 data-[hovering]:opacity-100 opacity-0"
      >
        <BaseScrollArea.Thumb className="relative flex-1 rounded-full bg-border" />
      </BaseScrollArea.Scrollbar>
    </BaseScrollArea.Root>
  )
}

export { ScrollArea }
