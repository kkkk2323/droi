// The model picker's rows and the settings' labels, for every Client.
import { BRAND_LABELS, BRAND_ORDER, brandOf, isMagpie, type Brand } from './model-brand'
import type { ModelChoice } from './use-session-settings'

/**
 * Which rows the picker lists: everything of the Daemon's own, the starred
 * ones, a Magpie gateway's, or one brand.
 */
export type PickerFilter = 'all' | 'favorites' | 'magpie' | Brand

export interface PickerRow extends ModelChoice {
  brand: Brand
}

/**
 * Rows in display order. A non-empty query searches every model regardless of
 * the filter; favorites keep the order they were starred in. Everything but the
 * Magpie tab leaves the gateway's models out: the Daemon's own list is one
 * crowd, Magpie's another, and a name both serve would read as two models of
 * one kind.
 */
export function visibleModels(
  models: ModelChoice[],
  favorites: string[],
  filter: PickerFilter,
  query: string,
): PickerRow[] {
  const rows = models.map((m) => ({ ...m, brand: brandOf(m.id, m.provider) }))
  const tokens = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (tokens.length > 0) {
    return rows.filter((row) => {
      const haystack = `${row.label} ${row.id} ${BRAND_LABELS[row.brand]}`.toLowerCase()
      return tokens.every((t) => haystack.includes(t))
    })
  }
  if (filter === 'all') return rows.filter((row) => !isMagpie(row.id))
  if (filter === 'favorites') {
    return rows
      .filter((row) => favorites.includes(row.id))
      .sort((a, b) => favorites.indexOf(a.id) - favorites.indexOf(b.id))
  }
  if (filter === 'magpie') return rows.filter((row) => isMagpie(row.id))
  return rows.filter((row) => !isMagpie(row.id) && row.brand === filter)
}

/** Whether the list holds a Magpie gateway's models, so the picker offers their tab. */
export function hasMagpie(models: ModelChoice[]): boolean {
  return models.some((m) => isMagpie(m.id))
}

/**
 * Brands present in the Daemon's own list, in rail order. Magpie's models have
 * their own tab rather than each brand's: their names repeat the Daemon's, and
 * a brand tab holding both is the crowd the tab is there to avoid.
 */
export function brandsOf(models: ModelChoice[]): Brand[] {
  const present = new Set(
    models.filter((m) => !isMagpie(m.id)).map((m) => brandOf(m.id, m.provider)),
  )
  return BRAND_ORDER.filter((b) => present.has(b))
}

/** `1.6` → `1.6×`, as Factory shows its usage multipliers. */
export function formatMultiplier(multiplier: number): string {
  return `${Number(multiplier.toFixed(2))}×`
}

export const EFFORT_LABELS: Record<string, string> = {
  none: 'None',
  dynamic: 'Dynamic',
  off: 'Off',
  minimal: 'Minimal',
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'Extra high',
  max: 'Max',
}

export const AUTONOMY_LABELS: Record<string, string> = {
  off: 'Ask for everything',
  low: 'Low autonomy',
  medium: 'Medium autonomy',
  high: 'High autonomy',
}
