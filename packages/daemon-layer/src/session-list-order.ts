// The computer's Session list order, through its Gateway: the pins and the
// Workspace order its window shows, which a phone lists its Sessions by.
import { gatewaySessionListUrl, type PinKind, type SessionListOrder } from './gateway'
import { DEFAULT_SORT, type SessionView, type SortOrder } from './sessions'

export interface SessionListOrderSource {
  /** Null from a Gateway that has no window to follow, or none to reach. */
  read(): Promise<SessionListOrder | null>
  setPinned(kind: PinKind, key: string, pinned: boolean): Promise<void>
}

export function createSessionListOrder(
  config: { gatewayUrl: string; pairingToken: string | null },
  fetchImpl: typeof fetch,
): SessionListOrderSource {
  const token = () => config.pairingToken ?? ''
  return {
    async read() {
      try {
        const response = await fetchImpl(gatewaySessionListUrl(config.gatewayUrl, token()), {
          cache: 'no-store',
        })
        if (!response.ok) return null
        return parseOrder(await response.json())
      } catch {
        return null
      }
    },
    async setPinned(kind, key, pinned) {
      const response = await fetchImpl(
        gatewaySessionListUrl(config.gatewayUrl, token(), { kind, key, pinned }),
        { method: 'POST', cache: 'no-store' },
      )
      if (!response.ok) throw new Error(`The computer did not pin it (HTTP ${response.status}).`)
    },
  }
}

const strings = (value: unknown): string[] =>
  Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []

function parseOrder(body: unknown): SessionListOrder | null {
  if (typeof body !== 'object' || body === null) return null
  const b = body as Record<string, unknown>
  return {
    pinnedSessions: strings(b['pinnedSessions']),
    pinnedWorkspaces: strings(b['pinnedWorkspaces']),
    workspaceOrder: strings(b['workspaceOrder']),
    sessionSort: typeof b['sessionSort'] === 'string' ? b['sessionSort'] : '',
    sessionView: typeof b['sessionView'] === 'string' ? b['sessionView'] : '',
  }
}

/** How the window lists the Sessions: by time, or by Workspace (the default). */
export function viewOf(shared: SessionListOrder | null): SessionView {
  return shared?.sessionView === 'time' ? 'time' : 'workspaces'
}

/** The window's order: its Workspaces as it shows them, the rest after. */
export function sortOrderOf(
  shared: SessionListOrder | null,
  firstSeen: Readonly<Record<string, number>>,
): SortOrder {
  if (!shared) return { ...DEFAULT_SORT, firstSeen }
  return {
    workspaces: shared.workspaceOrder.length > 0 ? 'manual' : DEFAULT_SORT.workspaces,
    sessions: shared.sessionSort === 'created' ? 'created' : 'recent',
    manual: shared.workspaceOrder,
    firstSeen,
  }
}

export function withPin(
  order: SessionListOrder,
  kind: PinKind,
  key: string,
  pinned: boolean,
): SessionListOrder {
  const field = kind === 'workspace' ? 'pinnedWorkspaces' : 'pinnedSessions'
  const rest = order[field].filter((k) => k !== key)
  return { ...order, [field]: pinned ? [...rest, key] : rest }
}
