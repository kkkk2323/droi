// What a Client needs to know to pair with a Gateway: the Pairing Token and,
// when it is not the page's own origin, the Gateway's address. The Desktop
// Shell's pairing link carries both in its fragment (`#pair=...&gateway=...`).

export interface Pairing {
  token: string
  gateway: string | null
}

export function parsePairingFragment(hash: string): Pairing | null {
  const fragment = new URLSearchParams(hash.replace(/^#/, ''))
  const token = fragment.get('pair')
  return token ? { token, gateway: fragment.get('gateway') } : null
}

/**
 * A pairing link pasted by hand: the whole link, just its fragment, or the
 * bare token. An iOS home-screen web app gets none of the link it was added
 * from and has its own storage, so this is how it gets paired.
 */
export function parsePairingInput(text: string): Pairing | null {
  const trimmed = text.trim()
  if (!trimmed) return null
  const hashAt = trimmed.indexOf('#')
  if (hashAt >= 0) {
    const pairing = parsePairingFragment(trimmed.slice(hashAt))
    if (!pairing) return null
    if (pairing.gateway) return pairing
    try {
      return { ...pairing, gateway: new URL(trimmed).origin }
    } catch {
      return pairing
    }
  }
  if (/^[A-Za-z0-9_-]+$/.test(trimmed)) return { token: trimmed, gateway: null }
  return null
}
