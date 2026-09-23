// The Paired Computers: which computers this phone can connect to, and which
// one it is connected to now. Names and addresses sit in app storage; each
// Pairing Token sits in the keychain, never in app storage.
import {
  createPreference,
  createStringPreference,
  type LocalPreference,
} from '@droi/daemon-layer/local-preference'

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
 * cold start is not a blank drawer. Transcripts are never kept.
 */
export interface SessionSummaryCache {
  sessionId: string
  title: string
  cwd: string | null
  repoRoot: string | null
  updatedAt: number
  archivedAt: string | null
  parentId: string | null
  /** Absent in caches written before subagents were told apart. */
  callingSessionId?: string | null
  callingToolUseId?: string | null
}

const summaryPreferences = new Map<string, LocalPreference<SessionSummaryCache[]>>()

export function sessionSummaries(computerId: string): LocalPreference<SessionSummaryCache[]> {
  let preference = summaryPreferences.get(computerId)
  if (!preference) {
    preference = createPreference<SessionSummaryCache[]>(`droi.sessions.${computerId}`, [], {
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
