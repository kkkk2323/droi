import { createHash } from 'node:crypto'

/**
 * What a fingerprint starts with since it is a hash. One written before is the normalized JSON itself: a
 * conversation.import's held every entry it imported, so the same text was kept three times (the memo entry, the
 * import-key row, the request), about 3 GB for 2,650 imported Sessions. {@link sameFingerprint} still matches one.
 */
const PREFIX = 'sha256:'

/** A stable fingerprint of "the same request": key order does not matter, so a client may resend freely. */
export function fingerprintOf(value: unknown): string {
  return hashOf(JSON.stringify(normalize(value)))
}

/** The fingerprint a stored one stands for: itself, or the hash of an older build's JSON. */
export function hashedFingerprint(stored: string): string {
  return isHashed(stored) ? stored : hashOf(stored)
}

/** Whether a stored fingerprint, of this build or an older one, is `current`'s. */
export function sameFingerprint(stored: string | null | undefined, current: string): boolean {
  if (typeof stored !== 'string') return false
  return stored === current || (!isHashed(stored) && hashOf(stored) === current)
}

export function isHashed(stored: string): boolean {
  return stored.length === PREFIX.length + 64 && stored.startsWith(PREFIX)
}

function hashOf(text: string): string {
  return PREFIX + createHash('sha256').update(text).digest('hex')
}

function normalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalize)
  if (typeof value !== 'object' || value === null) return value
  const record = value as Record<string, unknown>
  // A null prototype and `defineProperty`: an own `__proto__` in imported data is a field like any other, and
  // assigning it would set the prototype instead — losing it from the fingerprint so two different payloads
  // look like the same request.
  const out = Object.create(null) as Record<string, unknown>
  for (const key of Object.keys(record).sort()) {
    if (record[key] === undefined) continue
    Object.defineProperty(out, key, {
      value: normalize(record[key]),
      enumerable: true,
      writable: true,
      configurable: true,
    })
  }
  return out
}
