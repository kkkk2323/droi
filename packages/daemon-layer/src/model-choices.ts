// The model picker's rows and the settings' labels, for every Client.
import { BRAND_LABELS, BRAND_ORDER, brandOf, type Brand } from './model-brand'
import type { ModelChoice } from './use-session-settings'

/** Which rows the picker lists: everything, the starred ones, or one brand. */
export type PickerFilter = 'all' | 'favorites' | Brand

export interface PickerRow extends ModelChoice {
  brand: Brand
}

/**
 * Rows in display order. A non-empty query searches every model regardless of
 * the filter; favorites keep the order they were starred in.
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
  if (filter === 'all') return rows
  if (filter === 'favorites') {
    return rows
      .filter((row) => favorites.includes(row.id))
      .sort((a, b) => favorites.indexOf(a.id) - favorites.indexOf(b.id))
  }
  return rows.filter((row) => row.brand === filter)
}

/** Brands present in the list, in rail order. */
export function brandsOf(models: ModelChoice[]): Brand[] {
  const present = new Set(models.map((m) => brandOf(m.id, m.provider)))
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
}

export const AUTONOMY_LABELS: Record<string, string> = {
  off: 'Ask for everything',
  low: 'Low autonomy',
  medium: 'Medium autonomy',
  high: 'High autonomy',
}
