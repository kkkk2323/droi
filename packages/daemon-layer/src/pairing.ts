// What a Client needs to know to pair with a Gateway: the Pairing Token and,
// when it is not the page's own origin, the Gateway's address. The Desktop
// Shell's pairing link carries both in its fragment (`#pair=...&gateway=...`),
// and the Relay's address when the computer has one (`&relay=...`, ADR 0034).

/**
 * How long a computer's network address has to answer before a computer with a Relay is
 * reached through the Relay instead. On the same network it answers in milliseconds.
 */
export const DIRECT_WAIT_MS = 2_000

export interface Pairing {
  token: string
  gateway: string | null
  /** Where the computer is reached away from its network, if the link names a Relay. */
  relay?: string
}

/**
 * A Gateway address as Clients keep it: the origin, and the path without its trailing slash,
 * which a Relay's address has (https://relay.example.com/c/<Relay Id>). Null for anything but
 * an http(s) address.
 */
export function gatewayBase(address: string | null | undefined): string | null {
  if (!address) return null
  try {
    const url = new URL(address.trim())
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    return url.origin + url.pathname.replace(/\/+$/, '')
  } catch {
    return null
  }
}

export function parsePairingFragment(hash: string): Pairing | null {
  const fragment = new URLSearchParams(hash.replace(/^#/, ''))
  const token = fragment.get('pair')
  if (!token) return null
  const pairing: Pairing = { token, gateway: fragment.get('gateway') }
  const relay = gatewayBase(fragment.get('relay'))
  if (relay) pairing.relay = relay
  return pairing
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
    const gateway = pairing.gateway ?? gatewayBase(trimmed.slice(0, hashAt))
    if (!gateway) return pairing
    const result: Pairing = { ...pairing, gateway: gatewayBase(gateway) ?? gateway }
    if (result.relay === result.gateway) delete result.relay
    return result
  }
  if (/^[A-Za-z0-9_-]+$/.test(trimmed)) return { token: trimmed, gateway: null }
  return null
}
