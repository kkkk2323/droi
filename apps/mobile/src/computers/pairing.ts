// Turning a pasted (or scanned) pairing link into a Paired Computer: the
// link gives the Gateway's address, the Pairing Token and, when the computer
// has one, its Relay (ADR 0034); the Gateway's /meta names the computer and
// gives the id that recognises it next time.
import { GATEWAY_META_PATH, gatewayEndpoint, type GatewayMeta } from '@droi/daemon-layer/gateway'
import { DIRECT_WAIT_MS, parsePairingInput } from '@droi/daemon-layer/pairing'

export interface PairingResult {
  id: string
  name: string
  address: string
  /** The Relay's address, when the link named one. */
  relay?: string
  token: string
  version: string
}

export class PairingError extends Error {}

async function fetchMeta(
  address: string,
  fetchImpl: typeof fetch,
  waitMs?: number,
): Promise<Partial<GatewayMeta>> {
  const abort = waitMs === undefined ? null : new AbortController()
  const timer = abort ? setTimeout(() => abort.abort(), waitMs) : null
  try {
    const response = await fetchImpl(gatewayEndpoint(address, GATEWAY_META_PATH).toString(), {
      headers: { accept: 'application/json' },
      ...(abort ? { signal: abort.signal } : {}),
    })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    return (await response.json()) as Partial<GatewayMeta>
  } catch (cause) {
    if (abort?.signal.aborted) throw new Error('no answer in time', { cause })
    throw cause
  } finally {
    if (timer) clearTimeout(timer)
  }
}

export async function resolvePairing(
  text: string,
  fetchImpl: typeof fetch = (input, init) => fetch(input, init),
): Promise<PairingResult> {
  const pairing = parsePairingInput(text)
  if (!pairing) throw new PairingError('That is not a Droi pairing link.')
  if (!pairing.gateway) {
    throw new PairingError('Paste the whole pairing link, with the computer’s address.')
  }
  const address = pairing.gateway
  const relay = pairing.relay
  let meta: Partial<GatewayMeta>
  try {
    // The network address first; away from it, the Relay.
    meta = await fetchMeta(address, fetchImpl, relay ? DIRECT_WAIT_MS : undefined)
  } catch (cause) {
    const reason = cause instanceof Error ? cause.message : String(cause)
    try {
      if (!relay) throw cause
      meta = await fetchMeta(relay, fetchImpl)
    } catch (relayCause) {
      if (relay) {
        const relayReason = relayCause instanceof Error ? relayCause.message : String(relayCause)
        throw new PairingError(
          `Cannot reach ${address} (${reason}) or its relay ${relay} (${relayReason}). Check that Remote Access is on in Droi on the computer and that it shows the relay as connected.`,
        )
      }
      // Without a port the request goes to :80, where something else may answer.
      const hint = new URL(address).port
        ? 'Check that Remote Access is on in Droi on the computer and that the iPhone can reach it: the same network, Tailscale, or a proxy such as Surge.'
        : 'The address has no port; the pairing link Droi shows ends in one, such as :41417.'
      throw new PairingError(`Cannot reach ${address} (${reason}). ${hint}`)
    }
  }
  if (meta.app !== 'Droi' || !meta.computerId) {
    throw new PairingError(
      `${address} is not a Droi that can pair with this app. Update Droi on the computer.`,
    )
  }
  return {
    id: meta.computerId,
    name: meta.name || new URL(address).hostname,
    address,
    ...(relay ? { relay } : {}),
    token: pairing.token,
    version: meta.version ?? '',
  }
}
