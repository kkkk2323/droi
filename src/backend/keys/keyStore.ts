import type { ApiKeyEntry, ApiKeyUsage, PersistedAppStateV2 } from '../../shared/protocol'
import type { AppStateStore } from '../storage/appStateStore.ts'
import {
  fetchAllKeyUsages,
  isUsagePastSpilloverThreshold,
  selectActiveKey,
} from './keyUsageService.ts'

const CACHE_TTL_MS = 30_000
const INVALID_KEY_USAGE_ERRORS = new Set(['http_401', 'http_403'])

type SessionKeyBindings = Record<string, string>

export interface KeyStoreAPI {
  getKeys: () => Promise<ApiKeyEntry[]>
  addKeys: (keys: string[]) => Promise<{ added: number; duplicates: number }>
  removeKey: (index: number) => Promise<void>
  updateNote: (index: number, note: string) => Promise<void>
  getUsages: () => Promise<Map<string, ApiKeyUsage>>
  refreshUsages: () => Promise<Map<string, ApiKeyUsage>>
  invalidateUsages: () => void
  getActiveKey: (sessionId?: string) => Promise<string | null>
  getBoundKey: (sessionId: string) => Promise<string | null>
  bindSessionKey: (sessionId: string, key: string) => Promise<void>
  moveSessionBinding: (oldSessionId: string, newSessionId: string) => Promise<void>
  deleteSessionBinding: (sessionId: string) => Promise<void>
  rebindSessionsUsingKey: (oldKey: string, newKey: string | null) => Promise<void>
  resolveKeyForRequest: (
    presentedKey?: string | null,
  ) => Promise<{ key: string | null; reboundFrom?: string }>
}

