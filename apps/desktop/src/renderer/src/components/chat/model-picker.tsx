import { useId, useRef, useState, type KeyboardEvent } from 'react'
import { Popover } from '@base-ui/react/popover'
import { ChevronDown, Search, Star } from 'lucide-react'
import { BrandIcon } from './brand-icon'
import { favoriteModels, usePreference } from '@droi/daemon-layer/local-preference'
import { BRAND_LABELS, brandOf } from '@droi/daemon-layer/model-brand'
import {
  EFFORT_LABELS,
  brandsOf,
  formatMultiplier,
  visibleModels,
  type PickerFilter,
} from '@droi/daemon-layer/model-choices'
import { cn } from '@/lib/utils'
import { TOOL_MODES, TOOL_MODE_LABELS, type ToolMode } from '@droi/daemon-layer/tool-mode'
import type { ModelChoice } from '@droi/daemon-layer/use-session-settings'

/** A choice that is not a model, such as "Same as main"; it stays at the top of the list. */
export interface PickerExtra {
  value: string
  label: string
}

/**
 * The reasoning effort of the chosen model, set in the picker's footer. It
 * belongs with the model: each model offers its own levels.
 */
export interface PickerEffort {
  value: string | null
  /** The chosen model's levels; the footer is left out when there are none. */
  options: readonly string[]
  onChange: (effort: string) => void
}

/** Direct, Script or both, set under the reasoning effort; only a new Session can choose. */
export interface PickerToolMode {
  /** Null until known (droid decides and the Daemon has not said). */
  value: ToolMode | null
  onChange: (mode: ToolMode) => void
}

const NO_EXTRAS: PickerExtra[] = []
const TOOL_MODE_OPTIONS = TOOL_MODES.map((mode) => ({ value: mode, label: TOOL_MODE_LABELS[mode] }))

