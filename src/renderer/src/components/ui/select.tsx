import type { ReactNode } from 'react'
import { Select as SelectPrimitive } from '@base-ui/react/select'
import { Check, ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

export interface SelectGroup {
  label: string
  options: SelectOption[]
}

/**
 * Base UI Select with the Client's styling. The trigger is a combobox button
 * named by `label`; the popup lists `options` flat or in labelled groups.
 */
export function Select({
  label,
  value,
  onChange,
  options,
  groups,
  icon,
  quiet = false,
  className,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  options?: SelectOption[]
  groups?: SelectGroup[]
  icon?: ReactNode
  /** Text-button look for toolbars instead of a framed field. */
  quiet?: boolean
  className?: string
}) {
  const flat = options ?? groups?.flatMap((g) => g.options) ?? []
  const known = flat.some((o) => o.value === value)
  // The Daemon may hold a value the list no longer offers; still show it.
  const items = known || !value ? flat : [{ value, label: value }, ...flat]
  const empty = items.length === 0

  return (
    <SelectPrimitive.Root
      items={items}
      value={value || null}
      onValueChange={(next) => {
        if (typeof next === 'string' && next !== value) onChange(next)
      }}
      disabled={empty}
    >
      <SelectPrimitive.Trigger
        aria-label={label}
        className={cn(
          'inline-flex items-center gap-1 text-xs outline-none transition-colors select-none',
          'focus-visible:ring-2 focus-visible:ring-ring/50 data-[disabled]:opacity-50',
          quiet
            ? 'h-7 max-w-52 rounded-md px-2 text-muted-foreground hover:bg-muted hover:text-foreground data-[popup-open]:bg-muted data-[popup-open]:text-foreground'
            : 'h-8 min-w-32 justify-between rounded-lg border bg-background px-2.5 text-sm hover:bg-muted/60',
          className,
        )}
      >
        {icon}
        <SelectPrimitive.Value className="truncate" placeholder={label} />
        <SelectPrimitive.Icon className="flex shrink-0">
          <ChevronDown aria-hidden className="size-3 opacity-60" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Positioner
          alignItemWithTrigger={false}
          sideOffset={4}
          className="z-50 outline-none"
        >
          <SelectPrimitive.Popup className="max-h-[min(24rem,var(--available-height))] min-w-[var(--anchor-width)] overflow-y-auto rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none">
            {!known && value ? <Item option={{ value, label: value }} /> : null}
            {groups
              ? groups.map((group) => (
                  <SelectPrimitive.Group key={group.label}>
                    <SelectPrimitive.GroupLabel className="px-2 pb-1 pt-1.5 text-[11px] font-medium text-muted-foreground">
                      {group.label}
                    </SelectPrimitive.GroupLabel>
                    {group.options.map((option) => (
                      <Item key={option.value} option={option} />
                    ))}
                  </SelectPrimitive.Group>
                ))
              : (options ?? []).map((option) => <Item key={option.value} option={option} />)}
          </SelectPrimitive.Popup>
        </SelectPrimitive.Positioner>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  )
}

function Item({ option }: { option: SelectOption }) {
  return (
    <SelectPrimitive.Item
      value={option.value}
      label={option.label}
      disabled={option.disabled}
      data-value={option.value}
      className="grid cursor-default grid-cols-[1rem_1fr] items-center gap-1.5 rounded-md py-1.5 pl-1.5 pr-3 text-[13px] outline-none data-[disabled]:opacity-50 data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
    >
      <SelectPrimitive.ItemIndicator className="col-start-1 flex justify-center">
        <Check aria-hidden className="size-3.5" />
      </SelectPrimitive.ItemIndicator>
      <SelectPrimitive.ItemText className="col-start-2 truncate">
        {option.label}
      </SelectPrimitive.ItemText>
    </SelectPrimitive.Item>
  )
}
