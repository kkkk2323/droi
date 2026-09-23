// Where this Client runs and how it reaches the Gateway. The Local Client is
// told by the Desktop Shell preload; a Remote Client learns the Pairing Token
// from the link it was opened with (`#pair=...`) and remembers it.
import type { ClientConfig } from '@droi/daemon-layer/connection'
import { parsePairingFragment, type Pairing } from '@droi/daemon-layer/pairing'

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