export function ModelPicker({
  models,
  value,
  onChange,
  label = 'Model',
  extras = NO_EXTRAS,
  placeholder,
  disabled = false,
  field = false,
  effort,
  toolMode,
  className,
}: {
  models: ModelChoice[]
  value: string | null
  onChange: (modelId: string) => void
  /** The trigger's accessible name. */
  label?: string
  extras?: PickerExtra[]
  /** Trigger text while the value names nothing; defaults to `label`. */
  placeholder?: string
  disabled?: boolean
  /** Framed field that opens downwards, for settings rows, instead of the composer's text button. */
  field?: boolean
  effort?: PickerEffort
  toolMode?: PickerToolMode
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const [filter, setFilter] = useState<PickerFilter>('all')
  const [query, setQuery] = useState('')
  const [highlight, setHighlight] = useState(0)
  const [favorites, setFavorites] = usePreference(favoriteModels)
  const searchRef = useRef<HTMLInputElement>(null)
  // Several pickers can share a page, so their element ids must not collide.
  const baseId = useId()
  const listId = `${baseId}-list`
  const optionId = (id: string) => `${baseId}-option-${id.replace(/[^a-zA-Z0-9_-]/g, '_')}`

  const extra = extras.find((e) => e.value === value) ?? null
  const current = extra ? null : (models.find((m) => m.id === value) ?? null)
  const currentBrand = current ? brandOf(current.id, current.provider) : null
  const brands = brandsOf(models)
  const rows = visibleModels(models, favorites, filter, query)
  const choices = [
    ...extras.map((e) => ({ id: e.value, disabled: false })),
    ...rows.map((row) => ({ id: row.id, disabled: row.disabled })),
  ]
  const searching = query.trim().length > 0
  const activeIndex = Math.min(highlight, Math.max(choices.length - 1, 0))
  // The Daemon may hold a level the model no longer lists; still show it.
  const levels =
    effort && effort.value && !effort.options.includes(effort.value)
      ? [effort.value, ...effort.options]
      : (effort?.options ?? [])
  const effortLabel = effort?.value ? (EFFORT_LABELS[effort.value] ?? effort.value) : null

  const pick = (id: string) => {
    if (id !== value) onChange(id)
    setOpen(false)
  }
  const toggleFavorite = (id: string) =>
    setFavorites(favorites.includes(id) ? favorites.filter((f) => f !== id) : [...favorites, id])
  const choose = (next: PickerFilter) => {
    // Clicking the active filter again is the way back to the full list.
    setFilter(filter === next ? 'all' : next)
    setHighlight(0)
    searchRef.current?.focus()
  }

  const onSearchKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setHighlight(Math.min(activeIndex + 1, choices.length - 1))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setHighlight(Math.max(activeIndex - 1, 0))
    } else if (event.key === 'Enter') {
      const choice = choices[activeIndex]
      if (choice && !choice.disabled) {
        event.preventDefault()
        pick(choice.id)
      }
    }
  }

  return (
    <Popover.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) {
          setQuery('')
          setFilter('all')
          setHighlight(0)
        }
      }}
    >
      <Popover.Trigger
        aria-label={label}
        disabled={disabled || models.length + extras.length === 0}
        className={cn(
          'inline-flex select-none items-center gap-1.5 outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/50 data-[disabled]:opacity-50',
          field
            ? 'h-8 min-w-32 max-w-full rounded-lg border bg-background px-2.5 text-sm hover:bg-muted/60 data-[popup-open]:bg-muted/60'
            : 'h-7 max-w-64 rounded-md px-2 text-[13px] text-foreground/75 hover:bg-muted hover:text-foreground data-[popup-open]:bg-muted data-[popup-open]:text-foreground',
          className,
        )}
      >
        {currentBrand ? <BrandIcon brand={currentBrand} className="size-3.5" /> : null}
        <span className={cn('truncate', field && 'flex-1 text-left')}>
          {extra?.label ?? current?.label ?? value ?? placeholder ?? label}
        </span>
        {effortLabel && levels.length > 0 ? (
          <span className="shrink-0 text-muted-foreground">{effortLabel}</span>
        ) : null}
        {toolMode?.value && toolMode.value !== 'direct_only' ? (
          <span className="shrink-0 text-muted-foreground">{TOOL_MODE_LABELS[toolMode.value]}</span>
        ) : null}
        <ChevronDown aria-hidden className="size-3 shrink-0 opacity-60" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner
          side={field ? 'bottom' : 'top'}
          align={field ? 'end' : 'start'}
          sideOffset={field ? 4 : 6}
          className="z-50 outline-none"
        >
          <Popover.Popup
            initialFocus={searchRef}
            aria-label="Choose a model"
            className="flex h-[min(22rem,var(--available-height))] w-[min(30rem,var(--available-width))] overflow-hidden rounded-xl border bg-popover text-popover-foreground shadow-xl outline-none transition-[opacity,transform] duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-none"
          >
            <Popover.Title className="sr-only">Choose a model</Popover.Title>
            <div
              role="toolbar"
              aria-label="Filter models"
              aria-orientation="vertical"
              className="flex w-14 shrink-0 flex-col items-center gap-2 overflow-x-hidden overflow-y-auto border-r bg-sidebar px-2 py-3 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            >
              <RailButton
                label="Favorites"
                pressed={filter === 'favorites' && !searching}
                onClick={() => choose('favorites')}
              >
                <Star
                  aria-hidden
                  className={cn('size-3.5', filter === 'favorites' && 'fill-current')}
                />
              </RailButton>
              <div aria-hidden className="my-1 h-px w-5 shrink-0 bg-border" />
              {brands.map((brand) => (
                <RailButton
                  key={brand}
                  label={BRAND_LABELS[brand]}
                  pressed={filter === brand && !searching}
                  onClick={() => choose(brand)}
                >
                  <BrandIcon brand={brand} className="size-3.5" />
                </RailButton>
              ))}
            </div>

            <div className="flex min-w-0 flex-1 flex-col">
              <div className="flex items-center gap-2 border-b px-3">
                <Search aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                <input
                  ref={searchRef}
                  type="search"
                  aria-label="Search models"
                  aria-controls={listId}
                  aria-activedescendant={
                    choices[activeIndex] ? optionId(choices[activeIndex].id) : undefined
                  }
                  placeholder="Search models…"
                  value={query}
                  onChange={(event) => {
                    setQuery(event.target.value)
                    setHighlight(0)
                  }}
                  onKeyDown={onSearchKeyDown}
                  className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground [&::-webkit-search-cancel-button]:appearance-none"
                />
              </div>
              <ul
                id={listId}
                role="listbox"
                aria-label="Models"
                className="flex-1 overflow-y-auto p-1.5"
              >
                {extras.map((choice, index) => (
                  <li
                    key={choice.value}
                    id={optionId(choice.value)}
                    role="option"
                    aria-selected={choice.value === value}
                    data-value={choice.value}
                    data-highlighted={index === activeIndex || undefined}
                    onMouseMove={() => setHighlight(index)}
                    onClick={() => pick(choice.value)}
                    className={cn(
                      'flex h-9 items-center rounded-lg px-2.5 text-[13px] font-medium outline-none',
                      'data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground',
                      choice.value === value && 'bg-muted',
                    )}
                  >
                    <span className="truncate">{choice.label}</span>
                  </li>
                ))}
                {extras.length > 0 ? (
                  <li role="presentation" aria-hidden className="mx-1 my-1 h-px bg-border" />
                ) : null}
                {rows.length === 0 ? (
                  <li
                    className={cn(
                      'flex items-center justify-center px-4 text-center text-xs text-muted-foreground',
                      extras.length > 0 ? 'py-6' : 'h-full',
                    )}
                  >
                    {searching
                      ? 'No models match.'
                      : filter === 'favorites'
                        ? 'Star a model to keep it here.'
                        : 'No models.'}
                  </li>
                ) : null}
                {rows.map((row, rowIndex) => {
                  const index = extras.length + rowIndex
                  const selected = row.id === value
                  const starred = favorites.includes(row.id)
                  return (
                    <li
                      key={row.id}
                      id={optionId(row.id)}
                      role="option"
                      aria-selected={selected}
                      aria-disabled={row.disabled || undefined}
                      data-value={row.id}
                      data-highlighted={index === activeIndex || undefined}
                      onMouseMove={() => setHighlight(index)}
                      onClick={() => {
                        if (!row.disabled) pick(row.id)
                      }}
                      className={cn(
                        'group/row flex items-center gap-3 rounded-lg px-2.5 py-2 outline-none',
                        'data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground',
                        selected && 'bg-muted',
                        row.disabled && 'opacity-50',
                      )}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[13px] font-medium">{row.label}</div>
                        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                          <BrandIcon brand={row.brand} className="size-3" />
                          <span className="truncate">
                            {row.provider === null ? 'Router' : BRAND_LABELS[row.brand]}
                          </span>
                        </div>
                      </div>
                      {row.multiplier !== null ? (
                        <span
                          title="Usage multiplier"
                          className="shrink-0 font-mono text-[11px] text-muted-foreground tabular-nums"
                        >
                          {formatMultiplier(row.multiplier)}
                        </span>
                      ) : null}
                      <button
                        type="button"
                        aria-label={starred ? `Unstar ${row.label}` : `Star ${row.label}`}
                        aria-pressed={starred}
                        onClick={(event) => {
                          event.stopPropagation()
                          toggleFavorite(row.id)
                        }}
                        className={cn(
                          'flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground outline-none transition-colors hover:bg-background/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50',
                          starred
                            ? 'text-attention hover:text-attention'
                            : 'opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 group-data-[highlighted]/row:opacity-100 [@media(hover:none)]:opacity-100',
                        )}
                      >
                        <Star aria-hidden className={cn('size-4', starred && 'fill-current')} />
                      </button>
                    </li>
                  )
                })}
              </ul>
              {(effort && levels.length > 0) || toolMode ? (
                <div className="flex flex-col gap-1.5 border-t px-3 py-2">
                  {effort && levels.length > 0 ? (
                    <Segmented
                      title="Reasoning"
                      name="Reasoning effort"
                      options={levels.map((level) => ({
                        value: level,
                        label: EFFORT_LABELS[level] ?? level,
                      }))}
                      value={effort.value}
                      onChange={effort.onChange}
                    />
                  ) : null}
                  {toolMode ? (
                    <Segmented
                      title="Tools"
                      name="Tool calls"
                      options={TOOL_MODE_OPTIONS}
                      value={toolMode.value}
                      onChange={(mode) => toolMode.onChange(mode as ToolMode)}
                    />
                  ) : null}
                </div>
              ) : null}
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

