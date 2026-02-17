import { createHash } from 'crypto'

const MAX_TEXT_PREVIEW = 200

function safeStringify(value: unknown): string {
  try {
    return JSON.stringify(value)
  } catch {
    return '"[unserializable]"'
  }
}

export function sha256Hex(text: string): string {
  return createHash('sha256').update(text, 'utf8').digest('hex')
}

export function makeTextPreview(text: string, maxLen = MAX_TEXT_PREVIEW): { head: string; tail: string } {
  const s = String(text || '')
  if (s.length <= maxLen) return { head: s, tail: s }
  return { head: s.slice(0, maxLen), tail: s.slice(-maxLen) }
}

export type PromptSig = {
  promptLen: number
  promptSha256: string
  promptHead: string
  promptTail: string
}

export function promptSig(text: string): PromptSig {
  const s = String(text || '')
  const { head, tail } = makeTextPreview(s)
  return {
    promptLen: s.length,
    promptSha256: sha256Hex(s),
    promptHead: head,
    promptTail: tail,
  }
}

function maskQueryParam(url: string, key: string): string {
  // Keep URL structure but redact known sensitive params.
  try {
    const u = new URL(url)
    if (u.searchParams.has(key)) u.searchParams.set(key, '[REDACTED]')
    return u.toString()
  } catch {
    // Not a valid URL; do a cheap best-effort mask.
    const re = new RegExp(`([?&])${key}=([^&#\\s]+)`, 'gi')
    return url.replace(re, `$1${key}=[REDACTED]`)
  }
}

export function redactText(raw: string): string {
  let s = String(raw || '')

  // Common secrets in this project.
  s = maskQueryParam(s, 'authKey')
  s = maskQueryParam(s, 'apiKey')

  // Env-style secrets.
  s = s.replace(/FACTORY_API_KEY\\s*[:=]\\s*[^\\s\"']+/gi, 'FACTORY_API_KEY=[REDACTED]')
  s = s.replace(/DROID_REMOTE_ACCESS_KEY\\s*[:=]\\s*[^\\s\"']+/gi, 'DROID_REMOTE_ACCESS_KEY=[REDACTED]')

  // Generic token-ish patterns.
  s = s.replace(/\\b(fk-[A-Za-z0-9_-]{8,})\\b/g, '[REDACTED_KEY]')
  s = s.replace(/\\b(sk-[A-Za-z0-9_-]{8,})\\b/g, '[REDACTED_KEY]')

  return s
}

export function redactJson(value: unknown, maxStringLen = 16_384): unknown {
  if (value === null || value === undefined) return value
  if (typeof value === 'string') {
    const red = redactText(value)
    return red.length > maxStringLen ? `${red.slice(0, maxStringLen)}...[truncated]` : red
  }
  if (typeof value === 'number' || typeof value === 'boolean') return value
  if (Array.isArray(value)) return value.map((v) => redactJson(v, maxStringLen))
  if (typeof value === 'object') {
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      const key = String(k || '')
      if (/apiKey/i.test(key) || /authKey/i.test(key) || /remoteAccessKey/i.test(key) || /factory_api_key/i.test(key)) {
        out[key] = '[REDACTED]'
        continue
      }
      out[key] = redactJson(v, maxStringLen)
    }
    return out
  }
  return safeStringify(value)
}