export function createKeyStore(appStateStore: AppStateStore): KeyStoreAPI {
  let usageCache: Map<string, ApiKeyUsage> | null = null
  let usageCacheAt = 0

  const loadState = async (): Promise<PersistedAppStateV2> =>
    (await appStateStore.load()) as PersistedAppStateV2

  const normalizeBindings = (bindings: SessionKeyBindings): SessionKeyBindings | undefined =>
    Object.keys(bindings).length ? bindings : undefined

  const pruneBindings = (bindings: SessionKeyBindings, keys: ApiKeyEntry[]): SessionKeyBindings => {
    const validKeys = new Set(keys.map((entry) => entry.key))
    const next: SessionKeyBindings = {}
    for (const [sessionId, key] of Object.entries(bindings)) {
      if (validKeys.has(key)) next[sessionId] = key
    }
    return next
  }

  const selectBestKey = (keys: ApiKeyEntry[], usages: Map<string, ApiKeyUsage>): string | null => {
    if (keys.length === 0) return null
    if (keys.length === 1) return keys[0].key
    return selectActiveKey(keys, usages)?.key || keys[0].key
  }

  const shouldRebindKey = (
    key: string,
    keys: ApiKeyEntry[],
    usages: Map<string, ApiKeyUsage>,
  ): boolean => {
    if (!keys.some((entry) => entry.key === key)) return true
    const usage = usages.get(key)
    if (usage?.error && INVALID_KEY_USAGE_ERRORS.has(usage.error)) return true
    if (usage && usage.total != null && usage.used != null && usage.total - usage.used <= 0)
      return true
    return isUsagePastSpilloverThreshold(usage)
  }

  const getKeys = async (): Promise<ApiKeyEntry[]> => {
    const state = await loadState()
    return state.apiKeys || []
  }

  const addKeys = async (rawKeys: string[]): Promise<{ added: number; duplicates: number }> => {
    const state = await loadState()
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
    await appStateStore.update({
      apiKeys: next,
      apiKey: activeKey,
      sessionKeyBindings: normalizeBindings(pruneBindings(state.sessionKeyBindings || {}, next)),
    })
    usageCache = null
    return { added, duplicates }
  }

  const removeKey = async (index: number): Promise<void> => {
    const state = await loadState()
    const existing = state.apiKeys || []
    if (index < 0 || index >= existing.length) return
    const next = existing.filter((_, i) => i !== index)
    const activeKey = next[0]?.key
    await appStateStore.update({
      apiKeys: next,
      apiKey: activeKey,
      sessionKeyBindings: normalizeBindings(pruneBindings(state.sessionKeyBindings || {}, next)),
    })
    usageCache = null
  }

  const updateNote = async (index: number, note: string): Promise<void> => {
    const state = await loadState()
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

  const invalidateUsages = (): void => {
    usageCache = null
    usageCacheAt = 0
  }

  const getBoundKey = async (sessionId: string): Promise<string | null> => {
    const sid = String(sessionId || '').trim()
    if (!sid) return null
    const state = await loadState()
    return state.sessionKeyBindings?.[sid] || null
  }

  const bindSessionKey = async (sessionId: string, key: string): Promise<void> => {
    const sid = String(sessionId || '').trim()
    const nextKey = String(key || '').trim()
    if (!sid || !nextKey) return
    const state = await loadState()
    const nextBindings = { ...(state.sessionKeyBindings || {}), [sid]: nextKey }
    await appStateStore.update({ sessionKeyBindings: nextBindings, apiKey: nextKey })
  }

  const moveSessionBinding = async (oldSessionId: string, newSessionId: string): Promise<void> => {
    const oldId = String(oldSessionId || '').trim()
    const newId = String(newSessionId || '').trim()
    if (!oldId || !newId) return
    const state = await loadState()
    const bindings = { ...(state.sessionKeyBindings || {}) }
    const boundKey = bindings[oldId]
    if (!boundKey) return
    delete bindings[oldId]
    bindings[newId] = boundKey
    await appStateStore.update({ sessionKeyBindings: normalizeBindings(bindings) })
  }

  const deleteSessionBinding = async (sessionId: string): Promise<void> => {
    const sid = String(sessionId || '').trim()
    if (!sid) return
    const state = await loadState()
    const bindings = { ...(state.sessionKeyBindings || {}) }
    if (!(sid in bindings)) return
    delete bindings[sid]
    await appStateStore.update({ sessionKeyBindings: normalizeBindings(bindings) })
  }

  const rebindSessionsUsingKey = async (oldKey: string, newKey: string | null): Promise<void> => {
    const prevKey = String(oldKey || '').trim()
    const nextKey = String(newKey || '').trim()
    if (!prevKey || prevKey === nextKey) return
    const state = await loadState()
    const bindings = state.sessionKeyBindings || {}
    let changed = false
    const nextBindings: SessionKeyBindings = {}
    for (const [sessionId, key] of Object.entries(bindings)) {
      if (key !== prevKey) {
        nextBindings[sessionId] = key
        continue
      }
      changed = true
      if (nextKey) nextBindings[sessionId] = nextKey
    }
    if (!changed) return
    await appStateStore.update({
      sessionKeyBindings: normalizeBindings(nextBindings),
      apiKey: state.apiKey === prevKey ? nextKey || undefined : state.apiKey,
    })
  }

  const getActiveKey = async (sessionId?: string): Promise<string | null> => {
    const keys = await getKeys()
    if (keys.length === 0) return null
    const usages = await getUsages()

    const sid = String(sessionId || '').trim()
    if (sid) {
      const state = await loadState()
      const bindings = pruneBindings(state.sessionKeyBindings || {}, keys)
      const boundKey = bindings[sid]
      if (boundKey && !shouldRebindKey(boundKey, keys, usages)) {
        if (state.apiKey !== boundKey) await appStateStore.update({ apiKey: boundKey })
        return boundKey
      }

      const nextKey = selectBestKey(keys, usages)
      if (!nextKey) return null
      const nextBindings = { ...bindings, [sid]: nextKey }
      await appStateStore.update({
        apiKey: nextKey,
        sessionKeyBindings: normalizeBindings(nextBindings),
      })
      return nextKey
    }

    const nextKey = selectBestKey(keys, usages)
    if (!nextKey) return null
    const state = await loadState()
    if (state.apiKey !== nextKey) {
      await appStateStore.update({
        apiKey: nextKey,
        sessionKeyBindings: normalizeBindings(pruneBindings(state.sessionKeyBindings || {}, keys)),
      })
    }
    return nextKey
  }

  const resolveKeyForRequest = async (
    presentedKey?: string | null,
  ): Promise<{ key: string | null; reboundFrom?: string }> => {
    const keys = await getKeys()
    if (keys.length === 0) return { key: null }

    const usages = await getUsages()
    const currentKey = String(presentedKey || '').trim()
    if (currentKey && !shouldRebindKey(currentKey, keys, usages)) {
      return { key: currentKey }
    }

    const nextKey = selectBestKey(keys, usages)
    if (!nextKey) return { key: null }
    if (currentKey) await rebindSessionsUsingKey(currentKey, nextKey)
    return {
      key: nextKey,
      reboundFrom: currentKey && currentKey !== nextKey ? currentKey : undefined,
    }
  }

  return {
    getKeys,
    addKeys,
    removeKey,
    updateNote,
    getUsages,
    refreshUsages,
    invalidateUsages,
    getActiveKey,
    getBoundKey,
    bindSessionKey,
    moveSessionBinding,
    deleteSessionBinding,
    rebindSessionsUsingKey,
    resolveKeyForRequest,
  }
}
