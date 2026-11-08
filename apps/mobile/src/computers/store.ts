// The Paired Computers: which computers this phone can connect to, and which
// one it is connected to now. Names and addresses sit in app storage; each
// Pairing Token sits in the keychain, never in app storage.
import {
  createPreference,
  createStringPreference,
  type LocalPreference,
} from '@droi/daemon-layer/local-preference'
import type { GatewayMeta } from '@droi/daemon-layer/gateway'
import type { SessionTag } from '@droi/daemon-layer/sessions'
import type { SessionWorktree } from '@droi/daemon-layer/worktree'

export interface PairedComputer {
  /** The computer id from the Gateway's /meta; stable across tokens and addresses. */
  id: string
  name: string
  /** The Gateway's address, e.g. http://192.168.1.10:41417 or a Tailscale name. */
  address: string
  /** Its Relay's address (ADR 0034), used when the address above does not answer. */
  relay?: string
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
        typeof c?.id === 'string' &&
        typeof c.name === 'string' &&
        typeof c.address === 'string' &&
        (c.relay === undefined || typeof c.relay === 'string'),
    )
  },
  serialize: JSON.stringify,
})

export const selectedComputerId = createStringPreference('droi.selectedComputer')

export function tokenKey(computerId: string): string {
  return `pairingToken.${computerId}`
}

/**
 * Adds a computer, or updates the one with the same id: its address and Relay
 * move to the new ones and the name the user may have given it stays.
 */
export function mergeComputer(
  list: readonly PairedComputer[],
  next: PairedComputer,
): PairedComputer[] {
  const known = list.find((c) => c.id === next.id)
  if (!known) return [...list, next]
  return list.map((c) => {
    if (c.id !== next.id) return c
    const { relay: _old, ...rest } = c
    return { ...rest, address: next.address, ...(next.relay ? { relay: next.relay } : {}) }
  })
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
      ...(pairing.relay ? { relay: pairing.relay } : {}),
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

/**
 * Keeps a computer's Relay as its Gateway's /meta names it (ADR 0034), so a phone paired before
 * the Relay was set, or changed, follows it without pairing again. A /meta without the field,
 * from an older Droi, or of another computer changes nothing.
 */
export function followRelay(computerId: string, meta: Partial<GatewayMeta>): void {
  if (meta.computerId !== computerId || typeof meta.relay !== 'string') return
  const relay = meta.relay
  const known = pairedComputers.get().find((c) => c.id === computerId)
  if (!known || (known.relay ?? '') === relay) return
  pairedComputers.set(
    pairedComputers.get().map((c) => {
      if (c.id !== computerId) return c
      const { relay: _old, ...rest } = c
      return relay ? { ...rest, relay } : rest
    }),
  )
}

export function updateComputer(id: string, patch: Partial<Omit<PairedComputer, 'id'>>): void {
  pairedComputers.set(pairedComputers.get().map((c) => (c.id === id ? { ...c, ...patch } : c)))
}

/** Forgets a computer: its token, its entry and what was kept of its Session list. */
export async function removeComputer(id: string, keychain: Keychain): Promise<void> {
  await keychain.delete(tokenKey(id))
  const rest = pairedComputers.get().filter((c) => c.id !== id)
  pairedComputers.set(rest)
  if (selectedComputerId.get() === id) selectedComputerId.set(rest[0]?.id ?? null)
  sessionSummaries(id).set([])
  lastSessionOf(id).set(null)
}

/**
 * What was last seen of a computer's Session list, so switching to it or a
 * cold start is not a blank list. Transcripts are never kept.
 */
export interface SessionSummaryCache {
  sessionId: string
  title: string
  cwd: string | null
  repoRoot: string | null
  /** Absent in caches written before worktrees. */
  worktree?: SessionWorktree | null
  updatedAt: number
  activityAt: number
  viewedAt: number | null
  kind: string | null
  archivedAt: string | null
  parentId: string | null
  tags: SessionTag[]
}

const summaryPreferences = new Map<string, LocalPreference<SessionSummaryCache[]>>()

export function sessionSummaries(computerId: string): LocalPreference<SessionSummaryCache[]> {
  let preference = summaryPreferences.get(computerId)
  if (!preference) {
    // The Kernel's Sessions; a list kept from the Daemon's would name Sessions it does not have.
    preference = createPreference<SessionSummaryCache[]>(`droi.kernelSessions.${computerId}`, [], {
      parse: (raw) => {
        const value: unknown = JSON.parse(raw)
        return Array.isArray(value) ? (value as SessionSummaryCache[]) : []
      },
      serialize: JSON.stringify,
    })
    summaryPreferences.set(computerId, preference)
  }
  return preference
}

const lastSessionPreferences = new Map<string, LocalPreference<string | null>>()

/** The Session open when the app last showed this computer. */
export function lastSessionOf(computerId: string): LocalPreference<string | null> {
  let preference = lastSessionPreferences.get(computerId)
  if (!preference) {
    preference = createStringPreference(`droi.lastSession.${computerId}`)
    lastSessionPreferences.set(computerId, preference)
  }
  return preference
}
