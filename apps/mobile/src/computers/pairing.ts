// Turning a pasted (or scanned) pairing link into a Paired Computer: the
// link gives the Gateway's address and the Pairing Token, and the Gateway's
// /meta names the computer and gives the id that recognises it next time.
import { GATEWAY_META_PATH, type GatewayMeta } from '@droi/daemon-layer/gateway'
import { parsePairingInput } from '@droi/daemon-layer/pairing'

export interface PairingResult {
  id: string
  name: string
  address: string
  token: string
  version: string
}

export class PairingError extends Error {}

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
  let meta: Partial<GatewayMeta>
  try {
    const response = await fetchImpl(new URL(GATEWAY_META_PATH, address).toString(), {
      headers: { accept: 'application/json' },
    })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    meta = (await response.json()) as Partial<GatewayMeta>
  } catch (cause) {
    const reason = cause instanceof Error ? cause.message : String(cause)
    // Without a port the request goes to :80, where something else may answer.
    const hint = new URL(address).port
      ? 'Check that Remote Access is on in Droi on the computer and that the iPhone can reach it: the same network, Tailscale, or a proxy such as Surge.'
      : 'The address has no port; the pairing link Droi shows ends in one, such as :41417.'
    throw new PairingError(`Cannot reach ${address} (${reason}). ${hint}`)
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
    token: pairing.token,
    version: meta.version ?? '',
  }
}
