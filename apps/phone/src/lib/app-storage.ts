// The shared daemon layer reads preferences synchronously, and the phone's
// app storage (AsyncStorage) is asynchronous. The app reads every Droi key
// once before its first render and serves them from memory; writes go to
// memory at once and to app storage in the background.
import type { PreferenceStorage } from '@droi/daemon-layer/local-preference'

export interface AsyncBackend {
  getAllKeys(): Promise<readonly string[]>
  multiGet(keys: readonly string[]): Promise<ReadonlyArray<readonly [string, string | null]>>
  setItem(key: string, value: string): Promise<void>
  removeItem(key: string): Promise<void>
}

const PREFIX = 'droi.'

export async function hydrateStorage(backend: AsyncBackend): Promise<PreferenceStorage> {
  const values = new Map<string, string>()
  const keys = (await backend.getAllKeys()).filter((key) => key.startsWith(PREFIX))
  for (const [key, value] of await backend.multiGet(keys)) {
    if (value !== null) values.set(key, value)
  }
  const report = (cause: unknown) => console.warn('App storage write failed', cause)
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem(key, value) {
      values.set(key, value)
      backend.setItem(key, value).catch(report)
    },
    removeItem(key) {
      values.delete(key)
      backend.removeItem(key).catch(report)
    },
  }
}
