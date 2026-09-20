// Where this Client runs and how it reaches the Gateway. The Local Client is
// told by the Desktop Shell preload; a Remote Client learns the Pairing Token
// from the link it was opened with (`#pair=...`) and remembers it.

export interface ClientConfig {
  kind: 'local' | 'remote'
  gatewayUrl: string
  pairingToken: string | null
}

export interface ClientEnvironment {
  origin: string
  hash: string
  droiShell: Window['droiShell']
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
  replaceUrl: (url: string) => void
}

const TOKEN_KEY = 'droi.pairingToken'
const GATEWAY_KEY = 'droi.gatewayUrl'

export function resolveClientConfig(env: ClientEnvironment): ClientConfig {
  if (env.droiShell) {
    return {
      kind: 'local',
      gatewayUrl: env.droiShell.gatewayUrl,
      pairingToken: env.droiShell.pairingToken,
    }
  }

  const pairing = parsePairingFragment(env.hash)
  if (pairing) {
    savePairing(env.storage, pairing)
    // The token must not linger in the address bar, history or a shared screenshot.
    env.replaceUrl(new URL('/', env.origin).toString())
  }

  return {
    kind: 'remote',
    gatewayUrl: env.storage.getItem(GATEWAY_KEY) ?? env.origin,
    pairingToken: env.storage.getItem(TOKEN_KEY),
  }
}

export interface Pairing {
  token: string
  gateway: string | null
}

function parsePairingFragment(hash: string): Pairing | null {
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

export function savePairing(storage: ClientEnvironment['storage'], pairing: Pairing): void {
  storage.setItem(TOKEN_KEY, pairing.token)
  if (pairing.gateway) storage.setItem(GATEWAY_KEY, pairing.gateway)
}

export function forgetPairing(storage: ClientEnvironment['storage']): void {
  storage.removeItem(TOKEN_KEY)
}

export function browserEnvironment(): ClientEnvironment {
  return {
    origin: window.location.origin,
    hash: window.location.hash,
    droiShell: window.droiShell,
    storage: window.localStorage,
    replaceUrl: (url) => window.history.replaceState(null, '', url),
  }
}
