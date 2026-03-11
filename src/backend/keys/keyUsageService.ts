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

const MAX_USED_RATIO_BEFORE_SPILLOVER = 0.98

export function selectActiveKey(
  keys: ApiKeyEntry[],
  usages: Map<string, ApiKeyUsage>,
): { key: string; index: number } | null {
  if (keys.length === 0) return null

  type Candidate = {
    key: string
    index: number
    expiresTs: number
    expires: string
    usedRatio: number
    remaining: number
    total: number
  }

  const candidates: Candidate[] = []
  for (let i = 0; i < keys.length; i++) {
    const usage = usages.get(keys[i].key)
    if (!usage || usage.error) continue
    if (usage.total == null || usage.used == null) continue
    const remaining = usage.total - usage.used
    if (remaining <= 0) continue
    const usedRatio = usage.total > 0 ? usage.used / usage.total : 1
    candidates.push({
      key: keys[i].key,
      index: i,
      expiresTs: usage.expiresTs ?? Number.POSITIVE_INFINITY,
      expires: usage.expires || '9999-12-31',
      usedRatio,
      remaining,
      total: usage.total,
    })
  }

  if (candidates.length === 0) return null

  candidates.sort((a, b) => {
    if (a.expiresTs !== b.expiresTs) return a.expiresTs - b.expiresTs
    if (a.usedRatio !== b.usedRatio) return a.usedRatio - b.usedRatio
    return b.remaining - a.remaining
  })

  const eligible = candidates.find(
    (candidate) => candidate.usedRatio < MAX_USED_RATIO_BEFORE_SPILLOVER,
  )
  if (eligible) return { key: eligible.key, index: eligible.index }

  // All remaining keys are near exhaustion: pick the one with the most headroom.
  const fallback = candidates.reduce((a, b) => (a.remaining >= b.remaining ? a : b))
  return { key: fallback.key, index: fallback.index }
}
