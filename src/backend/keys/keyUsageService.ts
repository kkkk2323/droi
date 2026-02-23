import type { ApiKeyEntry, ApiKeyUsage } from '../../shared/protocol'

const USAGE_API_URL = 'https://app.factory.ai/api/organization/members/chat-usage'
const MAX_CONCURRENCY = 6
const REQUEST_TIMEOUT_MS = 8000

export async function fetchKeyUsage(key: string): Promise<ApiKeyUsage> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)

  try {
    const res = await fetch(USAGE_API_URL, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${key}`,
        'User-Agent': 'Droi/1.0',
      },
      signal: controller.signal,
    })

    if (!res.ok) {
      return {
        used: null,
        total: null,
        expires: null,
        expiresTs: null,
        lastCheckedAt: Date.now(),
        error: `http_${res.status}`,
      }
    }

    const data = await res.json()
    return parseUsageResponse(data)
  } catch (err) {
    return {
      used: null,
      total: null,
      expires: null,
      expiresTs: null,
      lastCheckedAt: Date.now(),
      error: err instanceof Error ? err.message : 'fetch_error',
    }
  } finally {
    clearTimeout(timer)
  }
}

function parseUsageResponse(data: any): ApiKeyUsage {
  const u = data?.usage ?? {}
  const expRaw = u.endDate ?? u.expire_at ?? u.expires_at

  let section: { total: number; used: number } | null = null
  for (const sec of [u.standard, u.premium, u.total, u.main]) {
    if (sec == null) continue
    const total = sec.totalAllowance ?? sec.basicAllowance ?? sec.allowance ?? null
    if (total != null) {
      const used =
        (sec.orgTotalTokensUsed ?? sec.used ?? sec.tokensUsed ?? 0) + (sec.orgOverageUsed ?? 0)
      section = { total, used }
      break
    }
  }

  let expiresTs: number | null = null
  let expires: string | null = null
  if (expRaw != null) {
    const ms = Number(expRaw)
    if (!isNaN(ms) && ms > 0) {
      expiresTs = ms / 1000
      const dt = new Date(ms)
      expires = dt.toISOString().slice(0, 10)
    }
  }

  return {
    used: section?.used ?? null,
    total: section?.total ?? null,
    expires,
    expiresTs,
    lastCheckedAt: Date.now(),
  }
}

export async function fetchAllKeyUsages(keys: ApiKeyEntry[]): Promise<Map<string, ApiKeyUsage>> {
  const result = new Map<string, ApiKeyUsage>()
  const queue = [...keys]
  const running: Promise<void>[] = []

  const process = async (entry: ApiKeyEntry) => {
    const usage = await fetchKeyUsage(entry.key)
    result.set(entry.key, usage)
  }

  while (queue.length > 0 || running.length > 0) {
    while (running.length < MAX_CONCURRENCY && queue.length > 0) {
      const entry = queue.shift()!
      const p = process(entry).then(() => {
        running.splice(running.indexOf(p), 1)
      })
      running.push(p)
    }
    if (running.length > 0) await Promise.race(running)
  }

  return result
}

export function selectActiveKey(
  keys: ApiKeyEntry[],
  usages: Map<string, ApiKeyUsage>,
  lastUsedIndex?: number,
): { key: string; index: number } | null {
  if (keys.length === 0) return null

  const candidates: Array<{ key: string; index: number; expires: string; remaining: number }> = []
  for (let i = 0; i < keys.length; i++) {
    const usage = usages.get(keys[i].key)
    if (!usage || usage.error) continue
    if (usage.total == null || usage.used == null) continue
    const remaining = usage.total - usage.used
    if (remaining <= 0) continue
    candidates.push({
      key: keys[i].key,
      index: i,
      expires: usage.expires || '9999-12-31',
      remaining,
    })
  }

  if (candidates.length === 0) return null

  candidates.sort((a, b) => {
    const dayA = a.expires.slice(0, 10)
    const dayB = b.expires.slice(0, 10)
    const cmp = dayA.localeCompare(dayB)
    if (cmp !== 0) return cmp
    return b.remaining - a.remaining
  })

  const earliestDay = candidates[0].expires.slice(0, 10)
  const earlyGroup = candidates.filter((c) => c.expires.slice(0, 10) === earliestDay)

  if (lastUsedIndex != null) {
    const nextInGroup = earlyGroup.find((c) => c.index > lastUsedIndex)
    if (nextInGroup) return { key: nextInGroup.key, index: nextInGroup.index }
  }

  return { key: earlyGroup[0].key, index: earlyGroup[0].index }
}
