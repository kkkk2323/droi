/**
 * Keys for a list drawn in order, from each item's own name and numbered
 * when a name repeats, so an item keeps its key as items are added after it.
 */
export function withKeys<T>(
  items: readonly T[],
  name: (item: T) => string,
): Array<{ key: string; item: T }> {
  const seen = new Map<string, number>()
  return items.map((item) => {
    const base = name(item)
    const count = seen.get(base) ?? 0
    seen.set(base, count + 1)
    return { key: count === 0 ? base : `${base}#${count}`, item }
  })
}
