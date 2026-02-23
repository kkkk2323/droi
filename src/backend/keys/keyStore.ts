import type { ApiKeyEntry, ApiKeyUsage, PersistedAppStateV2 } from '../../shared/protocol'
import type { AppStateStore } from '../storage/appStateStore.ts'
import { fetchAllKeyUsages, selectActiveKey } from './keyUsageService.ts'

const CACHE_TTL_MS = 30_000

export interface KeyStoreAPI {
  getKeys: () => Promise<ApiKeyEntry[]>
  addKeys: (keys: string[]) => Promise<{ added: number; duplicates: number }>
  removeKey: (index: number) => Promise<void>
  updateNote: (index: number, note: string) => Promise<void>
  getUsages: () => Promise<Map<string, ApiKeyUsage>>
  refreshUsages: () => Promise<Map<string, ApiKeyUsage>>
  getActiveKey: () => Promise<string | null>
}

export function createKeyStore(appStateStore: AppStateStore): KeyStoreAPI {
  let usageCache: Map<string, ApiKeyUsage> | null = null
  let usageCacheAt = 0
  let lastUsedIndex: number | undefined

  const getKeys = async (): Promise<ApiKeyEntry[]> => {
    const state = (await appStateStore.load()) as PersistedAppStateV2
    return state.apiKeys || []
  }

  const addKeys = async (rawKeys: string[]): Promise<{ added: number; duplicates: number }> => {
    const state = (await appStateStore.load()) as PersistedAppStateV2
    const existing = state.apiKeys || []
    const existingSet = new Set(existing.map((e) => e.key))
    let added = 0
    let duplicates = 0
    const now = Date.now()
    const next = [...existing]
    for (const k of rawKeys) {
      const trimmed = k.trim()
      if (!trimmed) continue
      if (existingSet.has(trimmed)) {
        duplicates++
        continue
      }
      existingSet.add(trimmed)
      next.push({ key: trimmed, addedAt: now })
      added++
    }
    const activeKey = next[0]?.key
    await appStateStore.update({ apiKeys: next, apiKey: activeKey })
    usageCache = null
    return { added, duplicates }
  }

  const removeKey = async (index: number): Promise<void> => {
    const state = (await appStateStore.load()) as PersistedAppStateV2
    const existing = state.apiKeys || []
    if (index < 0 || index >= existing.length) return
    const next = existing.filter((_, i) => i !== index)
    const activeKey = next[0]?.key
    await appStateStore.update({ apiKeys: next, apiKey: activeKey })
    usageCache = null
  }

  const updateNote = async (index: number, note: string): Promise<void> => {
    const state = (await appStateStore.load()) as PersistedAppStateV2
    const existing = state.apiKeys || []
    if (index < 0 || index >= existing.length) return
    const next = [...existing]
    next[index] = { ...next[index], note: note.trim() || undefined }
    await appStateStore.update({ apiKeys: next })
  }

  const getUsages = async (): Promise<Map<string, ApiKeyUsage>> => {
    if (usageCache && Date.now() - usageCacheAt < CACHE_TTL_MS) return usageCache
    return refreshUsages()
  }

  const refreshUsages = async (): Promise<Map<string, ApiKeyUsage>> => {
    const keys = await getKeys()
    if (keys.length === 0) {
      usageCache = new Map()
      usageCacheAt = Date.now()
      return usageCache
    }
    usageCache = await fetchAllKeyUsages(keys)
    usageCacheAt = Date.now()
    return usageCache
  }

  const getActiveKey = async (): Promise<string | null> => {
    const keys = await getKeys()
    if (keys.length === 0) return null
    if (keys.length === 1) return keys[0].key
    const usages = await getUsages()
    const result = selectActiveKey(keys, usages, lastUsedIndex)
    if (result) {
      lastUsedIndex = result.index
      if (((await appStateStore.load()) as PersistedAppStateV2).apiKey !== result.key) {
        await appStateStore.update({ apiKey: result.key })
      }
      return result.key
    }
    return keys[0].key
  }

  return { getKeys, addKeys, removeKey, updateNote, getUsages, refreshUsages, getActiveKey }
}
