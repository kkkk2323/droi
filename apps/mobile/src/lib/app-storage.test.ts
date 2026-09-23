import { describe, expect, test } from 'vitest'
import { hydrateStorage, type AsyncBackend } from './app-storage'

function fakeBackend(
  initial: Record<string, string>,
): AsyncBackend & { stored: Map<string, string> } {
  const stored = new Map(Object.entries(initial))
  return {
    stored,
    getAllKeys: async () => [...stored.keys()],
    multiGet: async (keys) => keys.map((key) => [key, stored.get(key) ?? null] as const),
    setItem: async (key, value) => void stored.set(key, value),
    removeItem: async (key) => void stored.delete(key),
  }
}

describe('hydrateStorage', () => {
  test('serves the stored Droi keys synchronously after one read', async () => {
    const backend = fakeBackend({ 'droi.textSize': 'large', other: 'x' })
    const storage = await hydrateStorage(backend)
    expect(storage.getItem('droi.textSize')).toBe('large')
    expect(storage.getItem('other')).toBeNull()
  })

  test('writes land in memory at once and in app storage after', async () => {
    const backend = fakeBackend({ 'droi.draft.a': 'hello' })
    const storage = await hydrateStorage(backend)
    storage.setItem('droi.theme', 'dark')
    storage.removeItem('droi.draft.a')
    expect(storage.getItem('droi.theme')).toBe('dark')
    expect(storage.getItem('droi.draft.a')).toBeNull()
    await Promise.resolve()
    expect(backend.stored.get('droi.theme')).toBe('dark')
    expect(backend.stored.has('droi.draft.a')).toBe(false)
  })
})