/**
 * Every choice named at once, one click each. Changing it leaves the picker
 * open; picking a model is what closes it.
 */
function Segmented({
  title,
  name,
  options,
  value,
  onChange,
}: {
  /** The short visible label. */
  title: string
  /** The radio group's accessible name. */
  name: string
  options: ReadonlyArray<{ value: string; label: string }>
  value: string | null
  onChange: (value: string) => void
}) {
  const buttons = useRef<Array<HTMLButtonElement | null>>([])
  const levels = options.map((option) => option.value)
  const current = Math.max(levels.indexOf(value ?? ''), 0)
  const move = (index: number) => {
    const next = levels[index]
    if (next === undefined) return
    buttons.current[index]?.focus()
    if (next !== value) onChange(next)
  }
  return (
    <div className="flex items-center gap-3">
      <span aria-hidden className="w-16 shrink-0 text-xs text-muted-foreground">
        {title}
      </span>
      <div
        role="radiogroup"
        aria-label={name}
        className="flex min-w-0 flex-1 gap-0.5 rounded-lg bg-muted p-0.5"
        onKeyDown={(event) => {
          if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
            event.preventDefault()
            move(Math.min(current + 1, levels.length - 1))
          } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
            event.preventDefault()
            move(Math.max(current - 1, 0))
          }
        }}
      >
        {options.map(({ value: level, label }, index) => {
          const checked = level === value
          return (
            <button
              key={level}
              ref={(element) => {
                buttons.current[index] = element
              }}
              type="button"
              role="radio"
              aria-checked={checked}
              tabIndex={index === current ? 0 : -1}
              onClick={() => {
                if (!checked) onChange(level)
              }}
              className={cn(
                'h-6 min-w-0 flex-1 truncate rounded-md px-1.5 text-[12px] text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50',
                checked && 'bg-background font-medium text-foreground shadow-sm',
              )}
            >
              {label}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function RailButton({
  label,
  pressed,
  onClick,
  children,
}: {
  label: string
  pressed: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-pressed={pressed}
      title={label}
      onClick={onClick}
      className={cn(
        'flex size-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground outline-none transition-colors hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50',
        pressed && 'bg-sidebar-accent text-foreground',
      )}
    >
      {children}
    </button>
  )
}
