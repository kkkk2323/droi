// The Paired Computers: which computers this phone can connect to, and which
// one it is connected to now. Names and addresses sit in app storage; each
// Pairing Token sits in the keychain, never in app storage.
import { createPreference, createStringPreference } from '@droi/daemon-layer/local-preference'

export interface PairedComputer {
  /** The computer id from the Gateway's /meta; stable across tokens and addresses. */
  id: string
  name: string
  /** The Gateway's address, e.g. http://192.168.1.10:41417 or a Tailscale name. */
  address: string
}

export interface Keychain {
  get(key: string): Promise<string | null>
  set(key: string, value: string): Promise<void>
  delete(key: string): Promise<void>
}

export const pairedComputers = createPreference<PairedComputer[]>('droi.pairedComputers', [], {
  parse: (raw) => {
    const value: unknown = JSON.parse(raw)
    if (!Array.isArray(value)) return []
    return value.filter(
      (c): c is PairedComputer =>
        typeof c?.id === 'string' && typeof c.name === 'string' && typeof c.address === 'string',
    )
  },
  serialize: JSON.stringify,
})

export const selectedComputerId = createStringPreference('droi.selectedComputer')

export function tokenKey(computerId: string): string {
  return `pairingToken.${computerId}`
}

/**
 * Adds a computer, or updates the one with the same id: its address moves to
 * the new one and the name the user may have given it stays.
 */
export function mergeComputer(
  list: readonly PairedComputer[],
  next: PairedComputer,
): PairedComputer[] {
  const known = list.find((c) => c.id === next.id)
  if (!known) return [...list, next]
  return list.map((c) => (c.id === next.id ? { ...c, address: next.address } : c))
}

/** Stores a freshly paired computer and selects it. */
export async function savePairing(
  pairing: PairedComputer & { token: string },
  keychain: Keychain,
): Promise<void> {
  await keychain.set(tokenKey(pairing.id), pairing.token)
  pairedComputers.set(
    mergeComputer(pairedComputers.get(), {
      id: pairing.id,
      name: pairing.name,
      address: pairing.address,
    }),
  )
  selectedComputerId.set(pairing.id)
}

/** The selected Paired Computer, falling back to the first. */
export function selectedComputer(
  list: readonly PairedComputer[],
  selectedId: string | null,
): PairedComputer | null {
  return list.find((c) => c.id === selectedId) ?? list[0] ?? null
}
