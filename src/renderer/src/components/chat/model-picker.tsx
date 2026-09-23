import { useRef, useState, type KeyboardEvent } from 'react'
import { Popover } from '@base-ui/react/popover'
import { ChevronDown, Search, Star } from 'lucide-react'
import { BrandIcon } from './brand-icon'
import { favoriteModels, usePreference } from '@droi/daemon-layer/local-preference'
import { BRAND_LABELS, brandOf } from '@droi/daemon-layer/model-brand'
import { brandsOf, visibleModels, type PickerFilter } from '@droi/daemon-layer/model-choices'
import { cn } from '@/lib/utils'
import type { ModelChoice } from '@droi/daemon-layer/use-session-settings'

export function ModelPicker({
  models,
  value,
  onChange,
}: {
  models: ModelChoice[]
  value: string | null
  onChange: (modelId: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [filter, setFilter] = useState<PickerFilter>('all')
  const [query, setQuery] = useState('')
  const [highlight, setHighlight] = useState(0)
  const [favorites, setFavorites] = usePreference(favoriteModels)
  const searchRef = useRef<HTMLInputElement>(null)

  const current = models.find((m) => m.id === value) ?? null
  const currentBrand = current ? brandOf(current.id, current.provider) : null
  const brands = brandsOf(models)
  const rows = visibleModels(models, favorites, filter, query)
  const searching = query.trim().length > 0
  const activeIndex = Math.min(highlight, Math.max(rows.length - 1, 0))

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
      setHighlight(Math.min(activeIndex + 1, rows.length - 1))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setHighlight(Math.max(activeIndex - 1, 0))
    } else if (event.key === 'Enter') {
      const row = rows[activeIndex]
      if (row && !row.disabled) {
        event.preventDefault()
        pick(row.id)
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
        aria-label="Model"
        disabled={models.length === 0}
        className={cn(
          'inline-flex h-7 max-w-52 select-none items-center gap-1.5 rounded-md px-2 text-[13px] text-foreground/75 outline-none transition-colors',
          'hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 data-[popup-open]:bg-muted data-[popup-open]:text-foreground data-[disabled]:opacity-50',
        )}
      >
        {currentBrand ? <BrandIcon brand={currentBrand} className="size-3.5" /> : null}
        <span className="truncate">{current?.label ?? value ?? 'Model'}</span>
        <ChevronDown aria-hidden className="size-3 shrink-0 opacity-60" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner side="top" align="start" sideOffset={6} className="z-50 outline-none">
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
                  aria-controls="model-picker-list"
                  aria-activedescendant={
                    rows[activeIndex] ? optionId(rows[activeIndex].id) : undefined
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
                id="model-picker-list"
                role="listbox"
                aria-label="Models"
                className="flex-1 overflow-y-auto p-1.5"
              >
                {rows.length === 0 ? (
                  <li className="flex h-full items-center justify-center px-4 text-center text-xs text-muted-foreground">
                    {searching
                      ? 'No models match.'
                      : filter === 'favorites'
                        ? 'Star a model to keep it here.'
                        : 'No models.'}
                  </li>
                ) : null}
                {rows.map((row, index) => {
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
                            ? 'text-amber-500 hover:text-amber-500'
                            : 'opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 group-data-[highlighted]/row:opacity-100 [@media(hover:none)]:opacity-100',
                        )}
                      >
                        <Star aria-hidden className={cn('size-4', starred && 'fill-current')} />
                      </button>
                    </li>
                  )
                })}
              </ul>
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

function optionId(modelId: string): string {
  return `model-option-${modelId.replace(/[^a-zA-Z0-9_-]/g, '_')}`
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
