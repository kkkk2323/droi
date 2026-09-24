import type { ReactNode } from 'react'
import { Switch as SwitchPrimitive } from '@base-ui/react/switch'
import { cn } from '@/lib/utils'

/** One card in a settings list: title and explanation left, control right. */
export function SettingRow({
  title,
  description,
  control,
  children,
  className,
}: {
  title: ReactNode
  description?: ReactNode
  /** Compact control shown on the title line (switch, select, button). */
  control?: ReactNode
  /** Full-width content under the description (forms, QR, ...). */
  children?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('rounded-xl bg-card px-4 py-3.5', className)}>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="min-w-48 flex-1">
          <h3 className="text-sm font-medium">{title}</h3>
          {description ? (
            <p className="mt-0.5 text-[13px] leading-5 text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {control ? <div className="min-w-0 max-w-full">{control}</div> : null}
      </div>
      {children ? <div className="mt-3">{children}</div> : null}
    </div>
  )
}

export function Switch(props: SwitchPrimitive.Root.Props) {
  return (
    <SwitchPrimitive.Root
      {...props}
      className={cn(
        'relative inline-flex h-[22px] w-9 shrink-0 cursor-pointer items-center rounded-full bg-input transition-colors duration-150 outline-none',
        'data-[checked]:bg-primary focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50',
        props.className,
      )}
    >
      <SwitchPrimitive.Thumb className="block size-[18px] translate-x-0.5 rounded-full bg-white shadow-sm transition-transform duration-150 data-[checked]:translate-x-[16px]" />
    </SwitchPrimitive.Root>
  )
}

export const settingInputClass =
  'h-9 min-w-0 flex-1 rounded-lg border bg-background px-3 text-sm outline-none transition-colors focus-visible:border-ring/60 focus-visible:ring-2 focus-visible:ring-ring/30'
